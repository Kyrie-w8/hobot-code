import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';

const app = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');
const api = readFileSync(new URL('./api.ts', import.meta.url), 'utf8');
const css = readFileSync(new URL('./App.css', import.meta.url), 'utf8');

test('schedule UI is gated by the board capability and supports exactly one cadence', () => {
  assert.match(app, /capabilities\.includes\('schedules\.v1'\)/);
  assert.match(app, /cadence === 'once'/);
  assert.match(app, /localDateTimeRFC3339\(at\)/);
  assert.match(app, /\{at: atRFC3339\}/);
  assert.match(app, /\{every\}/);
  assert.match(app, /type="datetime-local"/);
});

test('schedule actions stay board-owned and use an explicit delete confirmation', () => {
  for (const method of ['createSchedule', 'pauseSchedule', 'resumeSchedule', 'runSchedule', 'deleteSchedule']) {
    assert.match(api, new RegExp(`${method}:`));
  }
  assert.match(app, /setDeleteTarget\(schedule\)/);
  assert.match(app, /Delete \{deleteTarget\.name\}\?/);
  assert.match(app, /<Play size=\{15\} \/>/);
  assert.doesNotMatch(app.slice(app.indexOf('function ScheduleDialog'), app.indexOf('function DeploymentDialog')), /window\.confirm/);
});

test('schedule modal does not overflow narrow windows', () => {
  assert.match(css, /\.schedule-modal \{ width: min\(680px, calc\(100vw - 32px\)\)/);
  const mobile = css.slice(css.indexOf('@media (max-width: 760px)'));
  assert.match(mobile, /\.schedule-modal \{ width: calc\(100vw - 24px\)/);
  assert.match(mobile, /\.schedule-row \{ grid-template-columns: minmax\(0, 1fr\)/);
});

test('scheduled prompts are visibly distinguished from user-authored messages', () => {
  assert.match(app, /scheduled-message-label/);
  assert.match(app, /<CalendarClock size=\{13\} \/>Scheduled/);
  assert.match(app, /onEdit && !scheduled/);
  assert.match(css, /\.scheduled-message-label/);
});
