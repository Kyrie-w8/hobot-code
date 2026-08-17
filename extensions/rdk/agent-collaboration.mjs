import { lstat, readFile, readdir } from "node:fs/promises";
import { isAbsolute, join, relative, resolve } from "node:path";

const TASK_ID = /^[0-9a-f]{24}$/;
const MAX_TASKS = 1_000;
const MAX_METADATA_BYTES = 128 * 1024;
const MAX_EPHEMERAL_BYTES = 16 * 1024;
const MAX_EPHEMERAL_AGE_MS = 10_000;
const LIVE_STATUSES = new Set(["queued", "starting", "idle", "running", "waiting", "stopping"]);
const MAIN_ACTIVE_STATUSES = new Set(["starting", "running", "waiting"]);
const VALID_STATUSES = new Set([...LIVE_STATUSES, "stopped", "failed", "interrupted"]);
const VALID_ACTIVITIES = /^(?:thinking|responding|waiting for approval|using [A-Za-z0-9_.:-]{1,80})$/;

function boundedText(value, maximum) {
  const text = typeof value === "string" ? value.trim() : "";
  return text.length <= maximum ? text : text.slice(0, maximum);
}

function pathsOverlap(left, right) {
  if (!isAbsolute(left) || !isAbsolute(right)) return false;
  const physicalLeft = resolve(left);
  const physicalRight = resolve(right);
  const leftToRight = relative(physicalLeft, physicalRight);
  const rightToLeft = relative(physicalRight, physicalLeft);
  return leftToRight === "" || (!leftToRight.startsWith("..") && !isAbsolute(leftToRight))
    || (!rightToLeft.startsWith("..") && !isAbsolute(rightToLeft));
}

async function readMetadata(tasksRoot, entry) {
  if (!entry.isDirectory() || !TASK_ID.test(entry.name)) return undefined;
  const path = join(tasksRoot, entry.name, "metadata.json");
  try {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size > MAX_METADATA_BYTES || (info.mode & 0o077) !== 0) return undefined;
    const value = JSON.parse(await readFile(path, "utf8"));
    if (value?.id !== entry.name || !VALID_STATUSES.has(value.status) || !isAbsolute(value.cwd)) return undefined;
    if (value.parentTaskId && !TASK_ID.test(value.parentTaskId)) return undefined;
    if (value.sourceTaskId && !TASK_ID.test(value.sourceTaskId)) return undefined;
    if (value.branchKind && !["side", "edit"].includes(value.branchKind)) return undefined;
    const activity = boundedText(value.currentActivity, 96);
    return {
      id: value.id,
      name: boundedText(value.name, 96) || "Untitled task",
      cwd: resolve(value.cwd),
      status: value.status,
      activity: VALID_ACTIVITIES.test(activity) ? activity : "",
      parentTaskId: value.parentTaskId || "",
      branchKind: value.branchKind || "",
      updatedAt: typeof value.updatedAt === "string" ? value.updatedAt : "",
    };
  } catch {
    return undefined;
  }
}

function familyRoot(task, byId) {
  let current = task;
  const seen = new Set();
  for (let depth = 0; depth < 32 && current?.parentTaskId && !seen.has(current.id); depth += 1) {
    seen.add(current.id);
    const parent = byId.get(current.parentTaskId);
    if (!parent) break;
    current = parent;
  }
  return current?.id || task.id;
}

function statusPriority(task) {
  if (MAIN_ACTIVE_STATUSES.has(task.status)) return 3;
  if (task.status === "idle") return 2;
  if (task.status === "queued" || task.status === "stopping") return 1;
  return 0;
}

function newestPreferred(left, right) {
  const priority = statusPriority(right) - statusPriority(left);
  if (priority !== 0) return priority;
  return Date.parse(right.updatedAt || "") - Date.parse(left.updatedAt || "");
}

