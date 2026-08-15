export function friendlyError(value) {
  const message = String(value).replace(/^Error:\s*/i, '').replace(/^task_[a-z_]+:\s*/i, '');
  if (/board returned invalid diagnostics/i.test(message)) return 'Board diagnostics use an incompatible format. Update Hobot Code on the board, reconnect, and run the check again.';
  if (/models_qualification_write_failed|refuse invalid qualification evidence/i.test(message)) return 'The model check completed, but its verification result could not be saved. Update Hobot Code on the board and run the model check again.';
  if (/board configuration changed|configuration-restart-required/i.test(message)) return 'The board configuration changed. Run `hobot daemon restart` on the board, then reconnect.';
  if (/context deadline exceeded|operation timed out|connect to host .* timed out/i.test(message)) return 'Could not reach the board. Check the network or VPN and try again.';
  if (/requires a newer Hobot Code event schema/i.test(message)) return 'Update the board-side Hobot Code and reconnect.';
  if (/has no resumable Hobot Code session/i.test(message)) return 'This task has no saved session. Start a new session instead.';
  if (/background task limit reached.*all agents are currently working/i.test(message)) return 'This older board service cannot queue tasks. Update Hobot Code on the board, or stop a working agent.';
  return message;
}
