import assert from 'node:assert/strict';
import test from 'node:test';

import {approvalPresentation, approvalResponse} from './approval-model.js';

test('approval presentation preserves security detail over generic messages', () => {
  const view = approvalPresentation({
    title: 'Allow bash?\n\nTool: bash\nRisk: root shell\nTarget:\npwd\nReason: policy',
    message: 'Choose how Hobot Code may run this tool.',
    options: ['Allow once', 'Allow network for this task', 'Deny'],
  });
  assert.equal(view.title, 'Allow bash?');
  assert.match(view.detail, /Tool: bash[\s\S]*Target:[\s\S]*pwd[\s\S]*Reason: policy/);
  assert.doesNotMatch(view.detail, /Choose how/);
  assert.equal(view.remembersExactCall, false);
});

test('approval presentation retains non-generic backend guidance', () => {
  const view = approvalPresentation({title: 'Confirm?', message: 'Read the deployment plan first.'});
  assert.equal(view.detail, 'Read the deployment plan first.');
  assert.equal(view.remembersExactCall, false);
});

test('approval responses match all supported request methods', () => {
  assert.deepEqual(approvalResponse('confirm', 'confirm'), {confirmed: true});
  assert.deepEqual(approvalResponse('confirm', 'deny'), {confirmed: false});
  assert.deepEqual(approvalResponse('select', 'select', 'Allow once'), {value: 'Allow once'});
  assert.deepEqual(approvalResponse('input', 'submit', 'S600'), {value: 'S600'});
  assert.deepEqual(approvalResponse('editor', 'submit', 'line one\nline two'), {value: 'line one\nline two'});
  assert.deepEqual(approvalResponse('editor', 'cancel'), {cancelled: true});
});
