// vibetype frontend – vanilla ES module, no build step.
import { newSession, chars, keyToChar, press, cpm, accuracy, syncPayload } from './engine.js';
import { LAYOUTS, locate, hint, defaultLayout } from './keyboard.js';

const SYNC_INTERVAL_MS = 3000;
const LIVE_INTERVAL_MS = 3000;
const ACTIVE_WINDOW_MS = 60000; // a session counts as "typing" if synced within this window

const $app = document.getElementById('app');
const $status = document.getElementById('status');
const $user = document.getElementById('user');

// ---------- localStorage helpers ----------
// `vt:me` is global; texts, typing sessions and the sync queue live under
// `vt:u:<user id>:` so that two accounts on a shared classroom PC never see or
// sync each other's data.

let me = null;

const store = (prefix) => ({
  get(key, def = null) {
    const raw = localStorage.getItem(prefix() + key);
    return raw === null ? def : JSON.parse(raw);
  },
  set(key, value) { localStorage.setItem(prefix() + key, JSON.stringify(value)); },
  del(key) { localStorage.removeItem(prefix() + key); },
});
const ls = store(() => 'vt:');
const us = store(() => {
  if (!me) throw new Error('user storage used without login');
  return `vt:u:${me.id}:`;
});

function clearUserStorage(userId) {
  const prefix = `vt:u:${userId}:`;
  for (const k of Object.keys(localStorage)) if (k.startsWith(prefix)) localStorage.removeItem(k);
}

// ---------- tiny DOM helpers ----------

// fill replaces el's children; null/false children are skipped (plain
// replaceChildren would render them as the text "null"/"false").
function fill(el, ...children) {
  el.replaceChildren(...children.flat().filter((c) => c != null && c !== false));
}

function h(tag, attrs = {}, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'class') el.className = v;
    else if (v !== false && v != null) el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : String(c));
  }
  return el;
}

// ---------- API ----------

class ApiError extends Error {
  constructor(status, message) { super(message); this.status = status; }
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
  if (!res.ok) throw new ApiError(res.status, data.error || `HTTP ${res.status}`);
  return data;
}

// fetch threw (network down), or the reverse proxy reports the backend as unavailable
const isOffline = (err) => !(err instanceof ApiError) || [502, 503, 504].includes(err.status);

// ---------- status bar ----------

const status = { sync: 'ok', error: null };

function renderStatus() {
  fill($status);
  if (status.error) {
    $status.append(h('span', { class: 'bad' }, status.error, ' ',
      h('button', { class: 'link', onclick: () => { status.error = null; renderStatus(); } }, '×')));
  }
  const pending = me ? Object.keys(us.get('pending', {})).length : 0;
  if (status.sync === 'offline') $status.append(h('span', { class: 'warn' }, `offline – ${pending} Stand/Stände warten auf Sync`));
  else if (status.sync === 'unauthorized') $status.append(h('span', { class: 'bad' }, 'Anmeldung abgelaufen – bitte neu anmelden, damit der Fortschritt synchronisiert wird'));
  else if (pending) $status.append(h('span', { class: 'muted' }, 'synchronisiere …'));
}

function fail(msg) {
  status.error = msg;
  renderStatus();
}

// ---------- sync loop (client -> server) ----------
// "pending" maps sessionId -> latest payload. Progress is latest-wins, so the queue
// never holds more than one entry per session, even after a long offline phase.

function enqueue(state) {
  const pending = us.get('pending', {});
  pending[state.id] = syncPayload(state, Date.now());
  us.set('pending', pending);
}

