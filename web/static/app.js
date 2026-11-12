// vibetype frontend – vanilla ES module, no build step.
import { newSession, chars, keyToChar, press, cpm, accuracy, syncPayload } from './engine.js';

const SYNC_INTERVAL_MS = 3000;
const LIVE_INTERVAL_MS = 3000;
const ACTIVE_WINDOW_MS = 60000; // a session counts as "typing" if synced within this window

const $app = document.getElementById('app');
const $status = document.getElementById('status');
const $user = document.getElementById('user');

// ---------- localStorage helpers (texts, me, current sessions, sync queue) ----------

const ls = {
  get(key, def = null) {
    const raw = localStorage.getItem('vt:' + key);
    return raw === null ? def : JSON.parse(raw);
  },
  set(key, value) { localStorage.setItem('vt:' + key, JSON.stringify(value)); },
  del(key) { localStorage.removeItem('vt:' + key); },
};

// ---------- tiny DOM helper ----------

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

const isOffline = (err) => !(err instanceof ApiError); // fetch threw: network down

// ---------- status bar ----------

const status = { sync: 'ok', error: null };

function renderStatus() {
  $status.replaceChildren();
  if (status.error) {
    $status.append(h('span', { class: 'bad' }, status.error, ' ',
      h('button', { class: 'link', onclick: () => { status.error = null; renderStatus(); } }, '×')));
  }
  const pending = Object.keys(ls.get('pending', {})).length;
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
  const pending = ls.get('pending', {});
  pending[state.id] = syncPayload(state, Date.now());
  ls.set('pending', pending);
}

let syncing = false;
async function flush() {
  if (syncing) return;
  syncing = true;
  try {
    const pending = ls.get('pending', {});
    for (const [id, payload] of Object.entries(pending)) {
      let rejected = false;
      try {
        await api('PUT', `/api/sessions/${encodeURIComponent(id)}`, payload);
        status.sync = 'ok';
      } catch (err) {
        if (isOffline(err)) { status.sync = 'offline'; break; }
        if (err.status === 401) { status.sync = 'unauthorized'; break; }
        if (err.status >= 500) { fail(`Sync-Fehler auf dem Server (${err.status}): ${err.message}`); break; }
        // Permanent rejection (4xx): report loudly and drop, retrying cannot help.
        fail(`Sync abgelehnt (${err.status}): ${err.message}`);
        rejected = true;
      }
      // remove only if no newer state was queued meanwhile
      const now = ls.get('pending', {});
      if (rejected || JSON.stringify(now[id]) === JSON.stringify(payload)) {
        delete now[id];
        ls.set('pending', now);
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

let me = null;

async function loadMe() {
  try {
    me = await api('GET', '/api/me');
    ls.set('me', me);
  } catch (err) {
    if (isOffline(err)) me = ls.get('me'); // offline: trust the last known login
    else { me = null; ls.del('me'); }
  }
  renderUser();
}

function renderUser() {
  $user.replaceChildren();
  if (!me) return;
  $user.append(me.display_name, ' · ', h('button', {
    class: 'link',
    onclick: async () => {
      try { await api('POST', '/api/logout'); } catch (err) { fail(`Abmelden fehlgeschlagen: ${err.message}`); return; }
      me = null; ls.del('me'); renderUser(); route();
    },
  }, 'abmelden'));
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
  if (!me) { $app.replaceChildren(loginView()); return; }
  if (m) await typingView(m[1]);
  else await homeView();
}

// ---------- home: my texts, create text, open by code ----------

async function homeView() {
  const title = h('input', { type: 'text', placeholder: 'Titel', maxlength: 200, required: true });
  const content = h('textarea', { rows: 8, placeholder: 'Text zum Abtippen …', required: true });
  const code = h('input', { type: 'text', placeholder: 'Code, z. B. a7x9k2', pattern: '[a-z0-9]+', required: true, autocapitalize: 'off' });
  const list = h('ul', { class: 'texts' }, h('li', { class: 'muted' }, 'lade …'));

  $app.replaceChildren(
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
            ls.set('text:' + t.id, t);
            navigate('/t/' + t.id);
          } catch (err) { fail(`Text konnte nicht gespeichert werden: ${err.message}`); }
        },
      }, title, content, h('button', { class: 'primary' }, 'Speichern & teilen')),
    ),
    h('section', { class: 'card' }, h('h2', {}, 'Meine Texte'), list),
  );

  try {
    const texts = await api('GET', '/api/texts');
    list.replaceChildren(...(texts.length ? texts.map((t) => h('li', {},
      h('a', { href: '/t/' + t.id, 'data-link': true }, t.title), ' ',
      h('code', {}, t.id), ' ',
      h('span', { class: 'muted' }, `${chars(t.content).length} Zeichen`),
    )) : [h('li', { class: 'muted' }, 'Noch keine Texte.')]));
  } catch (err) {
    list.replaceChildren(h('li', { class: isOffline(err) ? 'warn' : 'bad' },
      isOffline(err) ? 'offline – Liste nicht verfügbar' : err.message));
  }
}

