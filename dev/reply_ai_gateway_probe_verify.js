/* Dev-only verification for the reply-AI gateway probe endpoints
 * (POST /api/settings/reply-ai/models and /api/settings/reply-ai/test).
 *
 * Prerequisite: the mock gateway must be running on the host, reachable from the
 * backend container at host.docker.internal:9456:
 *     node dev/reply_ai_mock_openai.js 9456
 *
 * The script logs in as the dev super admin, exercises model discovery and the
 * model test against the mock, then restores an inert (disabled) reply_ai block.
 * It never prints or stores a real credential: the key used here is a mock.
 */
const base = 'http://localhost:9173';
const gateway = process.argv[2] || 'http://host.docker.internal:9456/v1';
const apiKey = 'sk-dev-mock';

async function fetchRetry(url, opts, tries = 4) {
  for (let i = 0; i < tries; i += 1) {
    try {
      return await fetch(url, opts);
    } catch (err) {
      // Settings updates restart the backend; tolerate dropped sockets.
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
  // A dev stack whose admin password is unknown can be verified with an existing
  // session id: QA_SESSION=<value of the `session` cookie>.
  if (process.env.QA_SESSION) {
    return `session=${process.env.QA_SESSION}`;
  }

  const cj = {};
  let res = await fetchRetry(`${base}/admin/login`, { redirect: 'manual' });
  for (const sc of res.headers.getSetCookie()) {
    cj[sc.split('=')[0]] = sc.split(';')[0];
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
    cj[sc.split('=')[0]] = sc.split(';')[0];
  }
  return Object.values(cj).join('; ');
}

async function post(cookie, path, body) {
  const res = await fetchRetry(`${base}${path}`, {
    method: 'POST',
    headers: { cookie, 'content-type': 'application/json', 'X-Listmonk-Organization-ID': '0' },
    body: JSON.stringify(body),
  });
  const text = await res.text();
  let json = {};
  try {
    json = JSON.parse(text);
  } catch (err) {
    // Error pages are not always JSON.
  }
  return { status: res.status, json, text };
}

async function settings(cookie, method, body) {
  const res = await fetchRetry(`${base}/api/settings`, {
    method,
    headers: { cookie, 'content-type': 'application/json', 'X-Listmonk-Organization-ID': '0' },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: res.status, json: await res.json().catch(() => ({})) };
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

function check(label, cond, extra) {
  console.log(`${cond ? 'PASS' : 'FAIL'}  ${label}${cond || extra === undefined ? '' : ` -> ${JSON.stringify(extra)}`}`);
  if (!cond) process.exitCode = 1;
}

function step(result, name) {
  return (result.steps || []).find((s) => s.name === name) || {};
}

(async () => {
  const cookie = await login('root', 'Test@1234');

  // --- Model discovery ------------------------------------------------------
  const list = await post(cookie, '/api/settings/reply-ai/models', {
    base_url: gateway, api_key: apiKey, timeout: '5s',
  });
  check('POST models 200', list.status === 200, list.json);
  const models = (list.json.data && list.json.data.models) || [];
  const ids = models.map((m) => m.id);
  check('model list contains the mock catalogue', ['mock-v1', 'gpt-4o-mini'].every((id) => ids.includes(id)), ids);
  check('embeddings flagged as non-chat', models.some((m) => m.id.startsWith('text-embedding') && m.chat_hint === false));
  check('chat candidates counted', list.json.data.chat_candidates === 3, list.json.data.chat_candidates);
  check('API key never echoed', !list.text.includes(apiKey));

  // --- Gateway failure mapping ---------------------------------------------
  const rejected = await post(cookie, '/api/settings/reply-ai/models', {
    base_url: gateway, api_key: 'bad-key', timeout: '5s',
  });
  check('rejected key -> 400', rejected.status === 400, rejected.json);
  check('rejected key message is localized and hides the key',
    !!rejected.json.message && !rejected.json.message.includes('bad-key'), rejected.json.message);

  const missing = await post(cookie, '/api/settings/reply-ai/models', {
    base_url: 'http://host.docker.internal:9456/nowhere', api_key: apiKey, timeout: '5s',
  });
  check('missing /models endpoint -> 502', missing.status === 502, missing.json);

  const noUrl = await post(cookie, '/api/settings/reply-ai/models', { base_url: '', api_key: apiKey });
  check('missing base URL -> 400', noUrl.status === 400, noUrl.json);

  // A blank key means "reuse the stored key", so its outcome depends on whether
  // the instance already holds one; that path is asserted after storing one below.

  // --- Model test -----------------------------------------------------------
  const ok = await post(cookie, '/api/settings/reply-ai/test', {
    base_url: gateway, api_key: apiKey, model: 'mock-v1', timeout: '5s',
  });
  check('POST test 200', ok.status === 200, ok.json);
  const okResult = ok.json.data || {};
  check('test passed', okResult.status === 'success', okResult);
  check('all four steps reported',
    ['config', 'gateway', 'model', 'completion'].every((n) => step(okResult, n).status === 'success'), okResult.steps);
  check('built-in sample expects unsubscribe', okResult.expected_intent === 'unsubscribe', okResult.expected_intent);
  check('decision parsed', okResult.decision && okResult.decision.intent === 'unsubscribe'
    && okResult.decision.reason_code === 'explicit_unsubscribe', okResult.decision);
  check('matched flag set', okResult.matched === true, okResult.matched);
  check('gateway reported the catalogue size', okResult.model_count === 5, okResult.model_count);
  check('test response never echoes the key', !ok.text.includes(apiKey));

  const custom = await post(cookie, '/api/settings/reply-ai/test', {
    base_url: gateway,
    api_key: apiKey,
    model: 'gpt-4o-mini',
    timeout: '5s',
    sample_text: 'I will report spam about this sender.',
    expected_intent: 'complaint',
  });
  check('custom sample + expectation passes', custom.json.data.status === 'success', custom.json.data);

  const mismatch = await post(cookie, '/api/settings/reply-ai/test', {
    base_url: gateway,
    api_key: apiKey,
    model: 'gpt-4o-mini',
    timeout: '5s',
    sample_text: 'I will report spam about this sender.',
    expected_intent: 'unsubscribe',
  });
  check('expectation mismatch -> warning', mismatch.json.data.status === 'warning', mismatch.json.data);
  check('mismatch reason recorded', step(mismatch.json.data, 'completion').reason === 'unexpected_intent',
    step(mismatch.json.data, 'completion'));

  const unlisted = await post(cookie, '/api/settings/reply-ai/test', {
    base_url: gateway, api_key: apiKey, model: 'not-a-real-model', timeout: '5s',
  });
  check('unlisted model warns but still calls the gateway',
    step(unlisted.json.data, 'model').reason === 'model_not_advertised', unlisted.json.data.steps);

  const badKey = await post(cookie, '/api/settings/reply-ai/test', {
    base_url: gateway, api_key: 'bad-key', model: 'mock-v1', timeout: '5s',
  });
  check('rejected key fails the completion step',
    badKey.json.data.status === 'failed' && step(badKey.json.data, 'completion').reason === 'auth_rejected',
    badKey.json.data.steps);

  const noModel = await post(cookie, '/api/settings/reply-ai/test', {
    base_url: gateway, api_key: apiKey, model: '', timeout: '5s',
  });
  check('missing model -> 400', noModel.status === 400, noModel.json);

  // --- Masked key reuses the stored secret ---------------------------------
  const current = await settings(cookie, 'GET');
  const full = stripMasked(JSON.parse(JSON.stringify(current.json.data)));
  full.reply_ai = {
    enabled: false,
    base_url: gateway,
    api_key: apiKey,
    model: 'mock-v1',
    timeout: '5s',
    min_confidence: 0.98,
  };
  const saved = await settings(cookie, 'PUT', full);
  check('store a mock key (200)', saved.status === 200, saved.json);
  await settle();

  const masked = await post(cookie, '/api/settings/reply-ai/models', {
    base_url: gateway, api_key: '••••••••••', timeout: '5s',
  });
  check('masked key reuses the stored key', masked.status === 200, masked.json);

  const blank = await post(cookie, '/api/settings/reply-ai/models', {
    base_url: gateway, api_key: '', timeout: '5s',
  });
  check('blank key reuses the stored key', blank.status === 200, blank.json);

  // --- Restore an inert configuration --------------------------------------
  const after = await settings(cookie, 'GET');
  const restore = stripMasked(JSON.parse(JSON.stringify(after.json.data)));
  restore.reply_ai = {
    enabled: false, base_url: '', api_key: '', model: '', timeout: '15s', min_confidence: 0.98,
  };
  const reset = await settings(cookie, 'PUT', restore);
  check('reset reply_ai to disabled (200)', reset.status === 200, reset.json);
  await settle();
})().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
