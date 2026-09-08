/* Dev-only helper: enable the reply-AI classifier through the settings API.
 * Usage: node dev/reply_ai_enable_e2e.js [base_url] [model] [key]
 */
const base = 'http://localhost:9173';

async function fetchRetry(url, opts, tries = 4) {
  for (let i = 0; i < tries; i += 1) {
    try {
      return await fetch(url, opts);
    } catch (err) {
      if (i === tries - 1) throw err;
      await new Promise((r) => setTimeout(r, 2500));
    }
  }
  throw new Error('unreachable');
}

async function settle() {
  await new Promise((r) => setTimeout(r, 3000));
}

async function login(username, password) {
  const cj = {};
  let res = await fetchRetry(`${base}/admin/login`, { redirect: 'manual' });
  for (const sc of res.headers.getSetCookie()) {
    const name = sc.split('=')[0];
    cj[name] = sc.split(';')[0];
  }
  const html = await res.text();
  const nonce = /name="nonce" value="([^"]+)"/.exec(html)[1];
  const cookie = Object.values(cj).join('; ');
  res = await fetchRetry(`${base}/admin/login`, {
    method: 'POST',
    redirect: 'manual',
    headers: { 'content-type': 'application/x-www-form-urlencoded', cookie },
    body: `username=${username}&password=${password}&nonce=${nonce}&next=/admin`,
  });
  for (const sc of res.headers.getSetCookie()) {
    const name = sc.split('=')[0];
    cj[name] = sc.split(';')[0];
  }
  return Object.values(cj).join('; ');
}

async function api(cookie, method, body) {
  const res = await fetchRetry(`${base}/api/settings`, {
    method,
    headers: { cookie, 'content-type': 'application/json', 'X-Listmonk-Organization-ID': '0' },
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

(async () => {
  const baseUrl = process.argv[2] || 'http://host.docker.internal:9456/v1';
  const model = process.argv[3] || 'mock-v1';
  const apiKey = process.argv[4] || 'sk-e2e-mock';
  const cookie = await login('root', 'Test@1234');
  const g = await api(cookie, 'GET');
  if (g.status !== 200) throw new Error(`GET settings failed: ${g.status}`);
  const full = stripMasked(JSON.parse(JSON.stringify(g.json.data)));
  full.reply_ai = {
    enabled: true,
    base_url: baseUrl,
    api_key: apiKey,
    model,
    timeout: '5s',
    min_confidence: 0.9,
  };
  const p = await api(cookie, 'PUT', full);
  console.log(`enable reply_ai -> HTTP ${p.status}`);
  if (p.status !== 200) {
    console.log(JSON.stringify(p.json));
    process.exit(1);
  }
  await settle();
  const g2 = await api(cookie, 'GET');
  console.log(`reply_ai now: ${JSON.stringify(g2.json.data.reply_ai)}`);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
