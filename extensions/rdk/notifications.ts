export interface NotificationConfig {
  schemaVersion: 1;
  enabled: boolean;
  allowLocal: boolean;
  bell: boolean;
  protocol: "osc9" | "osc777" | "both";
  onApproval: boolean;
  onComplete: boolean;
  onFailure: boolean;
  minDurationMs: number;
}

function safeTerminalText(value: string, maxLength: number): string {
  return String(value ?? "")
    .replace(/[\x00-\x08\x0B-\x1F\x7F]/g, " ")
    .replace(/;/g, ",")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, maxLength);
}

export function emitTerminalNotification(
  config: NotificationConfig,
  title: string,
  message: string,
  interactiveTui = true,
): boolean {
  if (!config.enabled) return false;
  if (!interactiveTui) return false;
  if (!process.env.SSH_CONNECTION && !config.allowLocal) return false;
  if (!process.stderr.isTTY) return false;
  const safeTitle = safeTerminalText(title, 80) || "Hobot Code";
  const safeMessage = safeTerminalText(message, 240) || "Agent status changed";
  let sequence = "";
  if (config.protocol === "osc9" || config.protocol === "both") {
    sequence += `\x1b]9;${safeTitle}: ${safeMessage}\x07`;
  }
  if (config.protocol === "osc777" || config.protocol === "both") {
    sequence += `\x1b]777;notify;${safeTitle};${safeMessage}\x07`;
  }
  if (config.bell) sequence += "\x07";
  process.stderr.write(sequence);
  return true;
}
