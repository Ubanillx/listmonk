import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import readline from 'node:readline';
import { createChrome } from 'file:///C:/Users/topmax/.claude/skills/oil-ui-pro/scripts/chrome.mjs';

const chrome = createChrome();
const socket = new WebSocket(await chrome.launch());
await new Promise((ok, no) => { socket.onopen = ok; socket.onerror = no; });
let serial = 0;
const pending = new Map();
socket.onmessage = ({ data }) => {
  const message = JSON.parse(data);
  if (!message.id) return;
  const p = pending.get(message.id);
  if (!p) return;
  pending.delete(message.id);
  message.error ? p.reject(Error(message.error.message)) : p.resolve(message.result);
};
function send(method, params = {}, sessionId) {
  return new Promise((resolve, reject) => {
    const id = ++serial;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params, sessionId }));
  });
}
const target = await send('Target.createTarget', { url: 'about:blank' });
const { sessionId } = await send('Target.attachToTarget', { targetId: target.targetId, flatten: true });
const page = (method, params) => send(method, params, sessionId);
await page('Page.enable');
await page('Runtime.enable');
await page('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });

async function evaluate(expression) {
  const result = await page('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
  if (result.exceptionDetails) throw Error(result.exceptionDetails.text);
  return result.result.value;
}
const visible = `e => { const r=e.getBoundingClientRect(), s=getComputedStyle(e); return r.width>0 && r.height>0 && s.visibility!=='hidden' && s.display!=='none' }`;
const q = selector => `Array.from(document.querySelectorAll(${JSON.stringify(selector)})).filter(${visible})`;
const byText = text => `Array.from(document.querySelectorAll('button,a,[role="button"]')).filter(${visible}).find(e => (e.innerText||e.textContent||'').trim() === ${JSON.stringify(text)})`;
async function click(expression) {
  const rect = await evaluate(`(() => { const e=${expression}; if (!e) return null; e.scrollIntoView({block:'center'}); const r=e.getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; })()`);
  if (!rect) throw Error('Element not found');
  await page('Input.dispatchMouseEvent', { type: 'mousePressed', x: rect.x, y: rect.y, button: 'left', clickCount: 1 });
  await page('Input.dispatchMouseEvent', { type: 'mouseReleased', x: rect.x, y: rect.y, button: 'left', clickCount: 1 });
}
async function fill(selector, value) {
  const found = await evaluate(`(() => { const e=${q(selector)}[0]; if (!e) return false; e.focus(); const setter=Object.getOwnPropertyDescriptor(e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set; setter.call(e,${JSON.stringify(value)}); e.dispatchEvent(new Event('input',{bubbles:true})); e.dispatchEvent(new Event('change',{bubbles:true})); return true })()`);
  if (!found) throw Error('Field not found');
}
const config = readFileSync(resolve('frontend/cypress.config.js'), 'utf8');
function credential(key) {
  const match = config.match(new RegExp(`${key}:\\s*['\"]([^'\"]+)['\"]`));
  if (!match) throw Error(`Missing ${key}`);
  return match[1];
}
const rl = readline.createInterface({ input: process.stdin, terminal: false });
for await (const line of rl) {
  try {
    const { action, selector, text, path, width, height, index = 0 } = JSON.parse(line);
    let value;
    if (action === 'goto') { await page('Page.navigate', { url: 'http://127.0.0.1:9273/admin/user/profile' }); value = 'navigated'; }
    if (action === 'state') value = await evaluate(`({url:location.href, title:document.title, text:document.body.innerText.slice(0,12000), controls:Array.from(document.querySelectorAll('input,select,textarea,button,a,[role="button"]')).filter(${visible}).map(e=>({tag:e.tagName,text:(e.innerText||e.textContent||'').trim().slice(0,100),type:e.type||'',name:e.name||'',id:e.id||'',placeholder:e.placeholder||'',href:e.getAttribute('href')||''})).slice(0,150)})`);
    if (action === 'click') { await click(selector ? `${q(selector)}[${index}]` : byText(text)); value = 'clicked'; }
    if (action === 'fill') { await fill(selector, text); value = 'filled'; }
    if (action === 'login') {
      await fill('input[name="username"]', credential('LISTMONK_ADMIN_USER'));
      await fill('input[name="password"]', credential('LISTMONK_ADMIN_PASSWORD'));
      await click(`${q('button[type="submit"]')}[0]`);
      value = 'login submitted';
    }
    if (action === 'size') { await page('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 600 }); value = 'resized'; }
    if (action === 'shot') { const data = (await page('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })).data; mkdirSync(dirname(resolve(path)), { recursive: true }); writeFileSync(resolve(path), Buffer.from(data, 'base64')); value = path; }
    if (action === 'wait') { await new Promise(r => setTimeout(r, Number(text) || 500)); value = 'waited'; }
    console.log(JSON.stringify({ ok: true, value }));
  } catch (error) { console.log(JSON.stringify({ ok: false, error: error.message })); }
}
await chrome.stop(socket);
