export function currentModelRDKMatrix(matrix, model) {
	if (!matrix || !model) return undefined;
	return matrix.provider === model.provider && matrix.model === model.id ? matrix : undefined;
}

export function rdkProfileState(profile, activeProfile = '') {
	if (!profile) return 'idle';
	if (profile.availability === 'planned') return 'planned';
	if (profile.availability === 'unsupported-target') return 'unsupported';
	if (activeProfile === profile.id) return 'running';
	if (profile.evidenceState === 'stale') return 'stale';
	if (profile.result?.status === 'failed') return 'failed';
	if (profile.result?.status === 'passed') return profile.result.releaseEligible ? 'passed' : 'partial';
	return 'idle';
}

export function rdkProfileEvidenceLabel(profile) {
	const state = rdkProfileState(profile);
	if (state === 'planned') return 'Planned';
	if (state === 'unsupported') return 'Unavailable on this target';
	if (state === 'stale') return 'Retest needed';
	if (state === 'failed') return 'Failed';
	if (state === 'passed') return 'Release evidence';
	if (state === 'partial') return 'Development evidence';
	return 'Not tested';
}
