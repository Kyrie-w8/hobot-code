import assert from 'node:assert/strict';
import test from 'node:test';

import {accessModePresentation} from './access-mode.js';

test('access mode names the common safe task profiles', () => {
  assert.deepEqual(accessModePresentation({permissionMode: 'review', sandboxMode: 'review', networkMode: 'model-only'}), {
    label: 'Review only', tone: 'standard', summary: 'Review only · Read only · Model only',
  });
  assert.deepEqual(accessModePresentation({permissionMode: 'ask', sandboxMode: 'workspace', networkMode: 'model-only'}), {
    label: 'Ask first', tone: 'standard', summary: 'Ask first · Workspace · Model only',
  });
  assert.deepEqual(accessModePresentation({permissionMode: 'developer', sandboxMode: 'workspace', networkMode: 'model-only'}), {
    label: 'Developer', tone: 'standard', summary: 'Developer · Workspace · Model only',
  });
});

test('access mode makes hardware and unsandboxed profiles explicit', () => {
  assert.equal(accessModePresentation({permissionMode: 'developer', sandboxMode: 'system', networkMode: 'shared'}).label, 'Board access');
  assert.equal(accessModePresentation({permissionMode: 'developer', sandboxMode: 'system', networkMode: 'shared'}).tone, 'elevated');
  assert.equal(accessModePresentation({permissionMode: 'developer', sandboxMode: 'off', networkMode: 'shared'}).label, 'Unrestricted');
  assert.equal(accessModePresentation({permissionMode: 'developer', sandboxMode: 'off', networkMode: 'shared'}).tone, 'danger');
});

test('access mode preserves non-standard combinations as a readable custom summary', () => {
  const presentation = accessModePresentation({permissionMode: 'review', sandboxMode: 'workspace', networkMode: 'offline'});
  assert.equal(presentation.label, 'Custom');
  assert.equal(presentation.summary, 'Review only · Workspace · Offline');
});