export async function readAgentCollaboration({ stateRoot, currentTaskId } = {}) {
  if (!isAbsolute(stateRoot || "") || !TASK_ID.test(String(currentTaskId || ""))) return undefined;
  const tasksRoot = join(resolve(stateRoot), "agentd", "tasks");
  let entries;
  try {
    const info = await lstat(tasksRoot);
    if (!info.isDirectory() || info.isSymbolicLink()) return undefined;
    entries = await readdir(tasksRoot, { withFileTypes: true });
  } catch {
    return undefined;
  }
  if (entries.length > MAX_TASKS + 8) return undefined;
  const tasks = (await Promise.all(entries.slice(0, MAX_TASKS).map((entry) => readMetadata(tasksRoot, entry)))).filter(Boolean);
  const byId = new Map(tasks.map((task) => [task.id, task]));
  const current = byId.get(currentTaskId);
  if (!current) return undefined;
  const rootId = familyRoot(current, byId);
  const family = tasks.filter((task) => familyRoot(task, byId) === rootId);
  const role = current.branchKind === "side" ? "side" : "main";
  const mains = family.filter((task) => task.branchKind !== "side").sort(newestPreferred);
  const main = (role === "main" ? current : mains[0]) || current;
  const sides = family.filter((task) => task.branchKind === "side" && task.id !== current.id);
  const activeSides = sides.filter((task) => LIVE_STATUSES.has(task.status));
  return {
    schemaVersion: 1,
    role,
    current: { name: current.name, status: current.status, activity: current.activity, cwd: current.cwd },
    main: { name: main.name, status: main.status, activity: main.activity, cwd: main.cwd },
    mainActive: MAIN_ACTIVE_STATUSES.has(main.status),
    sharedWorkspace: pathsOverlap(current.cwd, main.cwd),
    sideAgents: {
      active: activeSides.length + (role === "side" && LIVE_STATUSES.has(current.status) ? 1 : 0),
      total: sides.length + (role === "side" ? 1 : 0),
    },
  };
}

export async function readEphemeralSideCollaboration(path) {
  if (!isAbsolute(path || "")) return undefined;
  try {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size > MAX_EPHEMERAL_BYTES || (info.mode & 0o077) !== 0) return undefined;
    const value = JSON.parse(await readFile(path, "utf8"));
    const main = value?.main;
    if (value?.schemaVersion !== 1 || value.role !== "side" || !main || !VALID_STATUSES.has(main.status) || !isAbsolute(main.cwd)) return undefined;
    const updatedAt = Date.parse(value.updatedAt || "");
    const age = Date.now() - updatedAt;
    if (!Number.isFinite(updatedAt) || age < -MAX_EPHEMERAL_AGE_MS || age > MAX_EPHEMERAL_AGE_MS) return undefined;
    const activity = boundedText(main.activity, 96);
    return {
      schemaVersion: 1,
      role: "side",
      current: { name: "Side Agent", status: "running", activity: "", cwd: resolve(main.cwd) },
      main: {
        name: boundedText(main.name, 96) || "Main Agent",
        status: main.status,
        activity: VALID_ACTIVITIES.test(activity) ? activity : "",
        cwd: resolve(main.cwd),
      },
      mainActive: MAIN_ACTIVE_STATUSES.has(main.status),
      sharedWorkspace: true,
      sideAgents: { active: 1, total: 1 },
    };
  } catch {
    return undefined;
  }
}

export function formatAgentCollaboration(snapshot) {
  if (!snapshot) return "";
  const activity = snapshot.main.activity ? `; current activity: ${snapshot.main.activity}` : "";
  if (snapshot.role === "side") {
    return [
      "## Live Agent collaboration",
      `Role: Side Agent. The Main Agent \"${snapshot.main.name}\" is ${snapshot.main.status}${activity}. This status is refreshed at the start of every turn.`,
      snapshot.sharedWorkspace
        ? "You share its workspace. The Main Agent has write priority while it is active; stay read-only until it settles, or use a separately isolated workspace."
        : "Your workspace is isolated from the Main Agent, so file writes may proceed subject to normal leases and safety policy.",
      "Files, processes, model capacity, and RDK devices are real shared resources. Never bypass a workspace or hardware lease, stop or approve the Main Agent, or assume its in-flight plan is unchanged.",
      "Your conversation is independent and is not merged back. Report useful conclusions to the user; persistent side effects remain visible to other Agents.",
    ].join("\n");
  }
  return [
    "## Live Agent collaboration",
    `Role: Main Agent. ${snapshot.sideAgents.active} Side Agent(s) are active in this task family.`,
    "You retain priority for writes in the shared workspace. Re-read files before mutation because isolated or previously completed Side Agents may still have produced persistent side effects.",
    "Do not assume Side Agent conclusions are merged into this conversation; use live files, task status, and resource leases as authoritative evidence.",
  ].join("\n");
}

export function sideAgentWriteConflict(snapshot) {
  return Boolean(snapshot?.role === "side" && snapshot.mainActive && snapshot.sharedWorkspace);
}

export function sideAgentWorkspaceWriteBlocked(isSideAgent, snapshot) {
  return Boolean(isSideAgent && (!snapshot || sideAgentWriteConflict(snapshot)));
}
