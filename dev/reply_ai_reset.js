/* Dev-only helper: reset the reply_ai settings row to its default value. */
const base = 'http://localhost:9173';

async function login(username, password) {
  const cj = {};
  let res = await fetch(`${base}/admin/login`, { redirect: 'manual' });
  for (const sc of res.headers.getSetCookie()) {
    const name = sc.split('=')[0];
    cj[name] = sc.split(';')[0];
  }
  const html = await res.text();
  const nonce = /name="nonce" value="([^"]+)"/.exec(html)[1];
  const cookie = Object.values(cj).join('; ');
  res = await fetch(`${base}/admin/login`, {
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

(async () => {
  const cookie = await login('root', 'Test@1234');
  const value = JSON.stringify({
    enabled: false,
    base_url: '',
    api_key: '',
    model: '',
    timeout: '15s',
    min_confidence: 0.98,
  });
  const res = await fetch(`${base}/api/settings/reply_ai`, {
    method: 'PUT',
    headers: { cookie, 'content-type': 'application/json', 'X-Listmonk-Organization-ID': '0' },
    body: value,
  });
  console.log(`reset reply_ai -> HTTP ${res.status}`);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
