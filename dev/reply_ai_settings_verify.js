/* Dev-only round-trip verification for the reply_ai settings block.
 * Logs in as the dev super admin and exercises the masked-secret semantics:
 *  1. GET settings (reply_ai.api_key must be masked or absent).
 *  2. PUT a full settings blob with a new reply_ai key -> 200.
 *  3. GET again: api_key must be masked; DB row holds the key (checked via a
 *     follow-up GET equivalence, since the key never leaves the server).
 *  4. PUT the same blob with api_key = '' (frontend dummy convention) ->
 *     key preserved (still masked on GET).
 *  5. PUT with enabled=true but base_url empty -> 400 (validation).
 */
const base = 'http://localhost:9173';

async function fetchRetry(url, opts, tries = 4) {
  for (let i = 0; i < tries; i += 1) {
    try {
      return await fetch(url, opts);
    } catch (err) {
      // The settings PUT reloads the server; tolerate dropped sockets.
      if (i === tries - 1) throw err;
      await new Promise((r) => setTimeout(r, 2500));
    }
  }
  throw new Error('unreachable');
}

// Settings updates restart the backend ~500ms after the response. Settle
// before the next authenticated call so the new process has booted.
async function settle() {
  await new Promise((r) => setTimeout(r, 3000));
}

async function login(username, password) {
  const cj = {};
  let res = await fetchRetry(`${base}/admin/login`, { redirect: 'manual' });
  const setCookies = res.headers.getSetCookie ? res.headers.getSetCookie() : [];
  for (const sc of setCookies) {
    const name = sc.split('=')[0];
    cj[name] = sc.split(';')[0];
  }
  const html = await res.text();
  const nonce = /name="nonce" value="([^"]+)"/.exec(html)[1];
  const cookie = Object.values(cj).join('; ');
  res = await fetchRetry(`${base}/admin/login`, {
    method: 'POST',
    redirect: 'manual',
    headers: {
      'content-type': 'application/x-www-form-urlencoded',
      cookie,
    },
    body: `username=${username}&password=${password}&nonce=${nonce}&next=/admin`,
  });
  for (const sc of res.headers.getSetCookie()) {
    const name = sc.split('=')[0];
    cj[name] = sc.split(';')[0];
  }
  return Object.values(cj).join('; ');
}

async function api(cookie, path, method, body) {
  const res = await fetchRetry(`${base}${path}`, {
    method: method || 'GET',
    headers: {
      cookie,
      'content-type': 'application/json',
      'X-Listmonk-Organization-ID': '0',
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const json = await res.json().catch(() => ({}));
  return { status: res.status, json };
}

function stripMasked(obj) {
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    if (typeof v === 'string' && v.length > 0 && /^•+$/.test(v)) {
      obj[k] = '';
    } else if (v && typeof v === 'object') {
      stripMasked(v);
    }
  }
  return obj;
}

function check(label, cond) {
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${label}`);
  if (!cond) process.exitCode = 1;
}

(async () => {
  const cookie = await login('root', 'Test@1234');
  const g1 = await api(cookie, '/api/settings');
  check('login + GET settings 200', g1.status === 200);
  check('reply_ai present', !!g1.json.data && 'reply_ai' in g1.json.data);

  const full = stripMasked(JSON.parse(JSON.stringify(g1.json.data)));
  full.reply_ai = {
    enabled: true,
    base_url: 'http://127.0.0.1:9999/v1',
    api_key: 'sk-roundtrip-secret',
    model: 'classifier-v1',
    timeout: '5s',
    min_confidence: 0.9,
  };
  const p1 = await api(cookie, '/api/settings', 'PUT', full);
  check('PUT with new key 200', p1.status === 200);
  await settle();

  const g2 = await api(cookie, '/api/settings');
  const key2 = g2.json.data.reply_ai.api_key || '';
  check('key masked on GET', /^•+$/.test(key2) && key2.length === 'sk-roundtrip-secret'.length);

  const full2 = stripMasked(JSON.parse(JSON.stringify(g2.json.data)));
  full2.reply_ai.api_key = ''; // frontend dummy -> keep stored key
  const p2 = await api(cookie, '/api/settings', 'PUT', full2);
  check('PUT blank key keeps stored key (200)', p2.status === 200);
  await settle();

  const g3 = await api(cookie, '/api/settings');
  const key3 = g3.json.data.reply_ai.api_key || '';
  check('key still masked after blank PUT', /^•+$/.test(key3) && key3.length === 'sk-roundtrip-secret'.length);

  const full3 = stripMasked(JSON.parse(JSON.stringify(g3.json.data)));
  full3.reply_ai.enabled = true;
  full3.reply_ai.base_url = '';
  const p3 = await api(cookie, '/api/settings', 'PUT', full3);
  check('enabled with empty base_url rejected (400)', p3.status === 400);

  // Reset to disabled to leave the dev instance inert.
  const full4 = stripMasked(JSON.parse(JSON.stringify(g3.json.data)));
  full4.reply_ai.enabled = false;
  full4.reply_ai.api_key = '';
  const p4 = await api(cookie, '/api/settings', 'PUT', full4);
  check('disable reply AI 200', p4.status === 200);
  await settle();
})().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