let syncing = false;
async function flush() {
  if (syncing || !me) return;
  syncing = true;
  try {
    const pending = us.get('pending', {});
    for (const [id, payload] of Object.entries(pending)) {
      let rejected = false;
      try {
        await api('PUT', `/api/sessions/${encodeURIComponent(id)}`, payload);
        status.sync = 'ok';
      } catch (err) {
        if (isOffline(err)) { status.sync = 'offline'; break; }
        if (err.status === 401) { status.sync = 'unauthorized'; break; }
        if (err.status >= 500 || err.status === 429) { fail(`Sync-Fehler auf dem Server (${err.status}): ${err.message}`); break; }
        // Permanent rejection (4xx): report loudly and drop, retrying cannot help.
        fail(`Sync abgelehnt (${err.status}): ${err.message}`);
        rejected = true;
      }
      // remove only if no newer state was queued meanwhile
      const now = us.get('pending', {});
      if (rejected || JSON.stringify(now[id]) === JSON.stringify(payload)) {
        delete now[id];
        us.set('pending', now);
      }
    }
  } finally {
    syncing = false;
    renderStatus();
  }
}

setInterval(flush, SYNC_INTERVAL_MS);
window.addEventListener('online', flush);

// ---------- auth (WebAuthn / passkeys) ----------

const b64urlToBuf = (s) => {
  const b = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4));
  return Uint8Array.from(b, (c) => c.charCodeAt(0)).buffer;
};
const bufToB64url = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf)))
  .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');

async function registerPasskey(displayName) {
  const { publicKey } = await api('POST', '/api/register/begin', { display_name: displayName });
  publicKey.challenge = b64urlToBuf(publicKey.challenge);
  publicKey.user.id = b64urlToBuf(publicKey.user.id);
  for (const c of publicKey.excludeCredentials || []) c.id = b64urlToBuf(c.id);
  const cred = await navigator.credentials.create({ publicKey });
  return api('POST', '/api/register/finish', {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: bufToB64url(cred.response.clientDataJSON),
      attestationObject: bufToB64url(cred.response.attestationObject),
      transports: cred.response.getTransports ? cred.response.getTransports() : [],
    },
  });
}

async function loginPasskey() {
  const { publicKey } = await api('POST', '/api/login/begin');
  publicKey.challenge = b64urlToBuf(publicKey.challenge);
  for (const c of publicKey.allowCredentials || []) c.id = b64urlToBuf(c.id);
  const cred = await navigator.credentials.get({ publicKey });
  return api('POST', '/api/login/finish', {
    id: cred.id,
    rawId: bufToB64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: bufToB64url(cred.response.clientDataJSON),
      authenticatorData: bufToB64url(cred.response.authenticatorData),
      signature: bufToB64url(cred.response.signature),
      userHandle: cred.response.userHandle ? bufToB64url(cred.response.userHandle) : undefined,
    },
  });
}

async function loadMe() {
  try {
    me = await api('GET', '/api/me');
    ls.set('me', me);
  } catch (err) {
    if (err.status === 401) { me = null; ls.del('me'); }
    else {
      // offline or server trouble: keep working with the last known login
      me = ls.get('me');
      if (!isOffline(err)) fail(`Server-Fehler beim Laden des Benutzers: ${err.message}`);
    }
  }
  renderUser();
}

async function logout() {
  await flush();
  const pending = Object.keys(us.get('pending', {})).length;
  if (pending && !confirm(`${pending} Tipp-Stand/Stände sind noch nicht synchronisiert und gehen beim Abmelden verloren. Trotzdem abmelden?`)) return;
  try { await api('POST', '/api/logout'); }
  catch (err) {
    if (!isOffline(err)) { fail(`Abmelden fehlgeschlagen: ${err.message}`); return; }
    // offline: the server-side session stays valid until it expires, but this browser forgets it
  }
  clearUserStorage(me.id);
  me = null;
  ls.del('me');
  renderUser();
  route();
}

function renderUser() {
  fill($user);
  if (!me) return;
  $user.append(me.display_name, ' · ', h('button', { class: 'link', onclick: logout }, 'abmelden'));
}

