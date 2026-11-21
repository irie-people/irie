// Draws the phone app, served by `go run ./tools/phonepreview`, into a PNG
// as an iPhone shows it from the Home Screen: its width, its pixels, touch,
// and the room its camera and home bar take. It runs a headless Chromium,
// so no window opens anywhere (AGENTS.md, "Windows stay out of sight"), and
// speaks the DevTools protocol over Node's own WebSocket, so it needs no
// package of its own; CHROME is the browser, such as Playwright's
// headless_shell.
//
//   node tools/phonepreview/snap.mjs URL OUT.png [WIDTH HEIGHT [light|dark [SCRIPT]]]
//
// 440 956 is an iPhone 16 Pro Max upright, 956 440 the same on its side.
// SCRIPT runs in the page once its sessions show, before the picture, in
// steps a second apart: "location.hash = '#/m/' + m + '/s/s3'", say, where m
// is the machine's id, then "document.querySelector('.seg-btn:nth-child(2)').click()"
// for the Term view, separated by a line of ---.

import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const [url, out, w = '440', h = '956', scheme = 'light', script = ''] = process.argv.slice(2);
if (!url || !out) {
  console.error('usage: node snap.mjs URL OUT.png [WIDTH HEIGHT [light|dark [SCRIPT]]]');
  process.exit(2);
}
const chrome = process.env.CHROME;
if (!chrome) {
  console.error('snap.mjs: set CHROME to a headless Chromium, such as Playwright\'s headless_shell');
  process.exit(2);
}
const width = Number(w);
const height = Number(h);
const side = width > height;
// The room the camera and the home bar take, upright and on its side.
const insets = side ? { top: 0, bottom: 21, left: 62, right: 62 } : { top: 62, bottom: 34, left: 0, right: 0 };
const profile = mkdtempSync(join(tmpdir(), 'phonesnap-'));
const browser = spawn(chrome, ['--headless', '--hide-scrollbars', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe'] });
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
const pause = (ms) => new Promise((r) => setTimeout(r, ms));

await send('Runtime.enable');
await send('Page.enable');
await send('Emulation.setUserAgentOverride', {
  userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.2 Mobile/15E148 Safari/604.1',
  platform: 'iPhone',
});
await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 3, mobile: true });
// Without it, as in an older Chromium, the bars sit under the camera.
const safe = await send('Emulation.setSafeAreaInsetsOverride', { insets: { ...insets, topMax: insets.top, bottomMax: insets.bottom } });
if (safe.error) console.error('snap.mjs: this Chromium draws no safe areas:', safe.error.message);
await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: scheme }] });
await send('Page.navigate', { url });
// Its sessions show once the stream is up and the snapshot drawn.
for (let i = 0; i < 100; i++) {
  if (await evaluate('!!document.querySelector(".card, .empty:not([hidden]), .welcome, .pair-form")')) break;
  await pause(100);
}
await pause(700);
for (const step of script ? script.split(/\n---\n/) : []) {
  const m = await evaluate('(JSON.parse(localStorage.getItem("irie.machines") || "[]")[0] || {}).id');
  const r = await evaluate(`(async (m) => { ${step} })(${JSON.stringify(m || '')}).then(() => 'ok', (e) => String(e))`);
  if (r !== 'ok') console.error('script:', r);
  await pause(1000);
}
const shot = await send('Page.captureScreenshot', { format: 'png' });
writeFileSync(out, Buffer.from(shot.result.data, 'base64'));
console.log(out);
stop(0);
