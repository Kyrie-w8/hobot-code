const hardwareConfidenceIssues = new Set([
  'board-unverified',
  'rdk-os-unknown',
  'rdk-os-line-mismatch',
  'rdk-os-unvalidated-version',
  'snapshot-unavailable',
  'snapshot-missing',
]);

const featureIssues = new Set([
  'legacy-event-schema',
  'version-line-mismatch',
]);

export function compatibilityPresentation(compatibility) {
  const issues = Array.isArray(compatibility?.issues) ? compatibility.issues : [];
  const codes = new Set(issues.map((issue) => issue.code));
  const firstAction = issues.find((issue) => issue.action)?.action ?? '';

  if (compatibility?.status === 'upgrade-required') {
    return {
      label: 'Update required',
      title: 'Update the board before using Studio',
      description: 'Studio stopped this connection because the board service cannot provide the required safety and task guarantees.',
      action: firstAction,
      tone: 'danger',
    };
  }
  if (compatibility?.status === 'supported') {
    return {
      label: 'Ready',
      title: 'Ready for RDK development',
      description: 'Daily Agent work and board-specific workflows are available on this validated setup.',
      action: '',
      tone: 'healthy',
    };
  }

  const hardwareOnly = issues.length > 0 && issues.every((issue) => hardwareConfidenceIssues.has(issue.code));
  if (hardwareOnly) {
    return {
      label: 'Hardware unverified',
      title: 'Daily Agent work is available',
      description: 'Use chat, code, files, and shell normally. Validate this exact board and RDK OS before relying on hardware-specific production results.',
      action: firstAction,
      tone: 'warning',
    };
  }
  const needsUpdate = issues.some((issue) => issue.code.startsWith('missing-') || featureIssues.has(issue.code));
  if (needsUpdate) {
    return {
      label: 'Update recommended',
      title: 'Core Agent work is available',
      description: 'This board can run tasks, but one or more newer Studio features are unavailable.',
      action: firstAction,
      tone: 'warning',
    };
  }
  return {
    label: 'Limited',
    title: 'Connected with safeguards',
    description: 'Core Agent work remains available. Review the detected limits before hardware-specific production use.',
    action: firstAction,
    tone: 'warning',
  };
}

export function compatibilityTargetLabel(compatibility) {
  if (!compatibility?.boardId) return 'Board identity unavailable';
  const target = `${compatibility.boardId.toUpperCase()}${compatibility.rdkOsVersion ? ` · RDK OS ${compatibility.rdkOsVersion}` : ''}`;
  return compatibility.validatedTarget ? `${target} · validated` : `${target} · not validated`;
}
