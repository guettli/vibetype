import { test } from 'node:test';
import assert from 'node:assert/strict';
import { newSession, chars, keyToChar, press, cpm, accuracy, syncPayload, MAX_CPM, MAX_GAP_MS, activeMs, wallMs } from '../static/engine.js';

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
  assert.equal(cpm(s, t0 + 999999), 40); // 4 chars in 6 s active time (the 57 s pause counts as 3 s)
  assert.equal(accuracy(s), 80);
  assert.deepEqual(s.keyErrors, { b: 1 }); // the x was typed while "b" was due
  assert.deepEqual(syncPayload(s, t0), {
    text_id: 'tid', cursor_pos: 4, error_count: 1, cpm: 40,
    started_at: '2026-01-01T00:00:00.000Z', finished_at: '2026-01-01T00:01:00.000Z', key_errors: { b: 1 },
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
  press(s, text, 'c', t0 + 1000 + 10 * 60000); // 10 min break -> counts as MAX_GAP_MS
  press(s, text, 'd', t0 + 1000 + 10 * 60000 + 1000);
  assert.equal(activeMs(s, 0), 1000 + MAX_GAP_MS + 1000);
  assert.equal(wallMs(s, 0), 1000 + 10 * 60000 + 1000);
  assert.equal(cpm(s, 0), Math.round(4 / ((5000) / 60000))); // 48
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

test('chars counts code points', () => {
  assert.equal(chars('a😀b').length, 3);
});
