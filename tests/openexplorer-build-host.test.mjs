import assert from 'node:assert/strict';
import {mkdtemp, readFile, symlink, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import test from 'node:test';

import {
  buildHostStatePath,
  isBuildHostVerified,
  loadBuildHostState,
  loadSelectedBuildHost,
  markBuildHostVerified,
  normalizeBuildHostTarget,
  normalizeRemoteBuildCommand,
  saveSelectedBuildHost,
  shellUsesSSHHost,
} from '../extensions/rdk/openexplorer-build-host.mjs';

test('build host targets accept aliases and reject SSH option injection', () => {
  assert.equal(normalizeBuildHostTarget('builder-5090'), 'builder-5090');
  assert.equal(normalizeBuildHostTarget('build.user@10.0.0.8'), 'build.user@10.0.0.8');
  for (const value of ['', '-oProxyCommand=bad', 'host command', 'host;touch-x', 'host:29001', 'user@']) {
    assert.throws(() => normalizeBuildHostTarget(value));
  }
});

test('remote build commands are bounded without rewriting user intent', () => {
  assert.equal(normalizeRemoteBuildCommand('cd /work && make'), 'cd /work && make');
  assert.throws(() => normalizeRemoteBuildCommand(''));
  assert.throws(() => normalizeRemoteBuildCommand('bad\0command'));
});

test('direct SSH to the selected build host is recognized without matching other hosts', () => {
  const command = 'ssh -o BatchMode=yes -o ConnectTimeout=5 openexplorer-builder "hostname && echo ok" 2>&1 | head -3';
  assert.equal(shellUsesSSHHost(command, 'openexplorer-builder'), true);
  assert.equal(shellUsesSSHHost(command, 'another-builder'), false);
  assert.equal(shellUsesSSHHost('echo openexplorer-builder; ssh another-builder hostname', 'openexplorer-builder'), false);
  assert.equal(shellUsesSSHHost('/usr/bin/ssh build.user@10.0.0.8 uname -m', 'build.user@10.0.0.8'), true);
});

test('selected build host is stored as private task state and rejects symlinks', async () => {
  const root = await mkdtemp(join(tmpdir(), 'hobot-openexplorer-host-'));
  const path = buildHostStatePath(root);
  await saveSelectedBuildHost('builder-5090', path);
  assert.equal(await loadSelectedBuildHost(path), 'builder-5090');
  assert.equal(await isBuildHostVerified('builder-5090', path), false);
  const saved = JSON.parse(await readFile(path, 'utf8'));
  assert.equal(saved.schemaVersion, 2);

  await markBuildHostVerified('builder-5090', path);
  const verified = await loadBuildHostState(path);
  assert.equal(verified.target, 'builder-5090');
  assert.match(verified.verifiedAt, /^\d{4}-\d{2}-\d{2}T/);
  assert.equal(await isBuildHostVerified('builder-5090', path), true);

  await saveSelectedBuildHost('builder-5090', path);
  assert.equal(await isBuildHostVerified('builder-5090', path), true);
  await saveSelectedBuildHost('builder-6000', path);
  assert.equal(await isBuildHostVerified('builder-5090', path), false);
  assert.equal(await isBuildHostVerified('builder-6000', path), false);
  await assert.rejects(() => markBuildHostVerified('builder-5090', path), /changed before verification/);

  const target = join(root, 'outside.json');
  const link = join(root, 'linked.json');
  await writeFile(target, '{"schemaVersion":1,"target":"builder"}\n', {mode: 0o600});
  await symlink(target, link);
  await assert.rejects(() => loadSelectedBuildHost(link), /private-file checks/);
});
