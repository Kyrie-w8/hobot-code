import { Database } from "bun:sqlite";
import { createHash, randomUUID } from "node:crypto";
import { chmodSync, mkdirSync, statSync } from "node:fs";
import { dirname } from "node:path";

import {
  memoryMatchQuery,
  normalizeMemoryContent,
  validateMemoryInput,
} from "./control-plane.mjs";

export type MemoryScope = "user" | "project" | "board" | "session";
export type MemoryKind = "preference" | "decision" | "fact" | "fix" | "instruction" | "note";

export interface MemoryContext {
  user: string;
  project: string;
  board: string;
  session?: string;
}

export interface MemoryRecord {
  id: string;
  scope: MemoryScope;
  kind: MemoryKind;
  content: string;
  createdAt: string;
  updatedAt: string;
  expiresAt?: string;
}

export interface MemoryEvent {
  event: string;
  memoryId?: string;
  actor: string;
  createdAt: string;
  details: Record<string, unknown>;
}

interface MemoryRow {
  public_id: string;
  scope: MemoryScope;
  kind: MemoryKind;
  content: string;
  created_at: string;
  updated_at: string;
  expires_at: string | null;
}

function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function toRecord(row: MemoryRow): MemoryRecord {
  return {
    id: row.public_id,
    scope: row.scope,
    kind: row.kind,
    content: row.content,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    ...(row.expires_at ? { expiresAt: row.expires_at } : {}),
  };
}

function scopeKey(scope: MemoryScope, context: MemoryContext): string | undefined {
  return context[scope];
}

function scopeFilter(context: MemoryContext, scopes?: MemoryScope[]): { sql: string; values: string[] } {
  const selected = scopes?.length ? scopes : ["user", "project", "board", "session"] as MemoryScope[];
  const clauses: string[] = [];
  const values: string[] = [];
  for (const scope of selected) {
    const key = scopeKey(scope, context);
    if (!key) continue;
    clauses.push("(m.scope = ? AND m.scope_key = ?)");
    values.push(scope, key);
  }
  return { sql: clauses.length ? `(${clauses.join(" OR ")})` : "0", values };
}

function lexicalScore(content: string, query: string): number {
  const normalized = content.toLowerCase();
  const terms = query.toLowerCase().match(/[\p{L}\p{N}_+.-]+/gu) ?? [];
  return terms.reduce((score, term) => score + (normalized.includes(term) ? Math.min(term.length, 12) : 0), 0);
}

export class MemoryStore {
  private readonly db: Database;

  constructor(readonly path: string) {
    mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
    this.db = new Database(path, { create: true });
    chmodSync(path, 0o600);
    this.db.exec(`
      PRAGMA journal_mode = WAL;
      PRAGMA synchronous = NORMAL;
      PRAGMA busy_timeout = 3000;
      PRAGMA foreign_keys = ON;
      PRAGMA secure_delete = ON;

      CREATE TABLE IF NOT EXISTS memories (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        public_id TEXT NOT NULL UNIQUE,
        scope TEXT NOT NULL CHECK (scope IN ('user', 'project', 'board', 'session')),
        scope_key TEXT NOT NULL,
        kind TEXT NOT NULL CHECK (kind IN ('preference', 'decision', 'fact', 'fix', 'instruction', 'note')),
        content TEXT NOT NULL,
        content_hash TEXT NOT NULL,
        source_session TEXT,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        last_used_at TEXT,
        expires_at TEXT,
        UNIQUE(scope, scope_key, kind, content_hash)
      );

      CREATE INDEX IF NOT EXISTS memories_scope_idx
        ON memories(scope, scope_key, updated_at DESC);
      CREATE INDEX IF NOT EXISTS memories_expiry_idx
        ON memories(expires_at);

      CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
        content,
        kind,
        content='memories',
        content_rowid='id',
        tokenize='unicode61 remove_diacritics 2'
      );

      CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
        INSERT INTO memory_fts(rowid, content, kind) VALUES (new.id, new.content, new.kind);
      END;
      CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
        INSERT INTO memory_fts(memory_fts, rowid, content, kind)
          VALUES ('delete', old.id, old.content, old.kind);
      END;
      CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
        INSERT INTO memory_fts(memory_fts, rowid, content, kind)
          VALUES ('delete', old.id, old.content, old.kind);
        INSERT INTO memory_fts(rowid, content, kind) VALUES (new.id, new.content, new.kind);
      END;

      CREATE TABLE IF NOT EXISTS memory_events (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        event TEXT NOT NULL,
        memory_public_id TEXT,
        actor TEXT NOT NULL,
        created_at TEXT NOT NULL,
        details TEXT NOT NULL
      );
    `);
    this.pruneExpired("system");
  }

  private audit(event: string, memoryId: string | undefined, actor: string, details: Record<string, unknown>): void {
    this.db.run(
      "INSERT INTO memory_events(event, memory_public_id, actor, created_at, details) VALUES (?, ?, ?, ?, ?)",
      [event, memoryId ?? null, actor, new Date().toISOString(), JSON.stringify(details)],
    );
  }

