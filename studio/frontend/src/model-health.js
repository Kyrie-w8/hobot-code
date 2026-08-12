export function currentModelHealth(health, model, now = Date.now()) {
  if (!health || !model) return undefined;
  if (health.provider !== model.provider || health.model !== model.id) return undefined;
  if (!Number.isFinite(Date.parse(health.expiresAt)) || Date.parse(health.expiresAt) <= now) return undefined;
  return health;
}

export function modelHealthLabel(category) {
  if (category === 'model-unavailable') return 'No route';
  if (category === 'authentication') return 'Auth failed';
  if (category === 'rate-limited') return 'Limited';
  if (category === 'timeout') return 'Timeout';
  if (category === 'network') return 'Offline';
  return 'Unavailable';
}