function loginView() {
  const name = h('input', { type: 'text', placeholder: 'Dein Name, z. B. Thomas', maxlength: 64, required: true, autocomplete: 'nickname' });
  const msg = h('p', { class: 'bad' });
  const run = (fn) => async (e) => {
    e.preventDefault();
    msg.textContent = '';
    try { me = await fn(); ls.set('me', me); renderUser(); route(); }
    catch (err) { msg.textContent = err.message; }
  };
  return h('section', { class: 'card narrow' },
    h('h2', {}, 'Anmelden'),
    h('p', {}, 'vibetype nutzt Passkeys – kein Passwort, keine E-Mail.'),
    h('button', { class: 'primary', onclick: run(loginPasskey) }, 'Mit Passkey anmelden'),
    h('hr'),
    h('h3', {}, 'Neu hier?'),
    h('form', { onsubmit: run(() => registerPasskey(name.value)) }, name, h('button', {}, 'Passkey erstellen')),
    msg,
  );
}

// ---------- on-screen keyboard ----------
// Preferences are per browser (global, not per user): { keyboard: bool, layout: 'de'|'us' }

function loadPrefs() {
  const p = { keyboard: true, layout: defaultLayout(navigator.language), ...ls.get('prefs', {}) };
  if (!LAYOUTS[p.layout]) p.layout = defaultLayout(navigator.language); // unknown id (old/new version, hand-edited)
  return p;
}

// keyboardWidget returns { el, show(nextChar), wrong(typedChar) }.
// refocus() is called whenever a control is done, so keystrokes go back to the text.
function keyboardWidget(refocus) {
  let prefs = loadPrefs();
  const $kb = h('div', { class: 'kb', 'aria-hidden': 'true' });
  const $hint = h('div', { class: 'kb-hint', 'aria-live': 'polite' });
  const $toggle = h('input', { type: 'checkbox' });
  const $layout = h('select', { 'aria-label': 'Tastaturlayout' }, Object.entries(LAYOUTS).map(([id, l]) => h('option', { value: id }, l.name)));
  const el = h('div', { class: 'kb-wrap' },
    h('div', { class: 'kb-controls muted' }, h('label', {}, $toggle, ' Tastatur anzeigen'), ' ', $layout),
    $hint, $kb);
  let keys = {};
  let next = null;

  function build() {
    $toggle.checked = prefs.keyboard;
    $layout.value = prefs.layout;
    $layout.hidden = $kb.hidden = $hint.hidden = !prefs.keyboard;
    keys = {};
    fill($kb, ...LAYOUTS[prefs.layout].rows.map((row) => h('div', { class: 'kb-row' }, row.map((key) => {
      const letter = !key.mod && key.shift && key.base.toUpperCase() === key.shift;
      const label = key.mod ? key.label : key.id === 'Space' ? '' : key.id === 'Enter' ? '↵' : letter ? key.shift : key.base;
      const $k = h('div', { class: `key f-${key.finger}${key.mod ? ' mod' : ''}${['f', 'j'].includes(key.id) ? ' bump' : ''}` },
        !key.mod && key.shift && !letter ? h('span', { class: 'sub' }, key.shift) : null,
        label,
        key.altgr ? h('span', { class: 'alt' }, key.altgr) : null);
      $k.style.flexGrow = String(key.width); // CSSOM is allowed by the CSP, style attributes are not
      keys[key.id] = $k;
      return $k;
    }))));
    show(next);
  }
  function show(ch) {
    next = ch;
    for (const $k of Object.values(keys)) $k.classList.remove('next');
    if (ch === null || !prefs.keyboard) { $hint.textContent = ''; return; }
    const loc = locate(ch, prefs.layout);
    $hint.textContent = (ch === ' ' ? 'Leertaste' : ch === '\n' ? 'Enter' : `„${ch}“`) + ': ' + hint(ch, prefs.layout);
    for (const step of loc ? loc.steps : []) {
      keys[step.key]?.classList.add('next');
      if (step.modifier) keys[step.modifier]?.classList.add('next');
    }
  }
  function wrong(ch) {
    const loc = prefs.keyboard && locate(ch, prefs.layout);
    if (!loc) return;
    const step = loc.steps[loc.steps.length - 1];
    for (const id of [step.key, step.modifier]) {
      const $k = id && keys[id];
      if (!$k) continue;
      $k.classList.remove('wrong');
      void $k.offsetWidth;
      $k.classList.add('wrong');
    }
  }
  function save() { ls.set('prefs', prefs); }
  $toggle.addEventListener('change', () => { prefs = { ...prefs, keyboard: $toggle.checked }; save(); build(); refocus(); });
  $layout.addEventListener('change', () => { prefs = { ...prefs, layout: $layout.value }; save(); build(); refocus(); });
  // closing the select with Esc or re-picking the same option fires no change event
  $layout.addEventListener('blur', () => setTimeout(refocus));
  $layout.addEventListener('keydown', (e) => { if (e.key === 'Escape') refocus(); });
  build();
  return { el, show, wrong };
}

