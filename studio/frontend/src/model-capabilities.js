export function effectiveModel(models, selection) {
  if (selection) return models.find((model) => `${model.provider}/${model.id}` === selection);
  return models.find((model) => model.default) ?? models[0];
}

export function modelAcceptsImages(models, selection) {
  return effectiveModel(models, selection)?.capabilities?.imageInput === true;
}
