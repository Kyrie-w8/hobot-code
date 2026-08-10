import { Database } from "bun:sqlite";
import { createHash, randomUUID } from "node:crypto";
import { chmodSync, mkdirSync, statSync } from "node:fs";
import { dirname } from "node:path";

export type GoalStatus = "active" | "paused" | "completed" | "cancelled";

export interface GoalRecord {
  id: string;
  project: string;
  objective: string;
  status: GoalStatus;
  turnBudget: number;
  tokenBudget?: number;
  turnsUsed: number;
  tokensUsed: number;
  elapsedMs: number;
  continuationCount: number;
  verificationStatus?: string;
  verificationFingerprint?: string;
  lastNote?: string;
  outcome?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
}

interface GoalRow {
  public_id: string;
  project: string;
  objective: string;
  status: GoalStatus;
  turn_budget: number;
  token_budget: number | null;
  turns_used: number;
  tokens_used: number;
  elapsed_ms: number;
  continuation_count: number;
  verification_status: string | null;
  verification_fingerprint: string | null;
  last_note: string | null;
  outcome: string | null;
  created_at: string;
  updated_at: string;
  completed_at: string | null;
}

function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function cleanText(value: string, label: string, maxLength: number): string {
  const normalized = String(value ?? "").replace(/\0/g, "").replace(/\s+/g, " ").trim();
  if (!normalized) throw new Error(`${label} must not be empty`);
  if (normalized.length > maxLength) throw new Error(`${label} exceeds ${maxLength} characters`);
  return normalized;
}

function toRecord(row: GoalRow): GoalRecord {
  return {
    id: row.public_id,
    project: row.project,
    objective: row.objective,
    status: row.status,
    turnBudget: row.turn_budget,
    ...(row.token_budget === null ? {} : { tokenBudget: row.token_budget }),
    turnsUsed: row.turns_used,
    tokensUsed: row.tokens_used,
    elapsedMs: row.elapsed_ms,
    continuationCount: row.continuation_count,
    ...(row.verification_status ? { verificationStatus: row.verification_status } : {}),
    ...(row.verification_fingerprint ? { verificationFingerprint: row.verification_fingerprint } : {}),
    ...(row.last_note ? { lastNote: row.last_note } : {}),
    ...(row.outcome ? { outcome: row.outcome } : {}),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    ...(row.completed_at ? { completedAt: row.completed_at } : {}),
  };
}

export class GoalStore {
  readonly path: string;
  private readonly db: Database;
  private readonly readOnly: boolean;

  constructor(path: string, options: { readOnly?: boolean } = {}) {
    this.path = path;
    this.readOnly = options.readOnly ?? false;
    if (this.readOnly) {
      this.db = new Database(path, { readonly: true });
      this.db.exec("PRAGMA query_only = ON; PRAGMA busy_timeout = 3000;");
      return;
    }
    mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
    this.db = new Database(path, { create: true });
    chmodSync(path, 0o600);
    this.db.exec(`
      PRAGMA journal_mode = WAL;
      PRAGMA synchronous = NORMAL;
      PRAGMA busy_timeout = 3000;
      PRAGMA secure_delete = ON;

      CREATE TABLE IF NOT EXISTS goals (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        public_id TEXT NOT NULL UNIQUE,
        project TEXT NOT NULL,
        objective TEXT NOT NULL,
        objective_hash TEXT NOT NULL,
        status TEXT NOT NULL CHECK (status IN ('active', 'paused', 'completed', 'cancelled')),
        turn_budget INTEGER NOT NULL,
        token_budget INTEGER,
        turns_used INTEGER NOT NULL DEFAULT 0,
        tokens_used INTEGER NOT NULL DEFAULT 0,
        elapsed_ms INTEGER NOT NULL DEFAULT 0,
        continuation_count INTEGER NOT NULL DEFAULT 0,
        last_session TEXT,
        last_started_at TEXT,
        verification_status TEXT,
        verification_fingerprint TEXT,
        last_note TEXT,
        outcome TEXT,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        completed_at TEXT
      );
      CREATE UNIQUE INDEX IF NOT EXISTS goals_one_open_per_project
        ON goals(project) WHERE status IN ('active', 'paused');
      CREATE INDEX IF NOT EXISTS goals_project_history
        ON goals(project, updated_at DESC);

      CREATE TABLE IF NOT EXISTS goal_events (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        goal_public_id TEXT NOT NULL,
        event TEXT NOT NULL,
        actor TEXT NOT NULL,
        created_at TEXT NOT NULL,
        details TEXT NOT NULL
      );
    `);
  }

