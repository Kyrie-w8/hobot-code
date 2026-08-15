const providerNames = {
  drobotics: 'D-Robotics',
};

export function includedProviderGroups(models, managedProviders = []) {
  const managed = new Set(managedProviders.map((provider) => provider?.id).filter(Boolean));
  const groups = new Map();

  for (const model of models ?? []) {
    if (!model?.provider || !model?.id || model.managed === true || managed.has(model.provider)) continue;
    if (!groups.has(model.provider)) {
      groups.set(model.provider, {
        id: model.provider,
        name: providerNames[model.provider] ?? model.provider,
        models: [],
      });
    }
    const group = groups.get(model.provider);
    if (!group.models.some((candidate) => candidate.id === model.id)) group.models.push(model);
  }

  return [...groups.values()]
    .map((group) => ({...group, models: group.models.sort(compareModels)}))
    .sort((left, right) => (left.id === 'drobotics' ? -1 : right.id === 'drobotics' ? 1 : left.name.localeCompare(right.name)));
}

export function includedModelSummary(model) {
  const capabilities = [];
  if (model?.capabilities?.reasoning === true) capabilities.push('Reasoning');
  if (model?.capabilities?.imageInput === true) capabilities.push('Images');
  if (model?.default === true) capabilities.push('Default');
  return capabilities.join(' · ') || 'Text';
}

function compareModels(left, right) {
  if (left.default === true && right.default !== true) return -1;
  if (right.default === true && left.default !== true) return 1;
  return (left.name || left.id).localeCompare(right.name || right.id);
}
