// Node resets its time zone cache when TZ is assigned: run with a DST zone.
process.env.TZ = 'Europe/Berlin';

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dayKey, windowDays, daily, streak, niceScale, plot } from '../static/progress.js';

const at = (y, m, d, hh = 12) => new Date(y, m - 1, d, hh).toISOString();
const run = (fin, cpm, extra = {}) => ({ updated_at: fin, finished_at: fin, cpm, cursor_pos: 100, error_count: 0, active_ms: 60000, ...extra });

test('windowDays is calendar based across DST changes', () => {
  // 2026-10-25 has 25 hours in Berlin; 2026-03-29 has 23
  assert.deepEqual(windowDays(new Date(2026, 9, 26, 0, 30), 3), ['2026-10-24', '2026-10-25', '2026-10-26']);
  assert.deepEqual(windowDays(new Date(2026, 2, 30, 23, 30), 3), ['2026-03-28', '2026-03-29', '2026-03-30']);
  assert.equal(windowDays(new Date(2026, 10, 13), 90).length, 90);
});

test('dayKey uses the local day, not UTC', () => {
  // 23:30 local on Nov 13 is 22:30 UTC, 00:30 local on Nov 14 is Nov 13 in UTC
  assert.equal(dayKey(new Date(2026, 10, 13, 23, 30)), '2026-11-13');
  assert.equal(dayKey(new Date('2026-11-13T23:30:00Z')), '2026-11-14');
});

test('daily: median cpm and accuracy per day, only finished runs with active_ms', () => {
  const today = new Date(2026, 10, 13, 18);
  const points = daily([
    run(at(2026, 11, 13, 9), 100),
    run(at(2026, 11, 13, 10), 300, { error_count: 25 }),
    run(at(2026, 11, 13, 11), 200),
    run(at(2026, 11, 12), 150, { cursor_pos: 50, error_count: 50 }),
    run(at(2026, 11, 12), 999, { active_ms: null }), // old client: pauses included
    { ...run(at(2026, 11, 11), 500), finished_at: null }, // not finished
    run(at(2026, 8, 1), 50), // outside the window
  ], today, 90);
  assert.deepEqual(points, [
    { day: '2026-11-12', index: 88, runs: 1, cpm: 150, accuracy: 50 },
    { day: '2026-11-13', index: 89, runs: 3, cpm: 200, accuracy: 92.3 },
  ]);
  assert.equal(daily([run(at(2026, 11, 13, 9), 100), run(at(2026, 11, 13, 10), 201)], today, 90)[0].cpm, 151);
});

test('streak counts consecutive days, today optional', () => {
  const today = new Date(2026, 10, 13, 18);
  const days = (...ds) => ds.map((d) => `2026-11-${String(d).padStart(2, '0')}`);
  assert.deepEqual(streak(days(9, 11, 12, 13), today, 90), { days: 3, today: true, capped: false, practiceDays: 4 });
  assert.deepEqual(streak(days(11, 12), today, 90), { days: 2, today: false, capped: false, practiceDays: 2 });
  assert.deepEqual(streak(days(10, 11), today, 90), { days: 0, today: false, capped: false, practiceDays: 2 });
  assert.deepEqual(streak([], today, 90), { days: 0, today: false, capped: false, practiceDays: 0 });
  assert.deepEqual(streak(days(11, 12, 13), today, 3), { days: 3, today: true, capped: true, practiceDays: 3 });
  assert.deepEqual(streak(days(11, 12), today, 3), { days: 2, today: false, capped: true, practiceDays: 2 });
  // days outside the window (the server sends one extra) do not count
  assert.deepEqual(streak(['2026-08-14', ...days(13)], today, 90), { days: 1, today: true, capped: false, practiceDays: 1 });
  // across the DST change on 2026-10-25
  assert.equal(streak(['2026-10-24', '2026-10-25', '2026-10-26'], new Date(2026, 9, 26, 0, 5), 90).days, 3);
});

test('niceScale', () => {
  assert.deepEqual(niceScale([120, 180], 'cpm'), { lo: 0, hi: 200 });
  assert.deepEqual(niceScale([10], 'cpm'), { lo: 0, hi: 50 });
  assert.deepEqual(niceScale([87.3, 99], 'accuracy'), { lo: 85, hi: 100 });
  assert.deepEqual(niceScale([100], 'accuracy'), { lo: 95, hi: 100 });
  assert.deepEqual(niceScale([0], 'accuracy'), { lo: 0, hi: 100 });
});

test('plot maps the days from the first data point to today onto the drawing area', () => {
  const box = { width: 200, height: 100, left: 10, right: 10, top: 10, bottom: 10 };
  const p = plot([{ index: 0, cpm: 0 }, { index: 89, cpm: 100 }], 'cpm', 'cpm', 90, box);
  assert.equal(p.hi, 150);
  assert.equal(p.first, 0);
  assert.deepEqual(p.coords.map(({ x, y }) => [x, Math.round(y)]), [[10, 90], [190, 37]]);
  // a new user: two days of data, the axis shows the last 7 days, not 90
  const q = plot([{ index: 87, cpm: 50 }, { index: 89, cpm: 60 }], 'cpm', 'cpm', 90, box);
  assert.equal(q.first, 83);
  assert.deepEqual(q.coords.map(({ x }) => x), [130, 190]);
  // data starting 30 days ago: the axis starts there
  assert.equal(plot([{ index: 60, cpm: 50 }, { index: 89, cpm: 60 }], 'cpm', 'cpm', 90, box).first, 60);
});
