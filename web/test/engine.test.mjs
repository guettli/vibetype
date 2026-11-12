// practice days are local days: pin the zone (Node resets its cache when TZ is assigned)
process.env.TZ = 'Europe/Berlin';

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { newSession, chars, keyToChar, press, cpm, accuracy, syncPayload, MAX_CPM, MAX_DAYS, MAX_GAP_MS, activeMs, wallMs, upgrade } from '../static/engine.js';

test('keyToChar', () => {
  assert.equal(keyToChar({ key: 'a' }), 'a');
  assert.equal(keyToChar({ key: 'A', shiftKey: true }), 'A');
  assert.equal(keyToChar({ key: 'Enter' }), '\n');
  assert.equal(keyToChar({ key: ' ' }), ' ');
  assert.equal(keyToChar({ key: 'Shift' }), null);
  assert.equal(keyToChar({ key: 'Dead' }), null);
  assert.equal(keyToChar({ key: 'c', ctrlKey: true }), null);
  assert.equal(keyToChar({ key: '@', ctrlKey: true, altKey: true }), '@'); // AltGr+Q
  assert.equal(keyToChar({ key: 'v', metaKey: true }), null);
  assert.equal(keyToChar({ key: 'ä' }), 'ä');
});

test('stop on error, finish, stats', () => {
  const text = chars('ab\nc');
  const s = newSession('sid', 'tid');
  const t0 = Date.parse('2026-01-01T00:00:00Z');
  assert.equal(press(s, text, 'a', t0), 'correct');
  assert.equal(press(s, text, 'x', t0 + 1000), 'error');
  assert.equal(s.cursor, 1);
  assert.equal(s.errors, 1);
  assert.equal(press(s, text, 'b', t0 + 2000), 'correct');
  assert.equal(press(s, text, '\n', t0 + 3000), 'correct');
  assert.equal(s.finishedAt, null);
  assert.equal(press(s, text, 'c', t0 + 60000), 'correct');
  assert.equal(s.finishedAt, '2026-01-01T00:01:00.000Z');
  assert.equal(press(s, text, 'c', t0 + 61000), 'ignored');
  assert.equal(cpm(s, t0 + 999999), 34); // 4 chars in 7 s active time (the 57 s pause counts as 4 x the 1 s rhythm)
  assert.equal(accuracy(s), 80);
  assert.deepEqual(s.keyErrors, { b: 1 }); // the x was typed while "b" was due
  assert.deepEqual(syncPayload(s, t0), {
    text_id: 'tid', cursor_pos: 4, error_count: 1, cpm: 34,
    started_at: '2026-01-01T00:00:00.000Z', finished_at: '2026-01-01T00:01:00.000Z', key_errors: { b: 1 }, practice_days: ['2026-01-01'], active_ms: 7000,
  });
});

test('cpm is capped like on the server', () => {
  const text = chars('x'.repeat(500));
  const s = newSession('sid', 'tid');
  const t0 = Date.parse('2026-01-01T00:00:00Z');
  for (let i = 0; i < 500; i++) press(s, text, 'x', t0 + i); // 500 chars in 0.5 s
  assert.equal(cpm(s, t0 + 1000), MAX_CPM);
});

test('pauses longer than MAX_GAP_MS do not count', () => {
  const text = chars('abcd');
  const s = newSession('sid', 'tid');
  const t0 = Date.parse('2026-01-01T00:00:00Z');
  press(s, text, 'a', t0);
  press(s, text, 'b', t0 + 1000);
  press(s, text, 'c', t0 + 1000 + 10 * 60000); // 10 min break -> counts as max(MAX_GAP_MS, 4 x 1 s rhythm) = 4 s
  press(s, text, 'd', t0 + 1000 + 10 * 60000 + 1000);
  assert.equal(activeMs(s, 0), 1000 + 4000 + 1000);
  assert.equal(wallMs(s, 0), 1000 + 10 * 60000 + 1000);
  assert.equal(cpm(s, 0), 40);
});

test('states of older versions use wall-clock time', () => {
  const text = chars('ab');
  const s = { id: 'x', textId: 't', cursor: 0, errors: 0, startedAt: null, finishedAt: null }; // no activeMs
  const t0 = Date.parse('2026-01-01T00:00:00Z');
  press(s, text, 'a', t0);
  press(s, text, 'b', t0 + 60000);
  assert.equal(activeMs(s, 0), 60000);
  assert.equal(cpm(s, 0), 2);
});

test('slow typists are not counted as pausing', () => {
  const text = chars('x'.repeat(30));
  const s = newSession('sid', 'tid');
  const t0 = 0;
  for (let i = 0; i < 30; i++) press(s, text, 'x', t0 + i * 5000); // 5 s per key = 12 cpm
  // only the very first gap (no rhythm known yet) is capped at MAX_GAP_MS: 13 instead of 12, not 22
  assert.equal(cpm(s, 0), 13);
});

test('presses without timing (old tab) fall back to wall time', () => {
  const text = chars('abcdef');
  const s = newSession('sid', 'tid');
  press(s, text, 'a', 0);
  s.cursor = 4; // an old-version tab typed b, c, d without updating the timing
  press(s, text, 'e', 60000);
  assert.equal(s.activeMs, undefined);
  assert.equal(activeMs(s, 60000), 60000);
  assert.equal(syncPayload(s, 60000).active_ms, null);
});

test('upgrade only touches unstarted old states', () => {
  assert.equal(upgrade({ startedAt: null }).activeMs, 0);
  assert.equal(upgrade({ startedAt: '2026-01-01T00:00:00Z' }).activeMs, undefined);
});

test('chars counts code points', () => {
  assert.equal(chars('a😀b').length, 3);
});

test('practice days: local days with key presses, a session can span several', () => {
  const text = chars('abcdef');
  const s = newSession('sid', 'tid');
  press(s, text, 'a', Date.parse('2026-11-11T22:30:00Z')); // 23:30 in Berlin
  press(s, text, 'x', Date.parse('2026-11-11T23:30:00Z')); // 00:30 next day: errors count as practice too
  press(s, text, 'b', Date.parse('2026-11-12T08:00:00Z'));
  press(s, text, 'c', Date.parse('2026-11-13T08:00:00Z'));
  assert.deepEqual(syncPayload(s, Date.parse('2026-11-13T09:00:00Z')).practice_days, ['2026-11-11', '2026-11-12', '2026-11-13']);
  // states of older versions have no days
  const old = { ...newSession('old', 'tid') };
  delete old.days;
  assert.deepEqual(syncPayload(old, 0).practice_days, []);
  // at most MAX_DAYS, the newest are kept
  const long = newSession('long', 'tid');
  const t = chars('a'.repeat(MAX_DAYS + 5));
  for (let i = 0; i < MAX_DAYS + 5; i++) press(long, t, 'a', Date.parse('2026-01-01T12:00:00Z') + i * 86400000);
  assert.equal(long.days.length, MAX_DAYS);
  assert.equal(long.days.at(-1), '2026-04-15');
});