// ---------- routing ----------

let teardown = () => {};

function navigate(path) {
  history.pushState(null, '', path);
  route();
}
window.addEventListener('popstate', route);

async function route() {
  teardown();
  teardown = () => {};
  const m = location.pathname.match(/^\/t\/([a-z0-9]+)$/);
  if (!me) { fill($app, loginView()); return; }
  if (m) await typingView(m[1]);
  else await homeView();
}

// ---------- home: my texts, create text, open by code ----------

async function homeView() {
  const title = h('input', { type: 'text', placeholder: 'Titel', maxlength: 200, required: true });
  const content = h('textarea', { rows: 8, placeholder: 'Text zum Abtippen …', required: true });
  const code = h('input', { type: 'text', placeholder: 'Code, z. B. a7x9k2', pattern: '[a-z0-9]+', required: true, autocapitalize: 'off' });
  const list = h('ul', { class: 'texts' }, h('li', { class: 'muted' }, 'lade …'));
  const results = h('div', {}, h('p', { class: 'muted' }, 'lade …'));

  fill($app, 
    h('section', { class: 'card' },
      h('h2', {}, 'Text öffnen'),
      h('form', { class: 'row', onsubmit: (e) => { e.preventDefault(); navigate('/t/' + code.value.trim().toLowerCase()); } },
        code, h('button', {}, 'Los')),
    ),
    h('section', { class: 'card' },
      h('h2', {}, 'Neuer Text'),
      h('form', {
        onsubmit: async (e) => {
          e.preventDefault();
          try {
            const t = await api('POST', '/api/texts', { title: title.value, content: content.value });
            us.set('text:' + t.id, t);
            navigate('/t/' + t.id);
          } catch (err) { fail(`Text konnte nicht gespeichert werden: ${err.message}`); }
        },
      }, title, content, h('button', { class: 'primary' }, 'Speichern & teilen')),
    ),
    h('section', { class: 'card' }, h('h2', {}, 'Meine Texte'), list),
    h('section', { class: 'card' }, h('h2', {}, 'Meine Ergebnisse'), results),
  );
  loadResults(results);

  try {
    const texts = await api('GET', '/api/texts');
    fill(list, ...(texts.length ? texts.map((t) => h('li', {},
      h('a', { href: '/t/' + t.id, 'data-link': true }, t.title), ' ',
      h('code', {}, t.id), ' ',
      h('span', { class: 'muted' }, `${chars(t.content).length} Zeichen`), ' ',
      h('button', {
        class: 'link danger',
        onclick: async () => {
          if (!confirm(`Text „${t.title}“ und alle Ergebnisse dazu (auch die anderer Personen) endgültig löschen?`)) return;
          try {
            await api('DELETE', '/api/texts/' + t.id);
            us.del('text:' + t.id);
            us.del('session:' + t.id);
            homeView();
          } catch (err) { fail(`Löschen fehlgeschlagen: ${err.message}`); }
        },
      }, 'löschen'),
    )) : [h('li', { class: 'muted' }, 'Noch keine Texte.')]));
  } catch (err) {
    fill(list, h('li', { class: isOffline(err) ? 'warn' : 'bad' },
      isOffline(err) ? 'offline – Liste nicht verfügbar' : err.message));
  }
}