// ---------- typing view ----------

async function loadText(id) {
  try {
    const t = await api('GET', '/api/texts/' + id);
    ls.set('text:' + id, t);
    return t;
  } catch (err) {
    const cached = ls.get('text:' + id);
    if (isOffline(err) && cached) return cached;
    throw err;
  }
}

async function typingView(textId) {
  $app.replaceChildren(h('p', { class: 'muted' }, 'lade Text …'));
  let text;
  try { text = await loadText(textId); }
  catch (err) {
    $app.replaceChildren(h('section', { class: 'card' },
      h('p', { class: 'bad' }, err.status === 404 ? `Text „${textId}" gibt es nicht.` : `Fehler: ${err.message}`),
      h('a', { href: '/', 'data-link': true }, 'zurück')));
    return;
  }
  const textChars = chars(text.content);
  const stateKey = 'session:' + textId;
  let state = ls.get(stateKey);
  if (!state) { state = newSession(crypto.randomUUID(), textId); ls.set(stateKey, state); }

  const $done = h('span', { class: 'done' });
  const $cur = h('span', { class: 'cur' });
  const $rest = h('span', { class: 'rest' });
  const $typing = h('div', { class: 'typing', tabindex: 0 }, $done, $cur, $rest);
  const $stats = h('div', { class: 'stats' });
  const $live = h('div', { class: 'live' });
  const shareUrl = `${location.origin}/t/${text.id}`;

  const display = (c) => (c === '\n' ? '↵\n' : c);
  function render() {
    $done.textContent = textChars.slice(0, state.cursor).join('');
    $cur.textContent = state.cursor < textChars.length ? display(textChars[state.cursor]) : '';
    $rest.textContent = textChars.slice(state.cursor + 1).join('');
    renderStats();
  }
  function renderStats() {
    const pct = Math.floor((state.cursor / textChars.length) * 100);
    $stats.replaceChildren(...[
      h('span', {}, h('b', {}, pct, ' %'), ' Fortschritt'),
      h('span', {}, h('b', {}, cpm(state, Date.now())), ' Anschläge/min'),
      h('span', {}, h('b', {}, state.errors), ' Fehler'),
      h('span', {}, h('b', {}, accuracy(state), ' %'), ' Genauigkeit'),
      state.finishedAt ? h('button', { class: 'primary', onclick: restart }, 'Nochmal') : null,
    ].filter(Boolean));
  }
  function restart() {
    state = newSession(crypto.randomUUID(), textId);
    ls.set(stateKey, state);
    render();
    $typing.focus();
  }
  function flash() {
    $cur.classList.remove('error-flash');
    void $cur.offsetWidth; // restart CSS animation
    $cur.classList.add('error-flash');
  }

  function onKey(ev) {
    if (document.activeElement && ['INPUT', 'TEXTAREA'].includes(document.activeElement.tagName)) return;
    const ch = keyToChar(ev);
    if (ch === null) return;
    ev.preventDefault(); // no page scroll on space, no Firefox quick-find on ' and /
    const result = press(state, textChars, ch, Date.now());
    if (result === 'ignored') return;
    if (result === 'error') flash();
    ls.set(stateKey, state);
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
      $live.replaceChildren(h('p', { class: isOffline(err) ? 'warn' : 'bad' },
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
    $live.replaceChildren(
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

  $app.replaceChildren(
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
