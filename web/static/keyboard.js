// On-screen keyboard with finger hints (pure data + lookup; rendering in app.js).
//
// Fingers: lp lr lm li = left pinky/ring/middle/index, ri rm rr rp = right
// index/middle/ring/pinky, th = thumb.

export const FINGER_NAMES = {
  lp: 'linker kleiner Finger', lr: 'linker Ringfinger', lm: 'linker Mittelfinger', li: 'linker Zeigefinger',
  ri: 'rechter Zeigefinger', rm: 'rechter Mittelfinger', rr: 'rechter Ringfinger', rp: 'rechter kleiner Finger',
  th: 'Daumen',
};

// key spec: [id, base, shift, altgr, finger, width, dead]
// dead: which layers are dead keys (German ^ ´ `): typing them alone needs a following space.
const k = (id, base, shift, altgr, finger, width = 1, dead = '') => ({ id, base, shift, altgr, finger, width, dead });
const mod = (id, label, finger, width) => ({ id, label, finger, width, mod: true });

// Every row adds up to 15 units so the rows line up like a real keyboard.
const SPACE_ROW = [mod('CtrlL', 'Strg', 'lp', 1.25), mod('Win', '◆', 'lp', 1.25), mod('AltL', 'Alt', 'th', 1.25),
  k('Space', ' ', null, null, 'th', 6.25), mod('AltGr', 'AltGr', 'th', 1.25), mod('Win2', '◆', 'rp', 1.25),
  mod('Menu', '☰', 'rp', 1.25), mod('CtrlR', 'Strg', 'rp', 1.25)];

export const LAYOUTS = {
  de: {
    name: 'Deutsch (QWERTZ)',
    rows: [
      [k('Backquote', '^', '°', null, 'lp', 1, 'base'), k('1', '1', '!', null, 'lp'), k('2', '2', '"', '²', 'lr'), k('3', '3', '§', '³', 'lm'),
        k('4', '4', '$', null, 'li'), k('5', '5', '%', null, 'li'), k('6', '6', '&', null, 'ri'), k('7', '7', '/', '{', 'ri'),
        k('8', '8', '(', '[', 'rm'), k('9', '9', ')', ']', 'rr'), k('0', '0', '=', '}', 'rp'), k('Minus', 'ß', '?', '\\', 'rp'),
        k('Equal', '´', '`', null, 'rp', 1, 'base shift'), mod('Backspace', '⌫', 'rp', 2)],
      [mod('Tab', '↹', 'lp', 1.5), k('q', 'q', 'Q', '@', 'lp'), k('w', 'w', 'W', null, 'lr'), k('e', 'e', 'E', '€', 'lm'),
        k('r', 'r', 'R', null, 'li'), k('t', 't', 'T', null, 'li'), k('z', 'z', 'Z', null, 'ri'), k('u', 'u', 'U', null, 'ri'),
        k('i', 'i', 'I', null, 'rm'), k('o', 'o', 'O', null, 'rr'), k('p', 'p', 'P', null, 'rp'), k('BracketLeft', 'ü', 'Ü', null, 'rp'),
        k('BracketRight', '+', '*', '~', 'rp'), k('Enter', '\n', null, null, 'rp', 1.5)],
      [mod('Caps', '⇪', 'lp', 1.75), k('a', 'a', 'A', null, 'lp'), k('s', 's', 'S', null, 'lr'), k('d', 'd', 'D', null, 'lm'),
        k('f', 'f', 'F', null, 'li'), k('g', 'g', 'G', null, 'li'), k('h', 'h', 'H', null, 'ri'), k('j', 'j', 'J', null, 'ri'),
        k('k', 'k', 'K', null, 'rm'), k('l', 'l', 'L', null, 'rr'), k('Semicolon', 'ö', 'Ö', null, 'rp'), k('Quote', 'ä', 'Ä', null, 'rp'),
        k('Backslash', '#', "'", null, 'rp'), mod('Pad2', '', 'rp', 1.25)],
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
        k('Equal', '=', '+', null, 'rp'), mod('Backspace', '⌫', 'rp', 2)],
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

// Combining marks that German dead keys produce, mapped to the dead key's character.
const DEAD_MARKS = { '́': '´', '̀': '`', '̂': '^' };

// one key press (with optional modifier) for ch, or null
function keyFor(ch, layout) {
  for (const row of layout.rows) {
    for (const key of row) {
      if (key.mod) continue;
      const layer = key.base === ch ? 'base' : key.shift === ch ? 'shift' : key.altgr === ch ? 'altgr' : null;
      if (!layer) continue;
      const modifier = layer === 'shift' ? (LEFT.has(key.finger) ? 'ShiftR' : 'ShiftL') : layer === 'altgr' ? 'AltGr' : null;
      return { ch, key: key.id, finger: key.finger, modifier, dead: key.dead.split(' ').includes(layer) };
    }
  }
  return null;
}

// locate returns the key presses needed to type ch on the layout, as
//   { steps: [{ ch, key, finger, modifier: null|'ShiftL'|'ShiftR'|'AltGr', dead }] }
// Usually one step. Dead keys need two: "^" alone is ^ then Space, "é" is ´ then e.
// Shift is pressed with the pinky of the OTHER hand (10-finger rule).
// Returns null if the character is not on the layout (e.g. ñ on German, é on US).
export function locate(ch, layoutId) {
  const layout = LAYOUTS[layoutId];
  if (!layout) throw new Error(`unknown keyboard layout ${layoutId}`);
  const direct = keyFor(ch, layout);
  if (direct && !direct.dead) return { steps: [direct] };
  if (direct) return { steps: [direct, keyFor(' ', layout)] };
  const [base, mark, ...rest] = Array.from(ch.normalize('NFD'));
  if (!mark || rest.length || !DEAD_MARKS[mark]) return null;
  const dead = keyFor(DEAD_MARKS[mark], layout);
  const letter = keyFor(base, layout);
  if (!dead || !dead.dead || !letter) return null;
  return { steps: [dead, letter] };
}

function describe(step) {
  const f = FINGER_NAMES[step.finger];
  if (step.modifier === 'ShiftL') return `${f} + linke Umschalttaste (linker kleiner Finger)`;
  if (step.modifier === 'ShiftR') return `${f} + rechte Umschalttaste (rechter kleiner Finger)`;
  if (step.modifier === 'AltGr') return `${f} + AltGr (rechter Daumen)`;
  return f;
}

const label = (c) => (c === ' ' ? 'Leertaste' : c === '\n' ? 'Enter' : c);

// hint describes the finger(s) for ch in German.
export function hint(ch, layoutId) {
  const loc = locate(ch, layoutId);
  if (!loc) return 'Taste nicht auf diesem Layout';
  if (loc.steps.length === 1) return describe(loc.steps[0]);
  return loc.steps.map((s, i) => `${i + 1}. ${label(s.ch)}${s.dead ? ' (Tottaste)' : ''}: ${describe(s)}`).join('  ');
}

export function defaultLayout(lang) {
  return /^de\b|^de-/.test(lang || '') ? 'de' : 'us';
}
