// Pure typing engine (no DOM) – unit tested with `node --test web/test`.

// newSession creates the local state of one typing run over `content`.
export function newSession(id, textId) {
  // keyErrors: expected character -> number of wrong key presses while it was due
  // activeMs: typing time without pauses (see MAX_GAP_MS); lastKeyAt: ms timestamp of the last key press
  return { id, textId, cursor: 0, errors: 0, keyErrors: {}, startedAt: null, finishedAt: null, activeMs: 0, lastKeyAt: null };
}

// chars splits a text into code points (the server counts runes, not UTF-16 units).
export function chars(content) {
  return Array.from(content);
}

// keyToChar maps a KeyboardEvent-like object to the typed character, or null
// if the key is not a character (Shift, Alt, arrows, shortcuts, dead keys …).
export function keyToChar(ev) {
  if (ev.isComposing || ev.metaKey) return null;
  // AltGr on Windows reports ctrlKey+altKey; a plain Ctrl shortcut has no altKey.
  if (ev.ctrlKey && !ev.altKey) return null;
  if (ev.key === 'Enter') return '\n';
  if (Array.from(ev.key || '').length === 1) return ev.key;
  return null;
}

// press applies one typed character (stop-on-error).
// Returns 'correct', 'error' or 'ignored' (text already finished).
export function press(state, textChars, ch, now) {
  if (state.finishedAt || state.cursor >= textChars.length) return 'ignored';
  if (!state.startedAt) state.startedAt = new Date(now).toISOString();
  if (state.activeMs !== undefined) { // undefined: state stored by an older version -> wall-clock time
    if (state.lastKeyAt !== null) state.activeMs += Math.min(Math.max(now - state.lastKeyAt, 0), MAX_GAP_MS);
    state.lastKeyAt = now;
  }
  if (ch === textChars[state.cursor]) {
    state.cursor++;
    if (state.cursor === textChars.length) state.finishedAt = new Date(now).toISOString();
    return 'correct';
  }
  state.errors++;
  state.keyErrors ??= {}; // sessions stored by older versions have no keyErrors
  const due = textChars[state.cursor];
  state.keyErrors[due] = (state.keyErrors[due] ?? 0) + 1;
  return 'error';
}

// More is not humanly possible (world records are around 1000-1300); the server
// clamps to the same value, so the client shows what the server stores.
export const MAX_CPM = 3000;

// A gap between two key presses counts at most this long, so a break (reading,
// talking, the bell) does not ruin the speed. Up to 3 s is normal thinking time.
export const MAX_GAP_MS = 3000;

// wallMs is the time from the first key press to the end (or now).
export function wallMs(state, now) {
  if (!state.startedAt) return 0;
  return (state.finishedAt ? Date.parse(state.finishedAt) : now) - Date.parse(state.startedAt);
}

// activeMs is the typing time without pauses; while typing, the running gap
// counts up to MAX_GAP_MS. States of older versions fall back to wall time.
export function activeMs(state, now) {
  if (state.activeMs === undefined || !state.startedAt) return wallMs(state, now);
  const running = state.finishedAt || state.lastKeyAt === null ? 0 : Math.min(Math.max(now - state.lastKeyAt, 0), MAX_GAP_MS);
  return state.activeMs + running;
}

// cpm = correct characters per minute of active typing time, at most MAX_CPM.
export function cpm(state, now) {
  if (!state.startedAt || state.cursor === 0) return 0;
  const minutes = activeMs(state, now) / 60000;
  return Math.min(MAX_CPM, Math.round(state.cursor / Math.max(minutes, 1 / 60))); // at least 1s to avoid silly peaks
}

// accuracy in percent (correct key presses / all key presses).
export function accuracy(state) {
  const total = state.cursor + state.errors;
  return total === 0 ? 100 : Math.round((state.cursor / total) * 1000) / 10;
}

// syncPayload is the body of PUT /api/sessions/{id}.
export function syncPayload(state, now) {
  return {
    text_id: state.textId,
    cursor_pos: state.cursor,
    error_count: state.errors,
    cpm: cpm(state, now),
    started_at: state.startedAt,
    finished_at: state.finishedAt,
    key_errors: state.keyErrors ?? {},
  };
}
