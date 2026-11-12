import { test } from 'node:test';
import assert from 'node:assert/strict';
import { newSession, chars, keyToChar, press, cpm, accuracy, syncPayload } from '../static/engine.js';

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
  assert.equal(cpm(s, t0 + 999999), 4); // 4 chars in 1 minute
  assert.equal(accuracy(s), 80);
  assert.deepEqual(syncPayload(s, t0), {
    text_id: 'tid', cursor_pos: 4, error_count: 1, cpm: 4,
    started_at: '2026-01-01T00:00:00.000Z', finished_at: '2026-01-01T00:01:00.000Z',
  });
});

test('chars counts code points', () => {
  assert.equal(chars('a😀b').length, 3);
});
