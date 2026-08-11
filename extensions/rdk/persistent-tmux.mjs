import { execFile } from "node:child_process";
import { basename } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const HOBOT_SESSION_PATTERN = /^hobot-code-[A-Za-z0-9][A-Za-z0-9_-]*$/;

export function parseTmuxClients(value) {
  return String(value ?? "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const separator = line.indexOf("\t");
      return separator < 0
        ? { tty: line, session: "" }
        : { tty: line.slice(0, separator), session: line.slice(separator + 1) };
    });
}

export function selectTmuxClient(clients, session, tty) {
  const sessionClients = clients.filter((client) => client.session === session);
  const exact = sessionClients.find((client) => client.tty === tty);
  return exact?.tty;
}

export async function detachPersistentTmuxClient(options = {}) {
  const environment = options.environment ?? process.env;
  const tmux = String(environment.TMUX ?? "");
  const pane = String(environment.TMUX_PANE ?? "");
  const socket = tmux.split(",", 1)[0];
  if (!socket || basename(socket) !== "hobot-code" || !/^%\d+$/.test(pane)) {
    throw new Error("/detach is available only inside a hobot persistent session");
  }

  const run = options.run ?? execFileAsync;
  const [{ stdout: currentOutput }, { stdout: clientsOutput }] = await Promise.all([
    run("tmux", ["display-message", "-p", "-t", pane, "#{session_name}\t#{client_tty}"]),
    run("tmux", ["list-clients", "-F", "#{client_tty}\t#{session_name}"]),
  ]);
  const current = String(currentOutput ?? "").trim();
  const separator = current.indexOf("\t");
  const session = separator < 0 ? "" : current.slice(0, separator);
  const tty = separator < 0 ? "" : current.slice(separator + 1);
  if (!HOBOT_SESSION_PATTERN.test(session)) {
    throw new Error("Current tmux session is not managed by Hobot Code");
  }

  const target = selectTmuxClient(parseTmuxClients(clientsOutput), session, tty);
  if (!target) throw new Error("Cannot identify the current attached terminal safely");
  await run("tmux", ["detach-client", "-t", target]);
  return { session, target };
}