// ---------- results ----------

const fmtDuration = (ms) => {
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};
const fmtDate = (iso) => new Date(iso).toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' });
const resultAccuracy = (r) => accuracy({ cursor: r.cursor_pos, errors: r.error_count });

// bestByText maps text_id -> id of the finished result with the highest cpm
function bestByText(results) {
  const best = {};
  for (const r of results) {
    if (!r.finished_at) continue;
    if (!best[r.text_id] || r.cpm > best[r.text_id].cpm) best[r.text_id] = r;
  }
  return Object.fromEntries(Object.entries(best).map(([k, r]) => [k, r.id]));
}

async function loadResults($el) {
  let data;
  try { data = await api('GET', '/api/me/results'); }
  catch (err) {
    fill($el, h('p', { class: isOffline(err) ? 'warn' : 'bad' }, isOffline(err) ? 'offline – Ergebnisse nicht verfügbar' : err.message));
    return;
  }
  const { results } = data;
  if (!results.length) { fill($el, h('p', { class: 'muted' }, 'Noch keine Ergebnisse. Öffne einen Text und tippe los.')); return; }
  const best = bestByText(results);
  fill($el, h('table', { class: 'results' },
    h('thead', {}, h('tr', {}, h('th', {}, 'Datum'), h('th', {}, 'Text'), h('th', {}, 'Fortschritt'), h('th', { class: 'num' }, 'Anschl./min'),
      h('th', { class: 'num' }, 'Fehler'), h('th', { class: 'num' }, 'Genauigkeit'), h('th', { class: 'num' }, 'Zeit'))),
    h('tbody', {}, results.map((r) => h('tr', {},
      h('td', {}, fmtDate(r.started_at)),
      h('td', {}, h('a', { href: '/t/' + r.text_id, 'data-link': true }, r.text_title), best[r.text_id] === r.id ? h('span', { class: 'best', title: 'Bestleistung' }, ' ★') : null),
      h('td', {}, r.finished_at ? 'fertig' : `${Math.floor((r.cursor_pos / r.text_length) * 100)} %`),
      h('td', { class: 'num' }, r.cpm),
      h('td', { class: 'num' }, r.error_count),
      h('td', { class: 'num' }, resultAccuracy(r), ' %'),
      h('td', { class: 'num' }, r.finished_at ? fmtDuration(Date.parse(r.finished_at) - Date.parse(r.started_at)) : '–'),
    )))));
}

// ---------- typing view ----------

async function loadText(id) {
  try {
    const t = await api('GET', '/api/texts/' + id);
    us.set('text:' + id, t);
    return t;
  } catch (err) {
    const cached = us.get('text:' + id);
    if (isOffline(err) && cached) return cached;
    throw err;
  }
}

