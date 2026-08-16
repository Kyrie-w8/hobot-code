import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';

import {applyTheme, normalizeThemePreference, readThemePreference, resolveTheme, saveThemePreference, THEME_STORAGE_KEY} from './appearance-theme.js';

test('normalizes unknown and missing preferences to system', () => {
  assert.equal(normalizeThemePreference('dark'), 'dark');
  assert.equal(normalizeThemePreference('light'), 'light');
  assert.equal(normalizeThemePreference('sepia'), 'system');
  assert.equal(normalizeThemePreference(null), 'system');
});

test('system preference follows the operating system while explicit choices do not', () => {
  assert.equal(resolveTheme('system', true), 'dark');
  assert.equal(resolveTheme('system', false), 'light');
  assert.equal(resolveTheme('light', true), 'light');
  assert.equal(resolveTheme('dark', false), 'dark');
});

test('reads and saves the preference without failing when storage is unavailable', () => {
  const values = new Map();
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
  assert.equal(readThemePreference(storage), 'system');
  assert.equal(saveThemePreference(storage, 'light'), 'light');
  assert.equal(values.get(THEME_STORAGE_KEY), 'light');
  assert.equal(readThemePreference(storage), 'light');
  assert.equal(readThemePreference({getItem() { throw new Error('blocked'); }}), 'system');
  assert.equal(saveThemePreference({setItem() { throw new Error('blocked'); }}, 'dark'), 'dark');
});

test('applies both resolved and requested themes to the document root', () => {
  const root = {dataset: {}, style: {}};
  assert.equal(applyTheme(root, 'system', false), 'light');
  assert.deepEqual(root.dataset, {themePreference: 'system', theme: 'light'});
  assert.equal(root.style.colorScheme, 'light');
});

test('both palettes keep body and status text readable', () => {
  const css = readFileSync(new URL('./App.css', import.meta.url), 'utf8');
  const dark = css.match(/:root \{([\s\S]*?)\n\}/)?.[1] ?? '';
  const light = css.match(/:root\[data-theme='light'\] \{([\s\S]*?)\n\}/)?.[1] ?? '';
  const color = (block, name) => block.match(new RegExp(`--${name}:\\s*(#[0-9a-f]{6})`, 'i'))?.[1] ?? '';
  const channel = (value) => {
    const normalized = value / 255;
    return normalized <= 0.03928 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
  };
  const luminance = (hex) => {
    const channels = hex.match(/[0-9a-f]{2}/gi)?.map((value) => channel(Number.parseInt(value, 16))) ?? [];
    assert.equal(channels.length, 3, `expected a six-digit color, received ${hex}`);
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  };
  const contrast = (foreground, background) => {
    const values = [luminance(foreground), luminance(background)].sort((left, right) => right - left);
    return (values[0] + 0.05) / (values[1] + 0.05);
  };

  for (const palette of [dark, light]) {
    const background = color(palette, 'canvas');
    for (const token of ['text', 'text-muted', 'accent-strong', 'blue', 'green', 'amber', 'red']) {
      assert.ok(contrast(color(palette, token), background) >= 4.5, `${token} must retain 4.5:1 contrast`);
    }
  }
});
