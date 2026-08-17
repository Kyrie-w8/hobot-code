import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';

const css = readFileSync(new URL('./App.css', import.meta.url), 'utf8');

function rule(selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const match = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  assert.ok(match, `expected ${selector} rule`);
  return match[1];
}

test('typography tokens establish readable Studio baselines', () => {
  const root = rule(':root');
  for (const [name, value] of Object.entries({
    'font-size-caption': '11px',
    'font-size-meta': '12px',
    'font-size-ui': '13px',
    'font-size-control': '14px',
    'font-size-body': '16px',
    'font-size-heading': '18px',
    'font-size-title': '22px',
  })) {
    assert.match(root, new RegExp(`--${name}:\\s*${value}`));
  }
  for (const name of ['line-height-tight', 'line-height-ui', 'line-height-body', 'line-height-code']) {
    assert.match(root, new RegExp(`--${name}:`));
  }
});

test('conversation, composer, navigation, tools, and forms use their intended tiers', () => {
  for (const selector of ['.user-message-content', '.assistant-content', '.composer textarea']) {
    const declaration = rule(selector);
    assert.match(declaration, /font-size:\s*var\(--font-size-body\)/);
    assert.match(declaration, /line-height:\s*var\(--line-height-body\)/);
  }

  for (const selector of ['.task-row-name', '.project-toggle span', '.form-grid input, .form-grid textarea, .form-grid select']) {
    assert.match(rule(selector), /font-size:\s*var\(--font-size-control\)/);
  }
  assert.match(rule('.task-title-line h1'), /font-size:\s*var\(--font-size-body\)/);

  for (const selector of ['.tool-detail pre', '.markdown pre code']) {
    assert.match(rule(selector), /font(?:-size)?:[^;]*var\(--font-size-ui\)/);
  }

  assert.match(rule('.status'), /font-size:\s*var\(--font-size-meta\)/);
  assert.match(rule('.workspace-mode-badge'), /font-size:\s*var\(--font-size-caption\)/);
});

test('high-frequency composer controls remain readable without growing the footer unpredictably', () => {
  const footer = rule('.composer-footer');
  assert.match(footer, /min-height:\s*38px/);

  const picker = rule('.model-picker, .permission-picker');
  const select = rule('.model-picker select, .permission-picker select');
  const accessMode = rule('.access-mode-button');
  for (const declaration of [picker, select, accessMode]) {
    assert.match(declaration, /height:\s*30px/);
  }
  for (const declaration of [select, accessMode]) {
    assert.match(declaration, /font-size:\s*var\(--font-size-ui\)/);
  }
});

test('Studio does not retain sub-11px fixed typography or shrink text at narrow widths', () => {
  assert.doesNotMatch(css, /font-size:\s*(?:8|9|10)px/);
  assert.doesNotMatch(css, /font:\s*[^;]*(?:8|9|10)px/);

  const responsive = css.slice(css.indexOf('@media (max-width: 1100px)'));
  assert.doesNotMatch(responsive, /font(?:-size)?:/);
});
