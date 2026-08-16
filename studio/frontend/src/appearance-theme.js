export const THEME_STORAGE_KEY = 'hobot-code.appearance';

const preferences = new Set(['system', 'light', 'dark']);

export function normalizeThemePreference(value) {
  return preferences.has(value) ? value : 'system';
}

export function readThemePreference(storage) {
  try {
    return normalizeThemePreference(storage?.getItem(THEME_STORAGE_KEY));
  } catch {
    return 'system';
  }
}

export function saveThemePreference(storage, preference) {
  const normalized = normalizeThemePreference(preference);
  try {
    storage?.setItem(THEME_STORAGE_KEY, normalized);
  } catch {
    // A private or locked-down webview may reject storage; the active theme still works.
  }
  return normalized;
}

export function resolveTheme(preference, systemPrefersDark) {
  const normalized = normalizeThemePreference(preference);
  return normalized === 'system' ? (systemPrefersDark ? 'dark' : 'light') : normalized;
}

export function applyTheme(root, preference, systemPrefersDark) {
  const normalized = normalizeThemePreference(preference);
  const resolved = resolveTheme(normalized, systemPrefersDark);
  root.dataset.themePreference = normalized;
  root.dataset.theme = resolved;
  root.style.colorScheme = resolved;
  return resolved;
}
