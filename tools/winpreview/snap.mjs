// Draws the Windows app's page, served by `go run ./tools/winpreview`, into
// PNGs with a headless Chromium: the engine WebView2 draws it with on
// Windows, and no window anywhere (AGENTS.md, "Windows stay out of
// sight"). It speaks the DevTools protocol over Node's own WebSocket, so it
// needs no package of its own; CHROME is the browser, such as Playwright's
// headless_shell.
//
//   node tools/winpreview/snap.mjs URL OUT.png [WIDTH HEIGHT [light|dark [SCRIPT]]]
//
// SCRIPT runs in the page once its sessions show, before the picture: a
// way to open a menu or a dialog first.

import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const [url, out, w = '1280', h = '800', scheme = 'light', script = ''] = process.argv.slice(2);
if (!url || !out) {
  console.error('usage: node snap.mjs URL OUT.png [WIDTH HEIGHT [light|dark [SCRIPT]]]');
  process.exit(2);
}
const chrome = process.env.CHROME;
if (!chrome) {
  console.error('snap.mjs: set CHROME to a headless Chromium, such as Playwright\'s headless_shell');
  process.exit(2);
}
const profile = mkdtempSync(join(tmpdir(), 'winsnap-'));
const browser = spawn(chrome, ['--headless', '--hide-scrollbars', '--remote-debugging-port=0', `--user-data-dir=${profile}`, `--window-size=${w},${h}`, 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe'] });
const stop = (code) => {
  browser.kill();
  try { rmSync(profile, { recursive: true, force: true }); } catch {}
  process.exit(code);
};
setTimeout(() => { console.error('snap.mjs: timed out'); stop(1); }, 60000);

const endpoint = await new Promise((resolve) => {
  let buf = '';
  browser.stderr.on('data', (d) => {
    buf += d;
    const m = buf.match(/DevTools listening on (ws:\/\/\S+)/);
    if (m) resolve(m[1]);
  });
});
const port = new URL(endpoint).port;
const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const page = pages.find((p) => p.type === 'page');
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener('open', r, { once: true }));
let id = 0;
const waiting = new Map();
ws.addEventListener('message', (e) => {
  const m = JSON.parse(e.data);
  if (m.id && waiting.has(m.id)) {
    waiting.get(m.id)(m);
    waiting.delete(m.id);
  }
  if (m.method === 'Runtime.exceptionThrown') console.error('page error:', JSON.stringify(m.params.exceptionDetails).slice(0, 800));
  if (m.method === 'Runtime.consoleAPICalled' && (m.params.type === 'error' || m.params.type === 'warning')) {
    console.error('page console:', m.params.args.map((a) => a.value ?? a.description).join(' '));
  }
});
const send = (method, params = {}) => new Promise((resolve) => {
  const i = ++id;
  waiting.set(i, resolve);
  ws.send(JSON.stringify({ id: i, method, params }));
});
const evaluate = async (expr) => (await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true })).result?.result?.value;

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: Number(w), height: Number(h), deviceScaleFactor: 1, mobile: false });
await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: scheme }] });
await send('Page.navigate', { url });
// Its sessions show once the link is up and the snapshot drawn.
for (let i = 0; i < 100; i++) {
  const ok = await evaluate('!!(window.__irie && (window.__irie.S.loaded || document.querySelector(".welcome:not([hidden])") || document.querySelector(".overlay:not([hidden]) .cover-card")))');
  if (ok) break;
  await new Promise((r) => setTimeout(r, 100));
}
await new Promise((r) => setTimeout(r, 700));
if (script) {
  const r = await evaluate(`(async () => { ${script} })().then(() => 'ok', (e) => String(e))`);
  if (r !== 'ok') console.error('script:', r);
  await new Promise((r) => setTimeout(r, 400));
}
const shot = await send('Page.captureScreenshot', { format: 'png' });
writeFileSync(out, Buffer.from(shot.result.data, 'base64'));
console.log(out);
stop(0);