  private accessibleRow(id: string, context: MemoryContext): MemoryRow | null {
    const filter = scopeFilter(context);
    return this.db.query(`
      SELECT m.public_id, m.scope, m.kind, m.content, m.created_at, m.updated_at, m.expires_at
      FROM memories m
      WHERE m.public_id = ? AND ${filter.sql}
    `).get(id, ...filter.values) as MemoryRow | null;
  }

  add(options: {
    scope: MemoryScope;
    kind: MemoryKind;
    content: string;
    context: MemoryContext;
    sourceSession?: string;
    expiresDays?: number | null;
    maxContentChars: number;
    actor: string;
  }): { record: MemoryRecord; created: boolean } {
    const input = validateMemoryInput(options.scope, options.kind, options.content, options.maxContentChars);
    const key = scopeKey(options.scope, options.context);
    if (!key) throw new Error(`${options.scope} memory is unavailable in this session`);
    const now = new Date().toISOString();
    const expiresAt = options.expiresDays
      ? new Date(Date.now() + options.expiresDays * 86_400_000).toISOString()
      : null;
    const hash = sha256(input.content);
    const existing = this.db.query(`
      SELECT public_id, scope, kind, content, created_at, updated_at, expires_at
      FROM memories
      WHERE scope = ? AND scope_key = ? AND kind = ? AND content_hash = ?
    `).get(options.scope, key, options.kind, hash) as MemoryRow | null;

    if (existing) {
      this.db.run(
        "UPDATE memories SET updated_at = ?, source_session = ?, expires_at = ? WHERE public_id = ?",
        [now, options.sourceSession ?? null, expiresAt, existing.public_id],
      );
      const updated = this.accessibleRow(existing.public_id, options.context);
      if (!updated) throw new Error("failed to reload deduplicated memory");
      this.audit("refresh", existing.public_id, options.actor, {
        scope: options.scope,
        kind: options.kind,
        contentHash: hash,
      });
      return { record: toRecord(updated), created: false };
    }

    const publicId = `mem_${randomUUID().replaceAll("-", "").slice(0, 12)}`;
    this.db.run(`
      INSERT INTO memories(
        public_id, scope, scope_key, kind, content, content_hash,
        source_session, created_at, updated_at, expires_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, [
      publicId,
      options.scope,
      key,
      options.kind,
      input.content,
      hash,
      options.sourceSession ?? null,
      now,
      now,
      expiresAt,
    ]);
    this.audit("create", publicId, options.actor, {
      scope: options.scope,
      kind: options.kind,
      contentHash: hash,
      expiresAt,
    });
    const created = this.accessibleRow(publicId, options.context);
    if (!created) throw new Error("failed to reload created memory");
    return { record: toRecord(created), created: true };
  }

  search(
    query: string,
    context: MemoryContext,
    scopes: MemoryScope[] | undefined,
    limit: number,
    auditActor: string | null = "agent",
  ): MemoryRecord[] {
    const normalized = normalizeMemoryContent(query, 1000);
    const filter = scopeFilter(context, scopes);
    const match = memoryMatchQuery(normalized);
    const boundedLimit = Math.max(1, Math.min(limit, 50));
    let rows: MemoryRow[] = [];

    if (match) {
      try {
        rows = this.db.query(`
          SELECT m.public_id, m.scope, m.kind, m.content, m.created_at, m.updated_at, m.expires_at
          FROM memory_fts
          JOIN memories m ON m.id = memory_fts.rowid
          WHERE memory_fts MATCH ?
            AND ${filter.sql}
            AND (m.expires_at IS NULL OR m.expires_at > ?)
          ORDER BY bm25(memory_fts), m.updated_at DESC
          LIMIT ?
        `).all(match, ...filter.values, new Date().toISOString(), boundedLimit) as MemoryRow[];
      } catch {
        rows = [];
      }
    }

    if (rows.length < boundedLimit) {
      const recent = this.db.query(`
        SELECT m.public_id, m.scope, m.kind, m.content, m.created_at, m.updated_at, m.expires_at
        FROM memories m
        WHERE ${filter.sql}
          AND (m.expires_at IS NULL OR m.expires_at > ?)
        ORDER BY m.updated_at DESC
        LIMIT 200
      `).all(...filter.values, new Date().toISOString()) as MemoryRow[];
      const seen = new Set(rows.map((row) => row.public_id));
      const fallback = recent
        .filter((row) => !seen.has(row.public_id))
        .map((row) => ({ row, score: lexicalScore(row.content, normalized) }))
        .filter((item) => item.score > 0)
        .sort((left, right) => right.score - left.score || right.row.updated_at.localeCompare(left.row.updated_at))
        .slice(0, boundedLimit - rows.length)
        .map((item) => item.row);
      rows.push(...fallback);
    }

    const now = new Date().toISOString();
    for (const row of rows) this.db.run("UPDATE memories SET last_used_at = ? WHERE public_id = ?", [now, row.public_id]);
    if (auditActor) {
      this.audit("search", undefined, auditActor, {
        queryHash: sha256(normalized),
        scopes: scopes ?? ["user", "project", "board", "session"],
        resultCount: rows.length,
      });
    }
    return rows.map(toRecord);
  }

  recall(query: string, context: MemoryContext, limit: number): MemoryRecord[] {
    const relevant = this.search(query, context, undefined, limit, null);
    const filter = scopeFilter(context, ["user", "project", "board"]);
    const pinned = this.db.query(`
      SELECT m.public_id, m.scope, m.kind, m.content, m.created_at, m.updated_at, m.expires_at
      FROM memories m
      WHERE ${filter.sql}
        AND m.kind IN ('preference', 'instruction')
        AND (m.expires_at IS NULL OR m.expires_at > ?)
      ORDER BY m.updated_at DESC
      LIMIT 3
    `).all(...filter.values, new Date().toISOString()) as MemoryRow[];
    const merged = [...pinned.map(toRecord), ...relevant];
    const records = merged
      .filter((record, index) => merged.findIndex((item) => item.id === record.id) === index)
      .slice(0, limit);
    if (records.length > 0) {
      this.audit("recall", undefined, "agent", {
        queryHash: sha256(query),
        resultCount: records.length,
      });
    }
    return records;
  }

  list(context: MemoryContext, scope: MemoryScope | undefined, limit = 50): MemoryRecord[] {
    const filter = scopeFilter(context, scope ? [scope] : undefined);
    const rows = this.db.query(`
      SELECT m.public_id, m.scope, m.kind, m.content, m.created_at, m.updated_at, m.expires_at
      FROM memories m
      WHERE ${filter.sql}
        AND (m.expires_at IS NULL OR m.expires_at > ?)
      ORDER BY m.updated_at DESC
      LIMIT ?
    `).all(...filter.values, new Date().toISOString(), Math.max(1, Math.min(limit, 100))) as MemoryRow[];
    return rows.map(toRecord);
  }

  forget(id: string, context: MemoryContext, actor: string): boolean {
    const row = this.accessibleRow(id, context);
    if (!row) return false;
    this.db.run("DELETE FROM memories WHERE public_id = ?", [id]);
    this.audit("delete", id, actor, {
      scope: row.scope,
      kind: row.kind,
      contentHash: sha256(row.content),
    });
    this.db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
    return true;
  }

  clear(scope: MemoryScope, context: MemoryContext, actor: string): number {
    const key = scopeKey(scope, context);
    if (!key) return 0;
    const row = this.db.query("SELECT count(*) AS count FROM memories WHERE scope = ? AND scope_key = ?")
      .get(scope, key) as { count: number };
    this.db.run("DELETE FROM memories WHERE scope = ? AND scope_key = ?", [scope, key]);
    this.audit("clear", undefined, actor, { scope, count: row.count });
    this.db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
    this.db.exec("VACUUM");
    return row.count;
  }

  pruneExpired(actor: string): number {
    const now = new Date().toISOString();
    const row = this.db.query("SELECT count(*) AS count FROM memories WHERE expires_at IS NOT NULL AND expires_at <= ?")
      .get(now) as { count: number };
    if (row.count > 0) {
      this.db.run("DELETE FROM memories WHERE expires_at IS NOT NULL AND expires_at <= ?", [now]);
      this.audit("expire", undefined, actor, { count: row.count });
    }
    return row.count;
  }

  stats(context: MemoryContext): { total: number; byScope: Record<string, number>; databaseBytes: number } {
    const filter = scopeFilter(context);
    const rows = this.db.query(`
      SELECT m.scope, count(*) AS count
      FROM memories m
      WHERE ${filter.sql}
        AND (m.expires_at IS NULL OR m.expires_at > ?)
      GROUP BY m.scope
    `).all(...filter.values, new Date().toISOString()) as Array<{ scope: string; count: number }>;
    const byScope = Object.fromEntries(rows.map((row) => [row.scope, row.count]));
    return {
      total: rows.reduce((sum, row) => sum + row.count, 0),
      byScope,
      databaseBytes: statSync(this.path).size,
    };
  }

  events(limit = 25): MemoryEvent[] {
    const rows = this.db.query(`
      SELECT event, memory_public_id, actor, created_at, details
      FROM memory_events
      ORDER BY id DESC
      LIMIT ?
    `).all(Math.max(1, Math.min(limit, 100))) as Array<{
      event: string;
      memory_public_id: string | null;
      actor: string;
      created_at: string;
      details: string;
    }>;
    return rows.map((row) => ({
      event: row.event,
      ...(row.memory_public_id ? { memoryId: row.memory_public_id } : {}),
      actor: row.actor,
      createdAt: row.created_at,
      details: JSON.parse(row.details) as Record<string, unknown>,
    }));
  }

  close(): void {
    try {
      this.db.exec("PRAGMA wal_checkpoint(TRUNCATE)");
    } finally {
      this.db.close();
    }
  }
}
