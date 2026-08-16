export type ThemePreference = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export const THEME_STORAGE_KEY: string;
export function normalizeThemePreference(value: unknown): ThemePreference;
export function readThemePreference(storage?: Pick<Storage, 'getItem'>): ThemePreference;
export function saveThemePreference(storage: Pick<Storage, 'setItem'> | undefined, preference: ThemePreference): ThemePreference;
export function resolveTheme(preference: ThemePreference, systemPrefersDark: boolean): ResolvedTheme;
export function applyTheme(root: HTMLElement, preference: ThemePreference, systemPrefersDark: boolean): ResolvedTheme;