  private audit(goalId: string, event: string, actor: string, details: Record<string, unknown>): void {
    this.db.run(
      "INSERT INTO goal_events(goal_public_id, event, actor, created_at, details) VALUES (?, ?, ?, ?, ?)",
      [goalId, event, actor, new Date().toISOString(), JSON.stringify(details)],
    );
  }

  private transaction<T>(operation: () => T): T {
    return this.db.transaction(operation).immediate();
  }

  private requireChange(result: { changes?: number }, action: string): void {
    if ((result.changes ?? 0) !== 1) {
      throw new Error(`Goal state changed before ${action}; reload and retry`);
    }
  }

  private row(id: string, project: string): GoalRow | null {
    return this.db.query(`
      SELECT public_id, project, objective, status, turn_budget, token_budget,
        turns_used, tokens_used, elapsed_ms, continuation_count,
        verification_status, verification_fingerprint, last_note, outcome,
        created_at, updated_at, completed_at
      FROM goals WHERE public_id = ? AND project = ?
    `).get(id, project) as GoalRow | null;
  }

  current(project: string): GoalRecord | undefined {
    const row = this.db.query(`
      SELECT public_id, project, objective, status, turn_budget, token_budget,
        turns_used, tokens_used, elapsed_ms, continuation_count,
        verification_status, verification_fingerprint, last_note, outcome,
        created_at, updated_at, completed_at
      FROM goals
      WHERE project = ? AND status IN ('active', 'paused')
      ORDER BY id DESC LIMIT 1
    `).get(project) as GoalRow | null;
    return row ? toRecord(row) : undefined;
  }

  create(options: {
    project: string;
    objective: string;
    turnBudget: number;
    tokenBudget?: number;
    session?: string;
  }): GoalRecord {
    const objective = cleanText(options.objective, "goal objective", 4000);
    if (!Number.isInteger(options.turnBudget) || options.turnBudget < 1 || options.turnBudget > 10_000) {
      throw new Error("turn budget must be between 1 and 10000");
    }
    if (options.tokenBudget !== undefined
      && (!Number.isInteger(options.tokenBudget) || options.tokenBudget < 1000 || options.tokenBudget > 1_000_000_000)) {
      throw new Error("token budget must be between 1000 and 1000000000");
    }
    return this.transaction(() => {
      if (this.current(options.project)) throw new Error("This project already has an active or paused goal");
      const id = `goal_${randomUUID().replaceAll("-", "").slice(0, 12)}`;
      const now = new Date().toISOString();
      this.db.run(`
        INSERT INTO goals(
          public_id, project, objective, objective_hash, status, turn_budget, token_budget,
          last_session, last_started_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?)
      `, [
        id, options.project, objective, sha256(objective), options.turnBudget,
        options.tokenBudget ?? null, options.session ?? null, now, now, now,
      ]);
      this.audit(id, "create", "user", {
        objectiveHash: sha256(objective),
        turnBudget: options.turnBudget,
        tokenBudget: options.tokenBudget ?? null,
      });
      return toRecord(this.row(id, options.project)!);
    });
  }

