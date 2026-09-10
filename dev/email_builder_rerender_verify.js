/* Dev-only browser verification for the email-builder render/subscription fix.
 *
 * The editor used to write the initial document and attach an onChange
 * subscription inside the React render body of App. Every re-render therefore
 * added another listener that was never released, so one document change invoked
 * the parent's onChange once per accumulated listener.
 *
 * This script drives a real headless Chrome over CDP against the built bundle
 * served by the dev backend, renders the editor, forces re-renders by toggling
 * the drawers through their own DOM buttons, and then asserts that a single
 * document change still notifies the parent exactly once.
 *
 * Usage: node dev/email_builder_rerender_verify.js [baseURL]
 */
const { spawn } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const BASE = process.argv[2] || 'http://localhost:9173';
const PORT = 9333;
const CHROME = [
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  path.join(process.env.LOCALAPPDATA || '', 'Google\\Chrome\\Application\\chrome.exe'),
].find((p) => p && fs.existsSync(p));

if (!CHROME) {
  console.error('Chrome not found; cannot verify in a browser.');
  process.exit(2);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function cdpTargets() {
  for (let i = 0; i < 60; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/list`);
      const list = await res.json();
      const page = list.find((t) => t.type === 'page' && t.webSocketDebuggerUrl);
      if (page) return page;
    } catch (e) { /* not up yet */ }
    await sleep(250);
  }
  throw new Error('Chrome DevTools endpoint did not come up');
}

// A minimal CDP client: enough for Runtime.evaluate on one page target.
function connect(url) {
  const ws = new WebSocket(url);
  let id = 0;
  const pending = new Map();
  const ready = new Promise((resolve, reject) => {
    ws.addEventListener('open', () => resolve());
    ws.addEventListener('error', (e) => reject(new Error(`websocket error: ${e.message || e.type}`)));
  });
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) reject(new Error(JSON.stringify(msg.error)));
      else resolve(msg.result);
    }
  });
  return {
    ready,
    send(method, params = {}) {
      const msgId = ++id;
      return new Promise((resolve, reject) => {
        pending.set(msgId, { resolve, reject });
        ws.send(JSON.stringify({ id: msgId, method, params }));
      });
    },
    close() { ws.close(); },
  };
}

const PAGE_TEST = `(async () => {
  const out = { ok: false, notes: [] };
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  // Load the built editor bundle the way VisualEditor.vue does.
  await new Promise((resolve, reject) => {
    if (window.EmailBuilder) return resolve();
    const s = document.createElement('script');
    s.src = '/admin/static/email-builder/email-builder.umd.js';
    s.onload = () => resolve();
    s.onerror = () => reject(new Error('email-builder bundle failed to load'));
    document.head.appendChild(s);
  });
  const em = window.EmailBuilder;
  out.exports = Object.keys(em);

  const container = document.createElement('div');
  container.id = 'dsh-eb-container';
  container.style.width = '900px';
  document.body.appendChild(container);

  let changes = 0;
  let lastDocument = null;
  em.render('dsh-eb-container', {
    data: {},
    onChange: (json) => { changes += 1; lastDocument = json; },
  });

  for (let i = 0; i < 100 && !container.hasChildNodes(); i++) await sleep(50);
  out.mounted = container.hasChildNodes();
  if (!out.mounted) { out.notes.push('editor never mounted'); return out; }

  // Baseline: with one subscription, a document change notifies once.
  changes = 0;
  em.setDocument({ root: { type: 'EmailLayout', data: { backdropColor: '#ff0000' } } });
  await sleep(150);
  out.baselineChanges = changes;

  // Force App to re-render repeatedly through the drawer toggle, which reads a
  // store value App subscribes to. The button swaps its icon with the drawer
  // state, so it has to be looked up again on every click.
  const findToggle = () => {
    const svg = document.querySelector(
      'svg[data-testid="LastPageOutlinedIcon"], svg[data-testid="AppRegistrationOutlinedIcon"]',
    );
    return svg ? svg.closest('button') : null;
  };
  const toggleIcon = () => {
    const svg = document.querySelector(
      'svg[data-testid="LastPageOutlinedIcon"], svg[data-testid="AppRegistrationOutlinedIcon"]',
    );
    return svg ? svg.getAttribute('data-testid') : null;
  };
  out.iconTestIds = [...document.querySelectorAll('svg[data-testid]')]
    .map((s) => s.getAttribute('data-testid'));
  out.toggleIconBefore = toggleIcon();

  let clicks = 0;
  // An odd number of clicks leaves the drawer in the opposite state, so the icon
  // comparison below proves the store actually changed.
  for (let round = 0; round < 5; round++) {
    const b = findToggle();
    if (!b) break;
    b.click();
    clicks += 1;
    await sleep(60);
  }
  out.clicks = clicks;
  await sleep(200);
  out.toggleIconAfter = toggleIcon();
  // The icon flips with the drawer state, which proves the clicks reached the
  // store and re-rendered App rather than being swallowed.
  out.drawerStateChanged = out.toggleIconBefore !== null
    && out.toggleIconAfter !== null
    && out.toggleIconBefore !== out.toggleIconAfter;

  // The accumulation bug shows up here: with N listeners attached by N renders,
  // one change fires onChange N times.
  changes = 0;
  em.setDocument({ root: { type: 'EmailLayout', data: { backdropColor: '#00ff00' } } });
  await sleep(200);
  out.changesAfterRerenders = changes;
  out.lastDocumentEdited = !!(lastDocument && lastDocument.root
    && lastDocument.root.data && lastDocument.root.data.backdropColor === '#00ff00');

  out.ok = out.baselineChanges === 1
    && out.changesAfterRerenders === 1
    && out.lastDocumentEdited === true
    && out.drawerStateChanged === true;

  return out;
})()`;

(async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'dsh-eb-chrome-'));
  const chrome = spawn(CHROME, [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${userDataDir}`,
    'about:blank',
  ], { stdio: 'ignore' });

  let client;
  try {
    const page = await cdpTargets();
    client = connect(page.webSocketDebuggerUrl);
    await client.ready;
    await client.send('Runtime.enable');
    await client.send('Page.enable');
    await client.send('Page.navigate', { url: `${BASE}/` });
    await sleep(1500);

    const res = await client.send('Runtime.evaluate', {
      expression: PAGE_TEST,
      awaitPromise: true,
      returnByValue: true,
    });
    if (res.exceptionDetails) {
      console.error('page exception:', JSON.stringify(res.exceptionDetails, null, 2));
      process.exitCode = 1;
      return;
    }

    const out = res.result.value;
    console.log(JSON.stringify(out, null, 2));
    console.log(out.ok
      ? 'PASS: one subscription survives re-renders (onChange fired once per change)'
      : 'FAIL: see the counters above');
    process.exitCode = out.ok ? 0 : 1;
  } catch (e) {
    console.error('verification error:', e.message);
    process.exitCode = 1;
  } finally {
    if (client) client.close();
    chrome.kill();
    try { fs.rmSync(userDataDir, { recursive: true, force: true }); } catch (e) { /* best effort */ }
  }
})();
