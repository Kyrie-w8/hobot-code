export function currentModelProbe(result, model) {
  if (!result || !model) return undefined;
  if (result.provider !== model.provider || result.model !== model.id) return undefined;
  return result;
}

export function currentModelQualification(result, model) {
  if (!result || !model || result.provider !== model.provider || result.model !== model.id) return undefined;
  return result;
}

export function qualificationLayer(qualification, layer, value, now = Date.now()) {
  const expiredByTime = (layer === 'route' || layer === 'protocol') && value?.expiresAt && Date.parse(value.expiresAt) <= now;
  if (!qualification || !value || expiredByTime || qualification.staleLayers?.includes(layer) || qualification.expiredLayers?.includes(layer)) return undefined;
  return value;
}

export function qualificationExpirations(qualification, now = Date.now()) {
  if (!qualification) return [];
  const expired = new Set(qualification.expiredLayers || []);
  if (qualification.health?.expiresAt && Date.parse(qualification.health.expiresAt) <= now) expired.add('route');
  if (qualification.conformance?.expiresAt && Date.parse(qualification.conformance.expiresAt) <= now) expired.add('protocol');
  return [...expired];
}

export function qualificationEvidenceNotice(qualification) {
  if (!qualification || qualification.state === 'untested') return '';
  if (qualification.state === 'stale') {
    const labels = {'configuration-changed': 'model configuration', 'product-version-changed': 'product version', 'build-changed': 'board service build', 'pi-runtime-changed': 'Pi runtime', 'board-changed': 'board or RDK OS', 'rdk-resources-changed': 'RDK prompt, extension, or knowledge'};
    const changed = qualification.staleReasons.map((reason) => labels[reason] || reason).join(', ');
    return `Saved evidence needs retesting because the ${changed} changed.`;
  }
  if (qualification.expiredLayers?.length) return `Saved ${qualification.expiredLayers.join(' and ')} evidence expired. Run those layers again for a current result.`;
  return qualification.updatedAt ? `Restored private board evidence from ${new Date(qualification.updatedAt).toLocaleString()}.` : 'Restored private board evidence.';
}

export function modelReadinessPresentation({health, conformance, runtime, rdk, evidenceState, running = false}) {
  if (running) return {label: 'Testing', tone: 'running', title: 'A bounded model qualification probe is running on the board.'};
  if (rdk?.status === 'failed' || runtime?.status === 'failed' || conformance?.status === 'failed' || health?.status === 'unavailable') {
    return {label: 'Needs attention', tone: 'failed', title: 'At least one completed readiness layer failed.'};
  }
  if (rdk?.status === 'passed') {
    return rdk.releaseEligible
      ? {label: 'Profile qualified', tone: 'passed', title: 'One named RDK profile passed with release-eligible evidence. Broader RDK workflows remain outside this result.'}
      : {label: 'Profile passed', tone: 'partial', title: 'One named RDK profile passed, but this build is not eligible as public release evidence.'};
  }
  if (runtime) return {label: 'Runtime tested', tone: runtime.status === 'partial' ? 'partial' : 'passed', title: 'The isolated Agent runtime suite ran. Broader RDK workflows remain outside this result.'};
  if (conformance) return {label: conformance.status === 'compatible' ? 'Protocol fallback' : 'Protocol OK', tone: conformance.status === 'compatible' ? 'partial' : 'passed', title: 'The model gateway protocol was tested; Agent behavior and RDK work were not.'};
  if (health?.status === 'available') return {label: 'Route available', tone: 'passed', title: 'The model route answered a minimal request; tools and RDK work were not tested.'};
  if (evidenceState === 'stale') return {label: 'Retest needed', tone: 'partial', title: 'Saved evidence no longer matches the current model, build, board, or RDK resources.'};
  if (evidenceState === 'expired') return {label: 'Checks expired', tone: 'partial', title: 'Saved short-lived route or protocol evidence has expired.'};
  return {label: 'Readiness', tone: 'idle', title: 'Inspect model availability, protocol behavior, Agent runtime behavior, and one bounded RDK profile.'};
}
