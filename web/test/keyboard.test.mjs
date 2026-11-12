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
  assert.equal(locate('ñ', 'de'), null);
  assert.equal(locate('é', 'us'), null);
  assert.equal(locate('ä', 'us'), null);
});

test('10-finger rules', () => {
  const one = (ch, l) => { const { steps } = locate(ch, l); assert.equal(steps.length, 1, ch); return steps[0]; };
  assert.deepEqual(one('f', 'de'), { ch: 'f', key: 'f', finger: 'li', modifier: null, dead: false });
  assert.deepEqual(one('F', 'de'), { ch: 'F', key: 'f', finger: 'li', modifier: 'ShiftR', dead: false }); // other hand's shift
  assert.equal(one('J', 'us').modifier, 'ShiftL');
  assert.deepEqual(one('@', 'de'), { ch: '@', key: 'q', finger: 'lp', modifier: 'AltGr', dead: false });
  assert.equal(one('z', 'de').finger, 'ri');
  assert.equal(one('z', 'us').finger, 'lp');
  assert.equal(one(' ', 'de').finger, 'th');
  assert.equal(one('^', 'us').dead, false); // no dead keys on US
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

test('german dead keys', () => {
  const keys = (ch) => locate(ch, 'de').steps.map((s) => `${s.modifier ?? ''}${s.key}`);
  assert.deepEqual(keys('^'), ['Backquote', 'Space']);
  assert.deepEqual(keys('´'), ['Equal', 'Space']);
  assert.deepEqual(keys('`'), ['ShiftLEqual', 'Space']);
  assert.deepEqual(keys('é'), ['Equal', 'e']);
  assert.deepEqual(keys('À'), ['ShiftLEqual', 'ShiftRa']);
  assert.deepEqual(keys('ô'), ['Backquote', 'o']);
  for (const ch of 'áéíóúýàèìòùâêîôûÁÉÍÓÚÝÀÈÌÒÙÂÊÎÔÛ') assert.equal(locate(ch, 'de').steps.length, 2, ch);
  assert.equal(hint('é', 'de'), '1. ´ (Tottaste): rechter kleiner Finger  2. e: linker Mittelfinger');
});

test('rows line up (15 units each)', () => {
  for (const [id, l] of Object.entries(LAYOUTS)) {
    for (const [i, row] of l.rows.entries()) assert.equal(row.reduce((a, k) => a + k.width, 0), 15, `${id} row ${i}`);
  }
});
