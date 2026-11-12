// Progress over time: pure functions (no DOM) for the chart on the home page.
// Input are the runs of GET /api/me/progress. Days are the user's local
// calendar days; the server does not know the time zone.

// dayKey is the local calendar day of d as YYYY-MM-DD.
export function dayKey(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

// windowDays lists the last n local days, oldest first, ending with today.
// Built from calendar fields, not by subtracting 24 h, so DST days are not skipped or doubled.
export function windowDays(today, n) {
  const out = [];
  for (let i = n - 1; i >= 0; i--) out.push(dayKey(new Date(today.getFullYear(), today.getMonth(), today.getDate() - i)));
  return out;
}

function median(xs) {
  const s = [...xs].sort((a, b) => a - b);
  const m = s.length >> 1;
  return s.length % 2 ? s[m] : Math.round((s[m - 1] + s[m]) / 2);
}

// daily groups finished runs with measured typing time (active_ms) by the
// local day they were finished. Runs of older clients (no active_ms) include
// pauses and would make the curve jump, so they are left out.
// Per day: median cpm, accuracy over all key presses of that day, number of runs.
export function daily(runs, today, n) {
  const days = windowDays(today, n);
  const index = new Map(days.map((d, i) => [d, i]));
  const byDay = new Map();
  for (const r of runs) {
    if (!r.finished_at || r.active_ms == null) continue;
    const day = dayKey(new Date(r.finished_at));
    if (!index.has(day)) continue;
    if (!byDay.has(day)) byDay.set(day, []);
    byDay.get(day).push(r);
  }
  return [...byDay.entries()]
    .map(([day, rs]) => {
      const typed = rs.reduce((s, r) => s + r.cursor_pos, 0);
      const errors = rs.reduce((s, r) => s + r.error_count, 0);
      return {
        day, index: index.get(day), runs: rs.length,
        cpm: median(rs.map((r) => r.cpm)),
        accuracy: typed + errors === 0 ? 100 : Math.round((typed / (typed + errors)) * 1000) / 10,
      };
    })
    .sort((a, b) => a.index - b.index);
}

// streak counts the days in a row with any typing (finished or not), by the
// local day of the last sync. A streak that has not been continued today is
// still current until the day is over: it counts from yesterday then.
// capped is true if the streak reaches back to the start of the data window,
// so it may be longer than shown.
export function streak(runs, today, n) {
  const active = new Set(runs.map((r) => dayKey(new Date(r.updated_at))));
  const days = windowDays(today, n);
  const todayActive = active.has(days[n - 1]);
  let count = 0;
  for (let i = todayActive ? n - 1 : n - 2; i >= 0 && active.has(days[i]); i--) count++;
  const practiceDays = days.filter((d) => active.has(d)).length;
  return { days: count, today: todayActive, capped: count > 0 && count === (todayActive ? n : n - 1), practiceDays };
}

// niceScale returns axis bounds for values: cpm from 0 to the next multiple of
// 50 above the maximum; accuracy (percent) from the next multiple of 5 below the
// minimum to 100.
export function niceScale(values, kind) {
  if (kind === 'accuracy') {
    const lo = Math.min(95, Math.floor(Math.min(...values) / 5) * 5);
    return { lo: Math.max(0, lo), hi: 100 };
  }
  return { lo: 0, hi: Math.max(50, Math.ceil((Math.max(...values) * 1.1) / 50) * 50) };
}

// plot maps day points (sorted by index) to SVG coordinates inside
// width × height, keeping the given margins free for axis labels. The x axis
// runs from the first day with data to today (index n-1), at least minDays wide,
// so a new user does not get a few dots squeezed against the right edge of a
// 90-day axis. first is the index of the leftmost day. y is the value of key.
export function plot(points, key, kind, n, { width, height, left, right, top, bottom }, minDays = 7) {
  const { lo, hi } = niceScale(points.map((p) => p[key]), kind);
  const first = Math.max(0, Math.min(points[0].index, n - minDays));
  const span = n - 1 - first;
  const x = (i) => left + (span === 0 ? 0 : ((i - first) / span) * (width - left - right));
  const y = (v) => height - bottom - ((v - lo) / (hi - lo)) * (height - top - bottom);
  return { lo, hi, first, x, y, coords: points.map((p) => ({ x: x(p.index), y: y(p[key]), point: p })) };
}
