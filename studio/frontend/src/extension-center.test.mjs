import assert from 'node:assert/strict';
import test from 'node:test';

import {extensionCatalogHealth, extensionCatalogSummary, extensionKindLabel, extensionTargetState, filterExtensions} from './extension-center.js';

const entries = [
  {id: 'hobot.skill.camera', name: 'Camera pipeline', kind: 'skill', description: 'MIPI and media workflow', defaultEnabled: true, required: false, provides: ['skill.camera'], permissions: [], targets: ['x5', 's100']},
  {id: 'hobot.rdk-core', name: 'RDK development core', kind: 'extension', description: 'Board coordination', defaultEnabled: true, required: true, provides: ['provider.drobotics'], permissions: ['rdk-devices'], targets: ['x5', 's100', 's600']},
  {id: 'hobot.integration.lab', name: 'Lab integration', kind: 'integration', description: 'Optional lab bridge', defaultEnabled: false, required: false, provides: [], permissions: ['model-network'], targets: []},
];

const catalog = {schemaVersion: 1, apiVersion: 'hobot.extensions/v1', productVersion: '0.26.0', hostVersion: '0.26.0', entries, policy: {inventoryOnly: true, executionAuthority: 'pi-runtime', permissionAuthority: 'board', thirdPartyRuntime: 'current-user', hotReload: false}};

test('catalog health requires a versioned read-only board-enforced boundary', () => {
  assert.deepEqual(extensionCatalogHealth(catalog), {healthy: true, issues: []});
  const unsafe = extensionCatalogHealth({...catalog, hostVersion: '0.25.0', policy: {...catalog.policy, hotReload: true}});
  assert.equal(unsafe.healthy, false);
  assert.deepEqual(unsafe.issues, ['Catalog and board versions differ', 'Unisolated hot reload is enabled']);
  const unsafeSource = extensionCatalogHealth({...catalog, diagnostics: [{source: 'hooks', status: 'unsafe'}]});
  assert.deepEqual(unsafeSource.issues, ['1 configured source could not be inspected']);
});

test('entry state describes packaging and target support without claiming execution', () => {
  assert.deepEqual(extensionTargetState(entries[1], 's600'), {state: 'required', label: 'Product required'});
  assert.deepEqual(extensionTargetState(entries[0], 's600'), {state: 'unavailable', label: 'Not for S600'});
  assert.deepEqual(extensionTargetState(entries[2], 's600'), {state: 'listed', label: 'Available'});
  assert.deepEqual(extensionTargetState({...entries[2], status: 'configured'}, 's600'), {state: 'included', label: 'Configured'});
  assert.deepEqual(extensionTargetState({...entries[2], status: 'missing'}, 's600'), {state: 'unavailable', label: 'Optional · not installed'});
  assert.deepEqual(extensionTargetState({...entries[2], status: 'disabled'}, 's600'), {state: 'unavailable', label: 'Optional · off'});
  assert.deepEqual(extensionTargetState({...entries[2], status: 'discovered'}, 's600'), {state: 'included', label: 'Discovered'});
  assert.deepEqual(extensionTargetState({...entries[2], status: 'declared'}, 's600'), {state: 'listed', label: 'Declared'});
  assert.equal(extensionKindLabel('skill'), 'Skill');
  assert.equal(extensionKindLabel('package'), 'Package');
  assert.equal(extensionKindLabel('prompt'), 'Prompt');
  assert.equal(extensionKindLabel('theme'), 'Theme');
});

test('task-context and trust diagnostics are informational safety boundaries', () => {
  assert.deepEqual(extensionCatalogHealth({...catalog, diagnostics: [{source: 'project-resources', status: 'contextual'}]}), {healthy: true, issues: []});
  assert.deepEqual(extensionCatalogHealth({...catalog, diagnostics: [{source: 'project-resources', status: 'untrusted'}]}), {healthy: true, issues: []});
  assert.deepEqual(extensionCatalogHealth({...catalog, diagnostics: [{source: 'project-skills', status: 'partial'}]}).issues, ['1 configured source could not be inspected']);
});

test('catalog summary and filters are deterministic and capability-aware', () => {
  assert.deepEqual(extensionCatalogSummary(catalog, 's600'), {total: 3, supported: 2, required: 1, skills: 1});
  assert.deepEqual(filterExtensions(entries, '', 'all').map((entry) => entry.id), ['hobot.rdk-core', 'hobot.integration.lab', 'hobot.skill.camera']);
  assert.deepEqual(filterExtensions(entries, 'MIPI', 'skill').map((entry) => entry.id), ['hobot.skill.camera']);
  assert.deepEqual(filterExtensions(entries, 'rdk-devices').map((entry) => entry.id), ['hobot.rdk-core']);
});
