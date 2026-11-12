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
await A.page.goto(`${BASE}/t/${slug}`);

const B = await newUser('Bob'); step('Bob registered');
await B.page.goto(`${BASE}/t/${slug}`);
await B.page.waitForSelector('.typing');
if (await B.page.$('.live')) throw new Error('Bob must not see live view');
await B.page.keyboard.type('Hal');
await B.page.keyboard.press('x');
const flashed = await B.page.$eval('.cur', (el) => el.classList.contains('error-flash') && el.textContent);
if (flashed !== 'l') throw new Error('expected error flash on "l", got ' + flashed);
await B.page.keyboard.press('Shift'); // modifier ignored
await B.page.keyboard.type('lo');
const done = await B.page.$eval('.done', (el) => el.textContent);
if (done !== 'Hallo') throw new Error('done=' + done);
step('stop-on-error works');

await A.page.waitForSelector('tr.active:has-text("Bob"):has-text("41 %")', { timeout: 10000 });
const row = await A.page.$eval('tr.active', (el) => el.innerText);
step('Alice live view: ' + row.replace(/\s+/g, ' '));

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

const relevant = errors.filter((e) => !e.includes('net::ERR_INTERNET_DISCONNECTED') && !e.includes('Failed to fetch') && !e.includes('401 (Unauthorized)') && !e.includes('400 (Bad Request)') /* /api/me before login */);
console.log('console errors:', relevant.length ? relevant : 'none', `(${errors.length - relevant.length} expected offline fetch errors)`);
await browser.close();
if (relevant.length) process.exit(1);
