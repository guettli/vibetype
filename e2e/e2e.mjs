// End-to-end test: two users with virtual passkeys, stop-on-error, live view, offline sync.
import { chromium } from 'playwright';
// Usage: start vibetype with -origin $BASE, then: node e2e/e2e.mjs
const BASE = process.env.BASE || 'http://localhost:8099';
const errors = [];
const browser = await chromium.launch();

async function newUser(name) {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  page.on('console', (m) => { if (m.type() === 'error') errors.push(`${name}: ${m.text()}`); });
  page.on('pageerror', (e) => errors.push(`${name}: ${e.message}`));
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator', { options: {
    protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true,
    isUserVerified: true, automaticPresenceSimulation: true } });
  await page.goto(BASE + '/');
  await page.fill('input[placeholder^="Dein Name"]', name);
  await page.click('text=Passkey erstellen');
  try { await page.waitForSelector(`#user:has-text("${name}")`, { timeout: 8000 }); }
  catch (e) { console.log('BODY:', await page.innerText('body'), errors); throw e; }
  return { ctx, page };
}
const step = (s) => console.log('✓', s);

const A = await newUser('Alice'); step('Alice registered');
await A.page.fill('input[placeholder="Titel"]', 'Test');
await A.page.fill('textarea', 'Hallo  „Welt“');
await A.page.click('text=Speichern & teilen');
await A.page.waitForURL(/\/t\/[a-z0-9]{6}$/);
const slug = A.page.url().split('/').pop();
await A.page.waitForSelector('text=Noch tippt niemand');
step(`text created ${slug}, live view empty`);

await A.page.click('text=abmelden');
await A.page.waitForSelector('text=Mit Passkey anmelden');
await A.page.click('text=Mit Passkey anmelden');
await A.page.waitForSelector('#user:has-text("Alice")');
step('Alice logout + passkey login (discoverable)');

// built-in lessons: list, type lesson 1 completely, summary links to lesson 2, home shows ✓
await A.page.goto(BASE + '/');
await A.page.waitForSelector('ol.lessons li a');
const lessonCount = await A.page.$$eval('ol.lessons li', (els) => els.length);
if (lessonCount !== 13) throw new Error('lessons: ' + lessonCount);
await A.page.click('ol.lessons li a >> nth=0');
await A.page.waitForURL(/\/t\/lek01$/);
await A.page.waitForSelector('.typing');
const lesson1 = await A.page.evaluate(() => fetch('/api/texts/lek01').then((r) => r.json()).then((t) => t.content));
// keyboard.type() sends no keydown for non-US characters like ö, so dispatch keydown events like a German keyboard would
await A.page.evaluate((text) => {
  const key = (k) => document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
  for (const ch of text) {
    if (ch === 'ö') key('p'); // Alice always misses the ö first -> it becomes a weak key
    key(ch === '\n' ? 'Enter' : ch);
  }
}, lesson1);
await A.page.waitForSelector('a.next-lesson[href="/t/lek02"]');
await A.page.waitForSelector('.summary:has-text("Dein erstes Ergebnis")');
step(`lesson 1 typed (${lesson1.length} chars): ` + (await A.page.textContent('.summary')).replace(/\s+/g, ' '));
await A.page.click('a.logo');
await A.page.waitForSelector('ol.lessons li.done');

// weak keys: ö is listed (every ö was missed once) and a practice text drills it
await A.page.waitForSelector('table.weak .keycap');
const weakFirst = await A.page.$eval('table.weak tr', (el) => el.innerText.replace(/\s+/g, ' '));
if (!weakFirst.startsWith('ö 50 %')) throw new Error('weak keys: ' + weakFirst);
step('weak key: ' + weakFirst);
await A.page.click('text=Übungstext für diese Tasten erstellen');
await A.page.waitForSelector('h2:has-text("Übung: ö")');
const practiceText = await A.page.textContent('.typing');
if (!practiceText.includes('ö')) throw new Error('practice text without ö: ' + practiceText);
step('practice text created: ' + practiceText.split('\n')[0]);
await A.page.goto(`${BASE}/t/${slug}`);

const B = await newUser('Bob'); step('Bob registered');
await B.page.goto(`${BASE}/t/${slug}`);
await B.page.waitForSelector('.typing');
if (await B.page.$('.live') || await B.page.$('img.qr')) throw new Error('Bob must not see live view or load the QR code');
// on-screen keyboard: "H" = right index finger + LEFT shift (other hand)
await B.page.evaluate(() => localStorage.setItem('vt:prefs', JSON.stringify({ keyboard: true, layout: 'de' })));
await B.page.reload();
await B.page.waitForSelector('.key.next');
const nextKeys = await B.page.$$eval('.key.next', (els) => els.map((e) => e.textContent));
const kbHint = await B.page.textContent('.kb-hint');
if (nextKeys.length !== 2 || !kbHint.includes('rechter Zeigefinger + linke Umschalttaste')) throw new Error(`keyboard hint wrong: ${nextKeys} / ${kbHint}`);
step('keyboard hint: ' + kbHint);
await B.page.click('text=Tastatur anzeigen');
if (await B.page.isVisible('.kb')) throw new Error('keyboard still visible after switching it off');
if (!await B.page.evaluate(() => document.activeElement.classList.contains('typing'))) throw new Error('focus not back on the text');
await B.page.click('text=Tastatur anzeigen');
await B.page.waitForSelector('.kb .key.next');
step('keyboard toggle hides the grid and returns focus to the text');
await B.page.keyboard.type('Hal');
await B.page.keyboard.press('x');
const flashed = await B.page.$eval('.cur', (el) => el.classList.contains('error-flash') && el.textContent);
if (flashed !== 'l') throw new Error('expected error flash on "l", got ' + flashed);
if (!await B.page.$('.key.wrong')) throw new Error('wrong key not flashed on keyboard');
await B.page.keyboard.press('Shift'); // modifier ignored
await B.page.keyboard.type('lo');
const done = await B.page.$eval('.done', (el) => el.textContent);
if (done !== 'Hallo') throw new Error('done=' + done);
step('stop-on-error works');

