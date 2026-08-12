const genericMessages = new Set([
  'Choose how Hobot Code may run this tool.',
]);

export function approvalPresentation(approval) {
  const titleLines = String(approval?.title ?? '')
    .split('\n')
    .map((line) => line.trimEnd());
  const title = titleLines.shift()?.trim() || 'Approval needed';
  const titleDetail = titleLines.join('\n').trim();
  const message = String(approval?.message ?? '').trim();
  const detailParts = [];
  if (titleDetail) detailParts.push(titleDetail);
  if (message && !genericMessages.has(message) && message !== titleDetail) detailParts.push(message);
  return {
    title,
    detail: detailParts.join('\n\n') || 'Review this tool request before continuing.',
    remembersExactCall: Array.isArray(approval?.options)
      && approval.options.some((option) => /exact call/i.test(String(option))),
  };
}

export function approvalResponse(method, action, value = '') {
  if (action === 'cancel') return {cancelled: true};
  if (method === 'confirm') return {confirmed: action === 'confirm'};
  if (method === 'select' || method === 'input' || method === 'editor') return {value: String(value)};
  throw new Error('Unsupported approval method.');
}
