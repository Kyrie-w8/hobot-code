import assert from 'node:assert/strict';
import test from 'node:test';

import {shouldToggleMaximise} from './titlebar-policy.js';

class TestElement {
  constructor(interactive = false) { this.interactive = interactive; }
  closest() { return this.interactive ? this : null; }
}

const originalElement = globalThis.Element;
globalThis.Element = TestElement;

test.after(() => { globalThis.Element = originalElement; });

test('double-clicking non-interactive titlebar content toggles maximise', () => {
  assert.equal(shouldToggleMaximise({button: 0, detail: 2, target: new TestElement(false)}), true);
});

test('titlebar controls and non-double-clicks do not toggle maximise', () => {
  assert.equal(shouldToggleMaximise({button: 0, detail: 2, target: new TestElement(true)}), false);
  assert.equal(shouldToggleMaximise({button: 0, detail: 1, target: new TestElement(false)}), false);
  assert.equal(shouldToggleMaximise({button: 1, detail: 2, target: new TestElement(false)}), false);
});
