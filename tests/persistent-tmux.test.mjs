import assert from "node:assert/strict";
import test from "node:test";

import {
  detachPersistentTmuxClient,
  parseTmuxClients,
  selectTmuxClient,
} from "../extensions/rdk/persistent-tmux.mjs";

test("persistent detach selects only the current Hobot Code client", async () => {
  const calls = [];
  const run = async (command, args) => {
    calls.push([command, args]);
    if (args[0] === "display-message") return { stdout: "hobot-code-main\n", stderr: "" };
    if (args[0] === "list-clients") {
      return { stdout: "/dev/pts/4\thobot-code-main\n/dev/pts/8\thobot-code-main\n", stderr: "" };
    }
    if (args[0] === "detach-client") return { stdout: "", stderr: "" };
    throw new Error(`unexpected tmux invocation: ${args.join(" ")}`);
  };

  const result = await detachPersistentTmuxClient({
    environment: { TMUX: "/tmp/tmux-0/hobot-code,123,0", TMUX_PANE: "%1" },
    readStdinTarget: async () => "/dev/pts/8",
    run,
  });
  assert.deepEqual(result, { session: "hobot-code-main", target: "/dev/pts/8" });
  assert.deepEqual(calls.at(-1), ["tmux", ["detach-client", "-t", "/dev/pts/8"]]);
});

test("persistent detach fails closed outside its managed tmux client", async () => {
  await assert.rejects(
    () => detachPersistentTmuxClient({ environment: {} }),
    /hobot persistent/,
  );
  await assert.rejects(
    () => detachPersistentTmuxClient({
      environment: { TMUX: "/tmp/tmux-0/ordinary,123,0", TMUX_PANE: "%1" },
    }),
    /hobot persistent/,
  );
  assert.equal(selectTmuxClient(parseTmuxClients(
    "/dev/pts/1\thobot-code-main\n/dev/pts/2\thobot-code-main\n",
  ), "hobot-code-main", "/dev/pts/9"), undefined);
  assert.equal(selectTmuxClient(parseTmuxClients(
    "/dev/pts/1\thobot-code-main\n",
  ), "hobot-code-main", "/dev/pts/9"), undefined);
});