await A.page.waitForSelector('tr.active:has-text("Bob"):has-text("41 %")', { timeout: 10000 });
const row = await A.page.$eval('tr.active', (el) => el.innerText);
step('Alice live view: ' + row.replace(/\s+/g, ' '));

// classroom: QR code of the share link + presentation mode
await A.page.waitForFunction(() => document.querySelector('img.qr')?.naturalWidth > 0);
await A.page.click('text=Präsentieren');
await A.page.waitForSelector('.live-card.present');
await A.page.waitForSelector('text=Präsentation beenden');
if (process.env.SHOT_DIR) await A.page.screenshot({ path: process.env.SHOT_DIR + '/present.png' });
await A.page.click('text=Präsentation beenden');
await A.page.waitForSelector('.live-card:not(.present)');
step('QR code loads, presentation mode toggles');

await B.ctx.setOffline(true);
await B.page.keyboard.type(' "Welt"');
await B.page.waitForSelector('text=Nochmal');
await B.page.waitForSelector('#status:has-text("offline")', { timeout: 10000 });
step('finished offline, status: ' + await B.page.$eval('#status', (e) => e.innerText));
await B.page.reload();
await B.page.waitForSelector('.typing');
const doneAfterReload = await B.page.$eval('.done', (el) => el.textContent);
if (doneAfterReload !== 'Hallo "Welt"') throw new Error('after offline reload: ' + doneAfterReload);
step('offline reload restores text + progress (service worker + localStorage)');
await B.ctx.setOffline(false);
await A.page.waitForSelector('tr:has-text("Bob"):has-text("fertig")', { timeout: 15000 });
step('after reconnect Alice sees: ' + (await A.page.$eval('tbody tr', (el) => el.innerText)).replace(/\s+/g, ' '));
for (const p of [A.page, B.page]) if (/\bnull\b/.test(await p.innerText('body'))) throw new Error('stray null in DOM');

// finish summary + results on the home page
if (!(await B.page.textContent('.summary')).includes('Fertig!')) throw new Error('no finish summary');
await B.page.click('a.logo');
await B.page.waitForSelector('table.results');
const resultRow = await B.page.$eval('table.results tbody tr', (el) => el.innerText.replace(/\s+/g, ' '));
if (!resultRow.includes('★') || !resultRow.includes('fertig')) throw new Error('results row: ' + resultRow);
step('home shows result: ' + resultRow);

// logout wipes the user's offline data (shared classroom PCs)
await B.page.click('text=abmelden');
await B.page.waitForSelector('text=Mit Passkey anmelden');
const leftover = await B.page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('vt:u:')));
if (leftover.length) throw new Error('user data left after logout: ' + leftover);
step('logout clears user-scoped localStorage');

// untypable characters are rejected with a visible error
await A.page.goto(BASE + '/');
await A.page.fill('input[placeholder="Titel"]', 'Emoji');
await A.page.fill('textarea', 'Hallo 😀');
await A.page.click('text=Speichern & teilen');
await A.page.waitForSelector('#status:has-text("cannot be typed")');
step('emoji text rejected: ' + await A.page.$eval('#status', (e) => e.innerText));

// Bob types offline, the creator deletes the text meanwhile -> Bob gets a clear message on reconnect
await B.page.click('text=Mit Passkey anmelden');
await B.page.waitForSelector('#user:has-text("Bob")');
await B.page.goto(`${BASE}/t/${slug}`);
await B.page.waitForSelector('.typing');
await B.ctx.setOffline(true);
await B.page.keyboard.type('Ha');
A.page.once('dialog', (d) => d.accept());
await A.page.goto(BASE + '/');
await A.page.click(`li:has(code:text("${slug}")) >> text=löschen`);
await A.page.waitForSelector(`code:text("${slug}")`, { state: 'detached' });
step('creator deleted the text');
await B.ctx.setOffline(false);
await B.page.waitForSelector('#status:has-text("von seinem Ersteller gelöscht")', { timeout: 15000 });
const leftAfterDelete = await B.page.evaluate((id) => Object.keys(localStorage).filter((k) => k.endsWith(':' + id) || (k.endsWith(':pending') && localStorage[k] !== '{}')), slug);
if (leftAfterDelete.length) throw new Error('stale data for deleted text: ' + leftAfterDelete);
step('typist told about the deletion, local data dropped');

const relevant = errors.filter((e) => !e.includes('net::ERR_INTERNET_DISCONNECTED') && !e.includes('Failed to fetch') && !e.includes('401 (Unauthorized)') && !e.includes('400 (Bad Request)') && !e.includes('404 (Not Found)') /* /api/me before login */);
console.log('console errors:', relevant.length ? relevant : 'none', `(${errors.length - relevant.length} expected offline fetch errors)`);
await browser.close();
if (relevant.length) process.exit(1);
