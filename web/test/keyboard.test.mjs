import { test } from 'node:test';
import assert from 'node:assert/strict';
import { locate, hint, LAYOUTS, defaultLayout } from '../static/keyboard.js';

test('every printable ASCII character is on both layouts', () => {
  for (const id of Object.keys(LAYOUTS)) {
    for (let c = 0x20; c <= 0x7e; c++) {
      const ch = String.fromCharCode(c);
      assert.ok(locate(ch, id), `${JSON.stringify(ch)} missing on ${id}`);
    }
    assert.ok(locate('\n', id));
  }
});

test('german specials', () => {
  for (const ch of 'äöüÄÖÜß€§°') assert.ok(locate(ch, 'de'), ch);
  assert.equal(locate('é', 'de'), null);
  assert.equal(locate('ä', 'us'), null);
});

test('10-finger rules', () => {
  assert.deepEqual(locate('f', 'de'), { key: 'f', finger: 'li', modifier: null });
  assert.deepEqual(locate('F', 'de'), { key: 'f', finger: 'li', modifier: 'ShiftR' }); // other hand's shift
  assert.deepEqual(locate('J', 'us'), { key: 'j', finger: 'ri', modifier: 'ShiftL' });
  assert.deepEqual(locate('@', 'de'), { key: 'q', finger: 'lp', modifier: 'AltGr' });
  assert.equal(locate('z', 'de').finger, 'ri');
  assert.equal(locate('z', 'us').finger, 'lp');
  assert.equal(locate(' ', 'de').finger, 'th');
  assert.equal(hint('A', 'de'), 'linker kleiner Finger + rechte Umschalttaste (rechter kleiner Finger)');
});

test('key ids are unique per layout', () => {
  for (const [id, l] of Object.entries(LAYOUTS)) {
    const ids = l.rows.flat().map((k) => k.id);
    assert.equal(new Set(ids).size, ids.length, id);
  }
});

test('defaultLayout', () => {
  assert.equal(defaultLayout('de-DE'), 'de');
  assert.equal(defaultLayout('de'), 'de');
  assert.equal(defaultLayout('en-US'), 'us');
  assert.equal(defaultLayout(undefined), 'us');
});
