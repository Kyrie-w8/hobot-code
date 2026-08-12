export function preferredDeploymentArtifact(artifacts) {
  return artifacts.find((artifact) => artifact.compatibility === 'candidate')
    ?? artifacts.find((artifact) => artifact.compatibility !== 'mismatch')
    ?? null;
}

export function deploymentCanStart(artifact) {
  return Boolean(artifact && artifact.compatibility !== 'mismatch');
}

export function deploymentCompatibilityLabel(value) {
  return ({candidate: 'Board candidate', unverified: 'Target unverified', 'conversion-required': 'Conversion required', mismatch: 'Different board'})[value] ?? value;
}

export function deploymentPhaseLabel(value) {
  return ({checking: 'Checking report', running: 'Deployment running', passed: 'Verified deployment', partial: 'Partially completed', failed: 'Deployment failed', incomplete: 'Report missing', 'invalid-report': 'Report rejected'})[value] ?? value;
}

export function deploymentProfileFor(artifact, boardId) {
  if (!artifact || boardId !== 'x5') return '';
  const name = artifact.name.toLowerCase();
  if (name.includes('regnet') && name.includes('400mf')) return 'regnet-x-400mf-x5';
  if (name.includes('rt_igev') || name.includes('rt-igev')) return 'rt-igev-x5';
  if (name.includes('mobileone') && name.includes('s0')) return 'mobileone-s0-x5';
  return '';
}
