// Dev-only mock of an OpenAI-compatible chat completions endpoint used by the
// reply-AI worker end-to-end verification. Decisions are keyed on substrings
// of the untrusted reply body:
//   - contains "unsubscribe me"           -> unsubscribe 0.99
//   - contains "report spam"              -> complaint   0.99
//   - contains "no idea"                  -> other        0.5
//   - contains "maybe unsubscribe"        -> unsubscribe 0.5 (below threshold)
const http = require('http');

const PORT = Number(process.argv[2]) || 9456;

http.createServer((req, res) => {
  let raw = '';
  req.on('data', (c) => { raw += c; });
  req.on('end', () => {
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
      } else if (text.includes('unsubscribe me')) {
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
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({
      choices: [{ message: { role: 'assistant', content: JSON.stringify({ intent, reason_code: reason, confidence }) } }],
    }));
  });
}).listen(PORT, '0.0.0.0', () => {
  console.log(`mock openai listening on ${PORT}`);
});
