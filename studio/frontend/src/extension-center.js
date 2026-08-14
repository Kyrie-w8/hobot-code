const knownKinds = new Set(['extension', 'skill', 'provider', 'integration', 'package', 'prompt', 'theme']);

export function extensionKindLabel(kind) {
  return ({extension: 'Extension', skill: 'Skill', provider: 'Provider', integration: 'Integration', package: 'Package', prompt: 'Prompt', theme: 'Theme'})[kind] ?? 'Capability';
}

export function extensionTargetState(entry, boardId) {
  const target = String(boardId ?? '').trim().toLowerCase();
  const targets = Array.isArray(entry?.targets) ? entry.targets.map((value) => String(value).toLowerCase()) : [];
  if (target && targets.length > 0 && !targets.includes(target)) return {state: 'unavailable', label: `Not for ${target.toUpperCase()}`};
  if (entry?.required) return {state: 'required', label: 'Product required'};
  if (entry?.status === 'missing') return {state: 'unavailable', label: 'Command missing'};
  if (entry?.status === 'disabled') return {state: 'listed', label: 'Disabled'};
  if (entry?.status === 'available') return {state: 'included', label: 'Available'};
  if (entry?.status === 'configured') return {state: 'included', label: 'Configured'};
  if (entry?.status === 'discovered') return {state: 'included', label: 'Discovered'};
  if (entry?.status === 'declared') return {state: 'listed', label: 'Declared'};
  if (entry?.defaultEnabled) return {state: 'included', label: 'Included'};
  return {state: 'listed', label: 'Available'};
}

export function extensionCatalogHealth(catalog) {
  const issues = [];
  if (catalog?.schemaVersion !== 1 || catalog?.apiVersion !== 'hobot.extensions/v1') issues.push('Unsupported catalog format');
  if (!catalog?.productVersion || !catalog?.hostVersion || catalog.productVersion !== catalog.hostVersion) issues.push('Catalog and board versions differ');
  if (!catalog?.policy?.inventoryOnly || catalog?.policy?.permissionAuthority !== 'board') issues.push('Board-side policy boundary is not declared');
  if (catalog?.policy?.hotReload) issues.push('Unisolated hot reload is enabled');
  if (!Array.isArray(catalog?.entries) || catalog.entries.length === 0) issues.push('No packaged capabilities were reported');
  const unhealthySources = (Array.isArray(catalog?.diagnostics) ? catalog.diagnostics : [])
    .filter((diagnostic) => !['ok', 'missing', 'contextual', 'untrusted'].includes(diagnostic.status));
  if (unhealthySources.length > 0) issues.push(`${unhealthySources.length} configured source${unhealthySources.length === 1 ? '' : 's'} could not be inspected`);
  return {healthy: issues.length === 0, issues};
}

export function extensionCatalogSummary(catalog, boardId) {
  const entries = Array.isArray(catalog?.entries) ? catalog.entries : [];
  const supported = entries.filter((entry) => extensionTargetState(entry, boardId).state !== 'unavailable').length;
  return {
    total: entries.length,
    supported,
    required: entries.filter((entry) => entry.required).length,
    skills: entries.filter((entry) => entry.kind === 'skill').length,
  };
}

export function filterExtensions(entries, query = '', kind = 'all') {
  const needle = String(query).trim().toLowerCase();
  return (Array.isArray(entries) ? entries : [])
    .filter((entry) => kind === 'all' || (knownKinds.has(kind) && entry.kind === kind))
    .filter((entry) => !needle || [entry.id, entry.name, entry.description, ...(entry.provides ?? []), ...(entry.permissions ?? []), ...(entry.targets ?? [])]
      .some((value) => String(value).toLowerCase().includes(needle)))
    .slice()
    .sort((left, right) => {
      if (left.required !== right.required) return left.required ? -1 : 1;
      if (left.kind !== right.kind) return String(left.kind).localeCompare(String(right.kind));
      return String(left.name).localeCompare(String(right.name));
    });
}
