// Dev-only mock of an OpenAI-compatible aggregator gateway used by the reply-AI
// verification scripts. Decisions are keyed on substrings of the untrusted reply
// body:
//   - contains "unsubscribe me"           -> unsubscribe 0.99
//   - contains "remove me from your mailing list" -> unsubscribe 0.99 (built-in probe sample)
//   - contains "report spam"              -> complaint   0.99
//   - contains "no idea"                  -> other        0.5
//   - contains "maybe unsubscribe"        -> unsubscribe 0.5 (below threshold)
// It also serves GET /models (and /v1/models) for the settings-page gateway
// probe, requiring a bearer token and rejecting the literal key "bad-key" so the
// auth-failure path can be verified. Any other path returns 404, so a wrong API
// root is reported as such.
const http = require('http');

const PORT = Number(process.argv[2]) || 9456;

// Catalogue mirrors an aggregator gateway: chat models mixed with non-chat ones.
const MODELS = [
  { id: 'mock-v1', object: 'model', owned_by: 'mock' },
  { id: 'gpt-4o-mini', object: 'model', owned_by: 'openai' },
  { id: 'deepseek-chat', object: 'model', owned_by: 'deepseek' },
  { id: 'text-embedding-3-small', object: 'model', owned_by: 'openai' },
  { id: 'whisper-1', object: 'model', owned_by: 'openai' },
];

function send(res, status, payload) {
  res.writeHead(status, { 'content-type': 'application/json' });
  res.end(JSON.stringify(payload));
}

// authorize validates the bearer token. Any non-empty token is accepted except
// "bad-key", which simulates a revoked gateway key.
function authorize(req, res) {
  const header = req.headers.authorization || '';
  const token = header.replace(/^Bearer\s+/i, '').trim();
  if (token === '') {
    send(res, 401, { error: { message: 'missing API key' } });
    return false;
  }
  if (token === 'bad-key') {
    send(res, 401, { error: { message: 'invalid token' } });
    return false;
  }
  return true;
}

http.createServer((req, res) => {
  const path = (req.url || '').split('?')[0];
  if (req.method === 'GET' && (path === '/models' || path === '/v1/models')) {
    if (!authorize(req, res)) return;
    send(res, 200, { object: 'list', data: MODELS });
    return;
  }
  if (path !== '/v1/chat/completions') {
    send(res, 404, { error: { message: `no such endpoint: ${path}` } });
    return;
  }

  let raw = '';
  req.on('data', (c) => { raw += c; });
  req.on('end', () => {
    if (!authorize(req, res)) return;
    let intent = 'other';
    let reason = 'none';
    let confidence = 0.5;
    try {
      const body = JSON.parse(raw);
      const text = JSON.stringify(body.messages || body).toLowerCase();
      if (text.includes('maybe unsubscribe')) {
        intent = 'unsubscribe';
        reason = 'explicit_unsubscribe';
        confidence = 0.5;
      } else if (text.includes('unsubscribe me') || text.includes('remove me from your mailing list')) {
        intent = 'unsubscribe';
        reason = 'explicit_unsubscribe';
        confidence = 0.99;
      } else if (text.includes('report spam')) {
        intent = 'complaint';
        reason = 'explicit_spam_or_abuse';
        confidence = 0.99;
      }
    } catch (err) {
      // Treat unparseable bodies as other.
    }
    send(res, 200, {
      choices: [{ message: { role: 'assistant', content: JSON.stringify({ intent, reason_code: reason, confidence }) } }],
    });
  });
}).listen(PORT, '0.0.0.0', () => {
  console.log(`mock openai listening on ${PORT}`);
});

