// On-screen keyboard with finger hints (pure data + lookup; rendering in app.js).
//
// Fingers: lp lr lm li = left pinky/ring/middle/index, ri rm rr rp = right
// index/middle/ring/pinky, th = thumb.

export const FINGER_NAMES = {
  lp: 'linker kleiner Finger', lr: 'linker Ringfinger', lm: 'linker Mittelfinger', li: 'linker Zeigefinger',
  ri: 'rechter Zeigefinger', rm: 'rechter Mittelfinger', rr: 'rechter Ringfinger', rp: 'rechter kleiner Finger',
  th: 'Daumen',
};

// key spec: [id, base, shift, altgr, finger, width]
const k = (id, base, shift, altgr, finger, width = 1) => ({ id, base, shift, altgr, finger, width });
const mod = (id, label, finger, width) => ({ id, label, finger, width, mod: true });

const SPACE_ROW = [mod('AltL', 'Alt', 'th', 1.5), k('Space', ' ', null, null, 'th', 6), mod('AltGr', 'AltGr', 'th', 1.5)];

export const LAYOUTS = {
  de: {
    name: 'Deutsch (QWERTZ)',
    rows: [
      [k('Backquote', '^', '°', null, 'lp'), k('1', '1', '!', null, 'lp'), k('2', '2', '"', '²', 'lr'), k('3', '3', '§', '³', 'lm'),
        k('4', '4', '$', null, 'li'), k('5', '5', '%', null, 'li'), k('6', '6', '&', null, 'ri'), k('7', '7', '/', '{', 'ri'),
        k('8', '8', '(', '[', 'rm'), k('9', '9', ')', ']', 'rr'), k('0', '0', '=', '}', 'rp'), k('Minus', 'ß', '?', '\\', 'rp'),
        k('Equal', '´', '`', null, 'rp'), mod('Backspace', '⌫', 'rp', 1.5)],
      [mod('Tab', '↹', 'lp', 1.5), k('q', 'q', 'Q', '@', 'lp'), k('w', 'w', 'W', null, 'lr'), k('e', 'e', 'E', '€', 'lm'),
        k('r', 'r', 'R', null, 'li'), k('t', 't', 'T', null, 'li'), k('z', 'z', 'Z', null, 'ri'), k('u', 'u', 'U', null, 'ri'),
        k('i', 'i', 'I', null, 'rm'), k('o', 'o', 'O', null, 'rr'), k('p', 'p', 'P', null, 'rp'), k('BracketLeft', 'ü', 'Ü', null, 'rp'),
        k('BracketRight', '+', '*', '~', 'rp'), k('Enter', '\n', null, null, 'rp', 1.5)],
      [mod('Caps', '⇪', 'lp', 1.75), k('a', 'a', 'A', null, 'lp'), k('s', 's', 'S', null, 'lr'), k('d', 'd', 'D', null, 'lm'),
        k('f', 'f', 'F', null, 'li'), k('g', 'g', 'G', null, 'li'), k('h', 'h', 'H', null, 'ri'), k('j', 'j', 'J', null, 'ri'),
        k('k', 'k', 'K', null, 'rm'), k('l', 'l', 'L', null, 'rr'), k('Semicolon', 'ö', 'Ö', null, 'rp'), k('Quote', 'ä', 'Ä', null, 'rp'),
        k('Backslash', '#', "'", null, 'rp'), mod('Pad2', '', 'rp', 0.75)],
      [mod('ShiftL', '⇧', 'lp', 1.25), k('IntlBackslash', '<', '>', '|', 'lp'), k('y', 'y', 'Y', null, 'lp'), k('x', 'x', 'X', null, 'lr'),
        k('c', 'c', 'C', null, 'lm'), k('v', 'v', 'V', null, 'li'), k('b', 'b', 'B', null, 'li'), k('n', 'n', 'N', null, 'ri'),
        k('m', 'm', 'M', 'µ', 'ri'), k('Comma', ',', ';', null, 'rm'), k('Period', '.', ':', null, 'rr'), k('Slash', '-', '_', null, 'rp'),
        mod('ShiftR', '⇧', 'rp', 2.75)],
      SPACE_ROW,
    ],
  },
  us: {
    name: 'English (QWERTY)',
    rows: [
      [k('Backquote', '`', '~', null, 'lp'), k('1', '1', '!', null, 'lp'), k('2', '2', '@', null, 'lr'), k('3', '3', '#', null, 'lm'),
        k('4', '4', '$', null, 'li'), k('5', '5', '%', null, 'li'), k('6', '6', '^', null, 'ri'), k('7', '7', '&', null, 'ri'),
        k('8', '8', '*', null, 'rm'), k('9', '9', '(', null, 'rr'), k('0', '0', ')', null, 'rp'), k('Minus', '-', '_', null, 'rp'),
        k('Equal', '=', '+', null, 'rp'), mod('Backspace', '⌫', 'rp', 1.5)],
      [mod('Tab', '↹', 'lp', 1.5), k('q', 'q', 'Q', null, 'lp'), k('w', 'w', 'W', null, 'lr'), k('e', 'e', 'E', null, 'lm'),
        k('r', 'r', 'R', null, 'li'), k('t', 't', 'T', null, 'li'), k('y', 'y', 'Y', null, 'ri'), k('u', 'u', 'U', null, 'ri'),
        k('i', 'i', 'I', null, 'rm'), k('o', 'o', 'O', null, 'rr'), k('p', 'p', 'P', null, 'rp'), k('BracketLeft', '[', '{', null, 'rp'),
        k('BracketRight', ']', '}', null, 'rp'), k('Backslash', '\\', '|', null, 'rp', 1.5)],
      [mod('Caps', '⇪', 'lp', 1.75), k('a', 'a', 'A', null, 'lp'), k('s', 's', 'S', null, 'lr'), k('d', 'd', 'D', null, 'lm'),
        k('f', 'f', 'F', null, 'li'), k('g', 'g', 'G', null, 'li'), k('h', 'h', 'H', null, 'ri'), k('j', 'j', 'J', null, 'ri'),
        k('k', 'k', 'K', null, 'rm'), k('l', 'l', 'L', null, 'rr'), k('Semicolon', ';', ':', null, 'rp'), k('Quote', "'", '"', null, 'rp'),
        k('Enter', '\n', null, null, 'rp', 2.25)],
      [mod('ShiftL', '⇧', 'lp', 2.25), k('z', 'z', 'Z', null, 'lp'), k('x', 'x', 'X', null, 'lr'), k('c', 'c', 'C', null, 'lm'),
        k('v', 'v', 'V', null, 'li'), k('b', 'b', 'B', null, 'li'), k('n', 'n', 'N', null, 'ri'), k('m', 'm', 'M', null, 'ri'),
        k('Comma', ',', '<', null, 'rm'), k('Period', '.', '>', null, 'rr'), k('Slash', '/', '?', null, 'rp'),
        mod('ShiftR', '⇧', 'rp', 2.75)],
      SPACE_ROW,
    ],
  },
};