async function typingView(textId) {
  fill($app, h('p', { class: 'muted' }, 'lade Text …'));
  let text;
  try { text = await loadText(textId); }
  catch (err) {
    fill($app, h('section', { class: 'card' },
      h('p', { class: 'bad' }, err.status === 404 ? `Text „${textId}" gibt es nicht.` : `Fehler: ${err.message}`),
      h('a', { href: '/', 'data-link': true }, 'zurück')));
    return;
  }
  const textChars = chars(text.content);
  const stateKey = 'session:' + textId;
  let state = us.get(stateKey);
  if (!state) { state = newSession(crypto.randomUUID(), textId); us.set(stateKey, state); }

  const $done = h('span', { class: 'done' });
  const $cur = h('span', { class: 'cur' });
  const $rest = h('span', { class: 'rest' });
  const $typing = h('div', { class: 'typing', tabindex: 0 }, $done, $cur, $rest);
  const $stats = h('div', { class: 'stats' });
  const $summary = h('div', { class: 'summary', hidden: true });
  // previous best (finished, highest cpm) of this user on this text; null = none, undefined = unknown (offline)
  let previousBest;
  api('GET', '/api/me/results?text_id=' + encodeURIComponent(textId))
    .then(({ results }) => {
      previousBest = results.filter((r) => r.finished_at && r.id !== state.id).reduce((b, r) => (!b || r.cpm > b.cpm ? r : b), null);
      renderSummary();
    })
    .catch((err) => { if (!isOffline(err)) fail(`Ergebnisse konnten nicht geladen werden: ${err.message}`); });
  const kb = keyboardWidget(() => { if (document.activeElement !== $typing) $typing.focus(); });
  const $live = h('div', { class: 'live' });
  const shareUrl = `${location.origin}/t/${text.id}`;

  const display = (c) => (c === '\n' ? '↵\n' : c);
  function render() {
    $done.textContent = textChars.slice(0, state.cursor).join('');
    $cur.textContent = state.cursor < textChars.length ? display(textChars[state.cursor]) : '';
    $rest.textContent = textChars.slice(state.cursor + 1).join('');
    kb.show(state.cursor < textChars.length ? textChars[state.cursor] : null);
    renderStats();
    renderSummary();
  }
  function renderStats() {
    const pct = Math.floor((state.cursor / textChars.length) * 100);
    fill($stats, ...[
      h('span', {}, h('b', {}, pct, ' %'), ' Fortschritt'),
      h('span', {}, h('b', {}, cpm(state, Date.now())), ' Anschläge/min'),
      h('span', {}, h('b', {}, state.errors), ' Fehler'),
      h('span', {}, h('b', {}, accuracy(state), ' %'), ' Genauigkeit'),
      state.finishedAt ? h('button', { class: 'primary', onclick: restart }, 'Nochmal') : null,
    ].filter(Boolean));
  }
  function renderSummary() {
    $summary.hidden = !state.finishedAt;
    if (!state.finishedAt) return;
    const myCpm = cpm(state, Date.now());
    let verdict = null;
    if (previousBest === null) verdict = h('p', {}, 'Dein erstes Ergebnis für diesen Text.');
    else if (previousBest && myCpm > previousBest.cpm) verdict = h('p', { class: 'good' }, `★ Neue Bestleistung! Vorher ${previousBest.cpm} Anschläge/min.`);
    else if (previousBest) verdict = h('p', {}, `Deine Bestleistung: ${previousBest.cpm} Anschläge/min (${resultAccuracy(previousBest)} % Genauigkeit).`);
    fill($summary, ...[
      h('h3', {}, 'Fertig!'),
      h('p', {}, `${myCpm} Anschläge/min · ${accuracy(state)} % Genauigkeit · ${state.errors} Fehler · Zeit ${fmtDuration(Date.parse(state.finishedAt) - Date.parse(state.startedAt))}`),
      verdict,
    ].filter(Boolean));
  }
  function restart() {
    if (state.finishedAt && previousBest !== undefined) {
      // the run just finished becomes the reference for the next one
      const mine = { id: state.id, cpm: cpm(state, Date.now()), cursor_pos: state.cursor, error_count: state.errors, finished_at: state.finishedAt };
      if (!previousBest || mine.cpm > previousBest.cpm) previousBest = mine;
    }
    state = newSession(crypto.randomUUID(), textId);
    us.set(stateKey, state);
    render();
    $typing.focus();
  }
  function flash() {
    $cur.classList.remove('error-flash');
    void $cur.offsetWidth; // restart CSS animation
    $cur.classList.add('error-flash');
  }

  function onKey(ev) {
    if (document.activeElement && ['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement.tagName)) return;
    const ch = keyToChar(ev);
    if (ch === null) return;
    ev.preventDefault(); // no page scroll on space, no Firefox quick-find on ' and /
    const result = press(state, textChars, ch, Date.now());
    if (result === 'ignored') return;
    if (result === 'error') { flash(); kb.wrong(ch); }
    us.set(stateKey, state);
    enqueue(state); // dirty flag: picked up by the sync loop
    render();
    $cur.scrollIntoView({ block: 'nearest' });
  }
  document.addEventListener('keydown', onKey);
  const statsTimer = setInterval(renderStats, 1000);

  const isCreator = text.creator_id === me.id;
  let liveTimer = null;
  async function pollLive() {
    try {
      const data = await api('GET', `/api/texts/${text.id}/sessions`);
      renderLive(data);
    } catch (err) {
      fill($live, h('p', { class: isOffline(err) ? 'warn' : 'bad' },
        isOffline(err) ? 'Live-Ansicht: offline' : `Live-Ansicht: ${err.message}`));
    }
  }
  function renderLive({ server_time, sessions }) {
    const now = Date.parse(server_time);
    const row = (s) => {
      const finished = !!s.finished_at;
      const active = !finished && now - Date.parse(s.updated_at) < ACTIVE_WINDOW_MS;
      const pct = Math.floor((s.cursor_pos / textChars.length) * 100);
      return h('tr', { class: active ? 'active' : '' },
        h('td', {}, s.user_name),
        h('td', {}, h('div', { class: 'bar' }, h('div', { class: 'fill', 'data-pct': pct })), ` ${pct} %`),
        h('td', { class: 'num' }, s.cpm),
        h('td', { class: 'num' }, s.error_count),
        h('td', {}, finished ? 'fertig' : active ? 'tippt' : 'inaktiv'),
      );
    };
    fill($live, 
      h('h2', {}, 'Live-Ansicht ', h('span', { class: 'muted' }, `(${sessions.length})`)),
      sessions.length === 0 ? h('p', { class: 'muted' }, 'Noch tippt niemand diesen Text.') :
        h('table', {},
          h('thead', {}, h('tr', {}, h('th', {}, 'Name'), h('th', {}, 'Fortschritt'), h('th', {}, 'Anschl./min'), h('th', {}, 'Fehler'), h('th', {}, 'Status'))),
          h('tbody', {}, sessions.map(row))),
    );
    // CSP forbids inline style attributes; set widths via CSSOM instead
    for (const el of $live.querySelectorAll('.fill')) el.style.width = el.dataset.pct + '%';
  }
  if (isCreator) {
    pollLive();
    liveTimer = setInterval(pollLive, LIVE_INTERVAL_MS);
  }

  fill($app, 
    h('section', { class: 'card' },
      h('div', { class: 'head' },
        h('h2', {}, text.title),
        h('span', { class: 'muted' }, 'von ', text.creator_name, ' · Code ', h('code', {}, text.id), ' · ',
          h('button', {
            class: 'link',
            onclick: async (e) => {
              try { await navigator.clipboard.writeText(shareUrl); e.target.textContent = 'kopiert ✓'; }
              catch (err) { fail(`Kopieren fehlgeschlagen: ${err.message} – Link: ${shareUrl}`); }
            },
          }, 'Link kopieren')),
      ),
      $typing,
      $stats,
      $summary,
      kb.el,
    ),
    ...(isCreator ? [h('section', { class: 'card' }, $live)] : []),
  );
  render();
  $typing.focus();

  teardown = () => {
    document.removeEventListener('keydown', onKey);
    clearInterval(statsTimer);
    if (liveTimer) clearInterval(liveTimer);
  };
}

// ---------- boot ----------

document.addEventListener('click', (e) => {
  const a = e.target.closest('a[data-link]');
  if (!a || e.ctrlKey || e.metaKey || e.shiftKey) return;
  e.preventDefault();
  navigate(a.getAttribute('href'));
});

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch((err) => fail(`Offline-Modus nicht verfügbar: ${err.message}`));
}

await loadMe();
route();
flush();