  restore(project: string, session: string): GoalRecord | undefined {
    return this.transaction(() => {
      const current = this.current(project);
      if (!current) return undefined;
      const state = this.db.query("SELECT last_session FROM goals WHERE public_id = ? AND project = ?")
        .get(current.id, project) as { last_session: string | null };
      if (state.last_session !== session) {
        const now = new Date().toISOString();
        const result = this.db.run(`
          UPDATE goals
          SET continuation_count = continuation_count + 1, last_session = ?, last_started_at = ?, updated_at = ?
          WHERE public_id = ? AND project = ? AND status IN ('active', 'paused')
        `, [session, now, now, current.id, project]) as { changes?: number };
        this.requireChange(result, "continuing the goal");
        this.audit(current.id, "continue", "system", { sessionHash: sha256(session) });
      }
      return toRecord(this.row(current.id, project)!);
    });
  }

  consumeTurn(project: string, tokens: number, durationMs: number): GoalRecord | undefined {
    const boundedTokens = Number.isFinite(tokens) ? Math.max(0, Math.round(tokens)) : 0;
    const boundedDuration = Number.isFinite(durationMs) ? Math.max(0, Math.round(durationMs)) : 0;
    return this.transaction(() => {
      const current = this.current(project);
      if (!current || current.status !== "active") return current;
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals
        SET turns_used = turns_used + 1,
          tokens_used = tokens_used + ?,
          elapsed_ms = elapsed_ms + ?,
          status = CASE
            WHEN turns_used + 1 >= turn_budget
              OR (token_budget IS NOT NULL AND tokens_used + ? >= token_budget)
            THEN 'paused'
            ELSE 'active'
          END,
          updated_at = ?
        WHERE public_id = ? AND project = ? AND status = 'active'
      `, [boundedTokens, boundedDuration, boundedTokens, now, current.id, project]) as { changes?: number };
      this.requireChange(result, "recording the goal turn");
      const updated = toRecord(this.row(current.id, project)!);
      this.audit(current.id, updated.status === "paused" ? "budget_exhausted" : "turn", "system", {
        turnsUsed: updated.turnsUsed,
        tokensUsed: updated.tokensUsed,
        durationMs: boundedDuration,
      });
      return updated;
    });
  }

  progress(project: string, note: string, actor: "agent" | "user"): GoalRecord {
    const normalized = cleanText(note, "goal note", 4000);
    return this.transaction(() => {
      const current = this.current(project);
      if (!current) throw new Error("No active or paused goal exists for this project");
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals SET last_note = ?, updated_at = ?
        WHERE public_id = ? AND project = ? AND status IN ('active', 'paused')
      `, [normalized, now, current.id, project]) as { changes?: number };
      this.requireChange(result, "recording goal progress");
      this.audit(current.id, "progress", actor, { noteHash: sha256(normalized) });
      return toRecord(this.row(current.id, project)!);
    });
  }

  extend(project: string, extraTurns: number, extraTokens: number | undefined): GoalRecord {
    if (!Number.isInteger(extraTurns) || extraTurns < 1 || extraTurns > 10_000) {
      throw new Error("extra turn budget must be between 1 and 10000");
    }
    if (extraTokens !== undefined
      && (!Number.isInteger(extraTokens) || extraTokens < 1000 || extraTokens > 1_000_000_000)) {
      throw new Error("extra token budget must be between 1000 and 1000000000");
    }
    return this.transaction(() => {
      const current = this.current(project);
      if (!current) throw new Error("No active or paused goal exists for this project");
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals
        SET turn_budget = turn_budget + ?,
          token_budget = CASE WHEN ? IS NULL THEN token_budget ELSE COALESCE(token_budget, tokens_used) + ? END,
          status = 'active', completed_at = NULL, outcome = NULL, updated_at = ?
        WHERE public_id = ? AND project = ? AND status IN ('active', 'paused')
      `, [extraTurns, extraTokens ?? null, extraTokens ?? null, now, current.id, project]) as { changes?: number };
      this.requireChange(result, "extending the goal");
      this.audit(current.id, "extend", "user", { extraTurns, extraTokens: extraTokens ?? null });
      return toRecord(this.row(current.id, project)!);
    });
  }

  pause(project: string, actor: "agent" | "user"): GoalRecord {
    return this.transaction(() => {
      const current = this.current(project);
      if (!current || current.status !== "active") throw new Error("No active goal exists for this project");
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals SET status = 'paused', updated_at = ?
        WHERE public_id = ? AND project = ? AND status = 'active'
      `, [now, current.id, project]) as { changes?: number };
      this.requireChange(result, "pausing the goal");
      this.audit(current.id, "pause", actor, {});
      return toRecord(this.row(current.id, project)!);
    });
  }

  resume(project: string): GoalRecord {
    return this.transaction(() => {
      const current = this.current(project);
      if (!current || current.status !== "paused") throw new Error("No paused goal exists for this project");
      if (current.turnsUsed >= current.turnBudget
        || (current.tokenBudget !== undefined && current.tokensUsed >= current.tokenBudget)) {
        throw new Error("Goal budget is exhausted; use /goal extend <turns> [tokens]");
      }
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals SET status = 'active', updated_at = ?
        WHERE public_id = ? AND project = ? AND status = 'paused'
      `, [now, current.id, project]) as { changes?: number };
      this.requireChange(result, "resuming the goal");
      this.audit(current.id, "resume", "user", {});
      return toRecord(this.row(current.id, project)!);
    });
  }

  complete(options: {
    project: string;
    outcome: string;
    actor: "agent" | "user";
    verificationStatus?: string;
    verificationFingerprint?: string;
  }): GoalRecord {
    const outcome = cleanText(options.outcome, "goal outcome", 4000);
    return this.transaction(() => {
      const current = this.current(options.project);
      if (!current) throw new Error("No active or paused goal exists for this project");
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals
        SET status = 'completed', outcome = ?, verification_status = ?,
          verification_fingerprint = ?, completed_at = ?, updated_at = ?
        WHERE public_id = ? AND project = ? AND status IN ('active', 'paused')
      `, [
        outcome, options.verificationStatus ?? null, options.verificationFingerprint ?? null,
        now, now, current.id, options.project,
      ]) as { changes?: number };
      this.requireChange(result, "completing the goal");
      this.audit(current.id, "complete", options.actor, {
        outcomeHash: sha256(outcome),
        verificationStatus: options.verificationStatus ?? null,
        verificationFingerprint: options.verificationFingerprint ?? null,
      });
      return toRecord(this.row(current.id, options.project)!);
    });
  }

  cancel(project: string, reason: string): GoalRecord {
    const normalized = cleanText(reason, "cancellation reason", 2000);
    return this.transaction(() => {
      const current = this.current(project);
      if (!current) throw new Error("No active or paused goal exists for this project");
      const now = new Date().toISOString();
      const result = this.db.run(`
        UPDATE goals SET status = 'cancelled', outcome = ?, completed_at = ?, updated_at = ?
        WHERE public_id = ? AND project = ? AND status IN ('active', 'paused')
      `, [normalized, now, now, current.id, project]) as { changes?: number };
      this.requireChange(result, "cancelling the goal");
      this.audit(current.id, "cancel", "user", { reasonHash: sha256(normalized) });
      return toRecord(this.row(current.id, project)!);
    });
  }

  history(project: string, limit = 20): GoalRecord[] {
    const rows = this.db.query(`
      SELECT public_id, project, objective, status, turn_budget, token_budget,
        turns_used, tokens_used, elapsed_ms, continuation_count,
        verification_status, verification_fingerprint, last_note, outcome,
        created_at, updated_at, completed_at
      FROM goals WHERE project = ? ORDER BY id DESC LIMIT ?
    `).all(project, Math.max(1, Math.min(limit, 100))) as GoalRow[];
    return rows.map(toRecord);
  }

  stats(): { databaseBytes: number } {
    return { databaseBytes: statSync(this.path).size };
  }

  close(): void {
    try {
      if (!this.readOnly) this.db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
    } finally {
      this.db.close();
    }
  }
}