const LEFT = new Set(['lp', 'lr', 'lm', 'li']);

// locate returns how to type ch on the layout:
//   { key, finger, modifier: null | 'ShiftL' | 'ShiftR' | 'AltGr' }
// or null if the character is not on the layout (e.g. é on a US keyboard).
// Shift is pressed with the pinky of the OTHER hand (10-finger rule).
export function locate(ch, layoutId) {
  const layout = LAYOUTS[layoutId];
  if (!layout) throw new Error(`unknown keyboard layout ${layoutId}`);
  for (const row of layout.rows) {
    for (const key of row) {
      if (key.mod) continue;
      if (key.base === ch) return { key: key.id, finger: key.finger, modifier: null };
      if (key.shift === ch) return { key: key.id, finger: key.finger, modifier: LEFT.has(key.finger) ? 'ShiftR' : 'ShiftL' };
      if (key.altgr === ch) return { key: key.id, finger: key.finger, modifier: 'AltGr' };
    }
  }
  // single letters not on the base layer, e.g. capital umlauts are covered above;
  // everything else (é, ç, …) has no fixed key.
  return null;
}

// hint describes the finger(s) for ch in German.
export function hint(ch, layoutId) {
  const loc = locate(ch, layoutId);
  if (!loc) return 'Taste nicht auf diesem Layout';
  const f = FINGER_NAMES[loc.finger];
  if (loc.modifier === 'ShiftL') return `${f} + linke Umschalttaste (linker kleiner Finger)`;
  if (loc.modifier === 'ShiftR') return `${f} + rechte Umschalttaste (rechter kleiner Finger)`;
  if (loc.modifier === 'AltGr') return `${f} + AltGr (rechter Daumen)`;
  return f;
}

export function defaultLayout(lang) {
  return /^de\b|^de-/.test(lang || '') ? 'de' : 'us';
}
