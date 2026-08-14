export function currentModelConformance(result, model, now = Date.now()) {
  if (!result || !model) return undefined;
  if (result.provider !== model.provider || result.model !== model.id) return undefined;
  if (!Number.isFinite(Date.parse(result.expiresAt)) || Date.parse(result.expiresAt) <= now) return undefined;
  return result;
}

export function modelConformancePresentation(result, probing = false) {
  if (probing) {
    return {label: 'Probing', title: 'Probing the model gateway protocol. RDK task quality is not part of this check.'};
  }
  if (!result) {
    return {label: 'Probe', title: 'Probe gateway streaming, tools, continuation, and declared input modes. This does not test RDK task quality.'};
  }
  const label = result.status === 'verified' ? 'Protocol OK' : result.status === 'compatible' ? 'Fallback' : 'Protocol failed';
  const checks = Array.isArray(result.checks) ? result.checks.map((check) => `${check.name}: ${check.status}`).join(', ') : '';
  const scope = result.scope || 'gateway-protocol';
  const runtimeStatus = result.agentRuntimeStatus || 'not-tested';
  const rdkTaskStatus = result.rdkTaskStatus || 'not-tested';
  return {
    label,
    title: `${result.message} Scope: ${scope}; Agent runtime: ${runtimeStatus}; RDK tasks: ${rdkTaskStatus}.${checks ? ` ${checks}.` : ''} Click to probe again.`,
  };
}
