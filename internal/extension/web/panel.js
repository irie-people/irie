// Irie's side panel (docs/proposals/browser-extension.md): its tools,
// the first of them Terms in short. It talks only to Irie on this
// computer, over native messaging; Irie's own code does the reading.
//
// What a page says is a stranger's words: it goes into the panel as text,
// never as HTML, and only http and https addresses become links.
"use strict";

const ext = globalThis.browser ?? globalThis.chrome;
const B = globalThis.IRIE_BRAND || { name: "Irie", slug: "irie", host: "", site: "" };
const $ = (id) => document.getElementById(id);

const VERDICTS = ["against", "watch", "know", "for"];
const WORDS = { against: "Against you", watch: "To watch", know: "Good to know", for: "For you" };
const GLYPH = { against: "✕", watch: "!", know: "·", for: "✓" };

// ---- Building the page ---------------------------------------------------

// h makes an element. Strings become text, so a page's words stay words.
function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const kid of kids.flat(Infinity)) {
    if (kid == null || kid === false) continue;
    el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)));
  }
  return el;
}

// web is an address the panel may link to: http or https, else null.
function web(address) {
  try {
    const u = new URL(address);
    return u.protocol === "https:" || u.protocol === "http:" ? u.href : null;
  } catch {
    return null;
  }
}

function outLink(address, text) {
  const href = web(address);
  return href ? h("a", { href, target: "_blank", rel: "noopener noreferrer" }, text) : text;
}

function hostOf(address) {
  try {
    return new URL(address).hostname.replace(/^www\./, "");
  } catch {
    return address || "this page";
  }
}

function thousands(n) {
  return Number(n || 0).toLocaleString("en");
}

// ---- Irie, over native messaging -------------------------------------------

const irie = { port: null, next: 1, waiting: new Map(), hello: null };

function connect() {
  return new Promise((resolve) => {
    let port;
    try {
      port = ext.runtime.connectNative(B.host);
    } catch (e) {
      resolve({ missing: String((e && e.message) || e) });
      return;
    }
    irie.port = port;
    let said = false;
    port.onMessage.addListener((r) => {
      said = true;
      const w = irie.waiting.get(r.id);
      if (!w) return;
      if (!r.done) {
        if (w.onPart && r.part) w.onPart(r.part);
        return;
      }
      irie.waiting.delete(r.id);
      const ok = r.hello || r.doc || r.answer || r.model || !r.error;
      ok ? w.resolve(r) : w.reject(r);
    });
    port.onDisconnect.addListener(() => {
      const why = (ext.runtime.lastError && ext.runtime.lastError.message) || (port.error && port.error.message) || "";
      irie.port = null;
      for (const w of irie.waiting.values()) w.reject({ code: "gone-host", error: why || B.name + " closed the connection." });
      irie.waiting.clear();
      if (!said) resolve({ missing: why || "no answer" });
      else if (S.view !== "missing") {
        S.note = B.name + " went away for a moment; the next thing you ask reconnects.";
        render();
      }
    });
    call({ op: "hello", browser: browserName(), version: ext.runtime.getManifest().version_name || "" })
      .then((r) => resolve({ hello: r.hello }))
      .catch((r) => resolve({ missing: r.error || "no answer" }));
  });
}

// LIMIT is how long a request may take before the panel gives up on it:
// a model's answer longest, then a read that follows a page's links.
const LIMIT = { "terms.ask": 240000, "terms.read": 150000 };

function call(req, onPart) {
  return new Promise((resolve, reject) => {
    if (!irie.port) {
      reject({ code: "gone-host", error: B.name + " isn't connected." });
      return;
    }
    const id = irie.next++;
    const timer = setTimeout(() => {
      if (irie.waiting.delete(id)) reject({ code: "slow", error: B.name + " took too long to answer. Try again." });
    }, LIMIT[req.op] || 30000);
    const done = (f) => (v) => {
      clearTimeout(timer);
      f(v);
    };
    irie.waiting.set(id, { resolve: done(resolve), reject: done(reject), onPart });
    try {
      irie.port.postMessage({ ...req, id });
    } catch (e) {
      irie.waiting.delete(id);
      clearTimeout(timer);
      reject({ code: "gone-host", error: String((e && e.message) || e) });
    }
  });
}

// ask calls Irie, connecting again first when the connection went.
async function ask(req, onPart) {
  if (!irie.port) {
    const c = await connect();
    if (c.missing) throw { code: "gone-host", error: c.missing };
    irie.hello = c.hello;
  }
  return call(req, onPart);
}

function browserName() {
  const brands = (navigator.userAgentData && navigator.userAgentData.brands) || [];
  const known = ["Google Chrome", "Brave", "Microsoft Edge", "Opera", "Vivaldi", "Arc"];
  for (const k of known) if (brands.some((b) => b.brand === k)) return k.replace("Google ", "").replace("Microsoft ", "");
  if (globalThis.browser && ext.sidebarAction) return "Firefox";
  if (brands.some((b) => b.brand === "Chromium")) return "Chromium";
  return "Chrome";
}

// ---- State ---------------------------------------------------------------------

const S = {
  view: "connecting", // connecting, missing, off, idle, reading, doc, notfound, error
  why: "",
  note: "",
  update: false, // this extension isn't the one Irie has now
  reading: "",
  doc: null,
  suggest: [],
  others: [],
  error: "",
  retry: null,
  reads: 0, // reads begun, so a newer one wins
  terms: null, // the terms read before their privacy policy, to go back to
  thread: [],
  model: { name: "", on: false, why: "", where: "" },
  pressedAt: 0,
  tabId: null,
  otherTab: false,
  windowId: null,
};

// ---- Reading a page ---------------------------------------------------------------

// grab runs in the page the button was pressed on: its address, title and
// HTML as the browser shows it now, scripts' work included.
function grab() {
  const html = document.documentElement ? String(document.documentElement.outerHTML) : "";
  // A page can stand an element in for document.title, so only a string
  // goes. 8 million characters: as UTF-8 and JSON, well inside the 64 MiB
  // a browser sends a host at once.
  const title = typeof document.title === "string" ? document.title.slice(0, 500) : "";
  return { url: String(location.href).slice(0, 8000), title, html: html.length > 8000000 ? html.slice(0, 8000000) : html };
}

async function readPressed(pressed) {
  if (!pressed || pressed.at === S.pressedAt || Date.now() - pressed.at > 120000) return;
  if (S.windowId != null && pressed.windowId !== S.windowId) return;
  S.pressedAt = pressed.at;
  S.tabId = pressed.tabId;
  S.otherTab = false;
  let page = null;
  let why = "";
  try {
    const [r] = await ext.scripting.executeScript({ target: { tabId: pressed.tabId }, func: grab });
    page = r && r.result;
  } catch (e) {
    why = String((e && e.message) || e);
  }
  if (!page || !page.url) {
    S.view = "error";
    S.error = B.name + " can't read this page: browsers keep their own pages, their stores and PDFs closed to extensions. Open the site itself, or give the address of its terms below.";
    S.retry = null;
    if (why) console.debug("read:", why);
    render();
    return;
  }
  S.terms = null;
  await read({ url: page.url, title: page.title, html: page.html });
}

async function read(req) {
  // A newer read wins: what an older, slower one finds is dropped.
  const mine = ++S.reads;
  S.view = "reading";
  S.reading = hostOf(req.url);
  S.thread = [];
  S.error = "";
  render();
  try {
    const r = await ask({ op: "terms.read", url: req.url, title: req.title, html: req.html, want: req.want });
    if (mine !== S.reads) return;
    S.doc = r.doc;
    S.suggest = r.ask || [];
    S.view = "doc";
    S.retry = req;
  } catch (r) {
    if (mine !== S.reads) return;
    S.retry = req;
    if (r.code === "notfound") {
      S.view = "notfound";
      S.others = r.others || [];
    } else if (r.code === "off") {
      S.view = "off";
      S.why = r.error;
    } else {
      S.view = "error";
      S.error = r.error || String(r);
    }
  }
  render();
  $("main").scrollTop = 0;
}

async function question(text) {
  const q = (text || "").trim();
  if (!q || !S.doc) return;
  const item = { q, says: "", answer: null, writing: true, note: "" };
  S.thread.push(item);
  renderThread(true);
  const once = async () =>
    ask({ op: "terms.ask", doc: S.doc.id, question: q, model: S.model.on && !!S.model.name }, (part) => {
      item.says += part;
      renderThread(true);
    });
  try {
    let r;
    try {
      r = await once();
    } catch (e) {
      if (e.code !== "gone" && e.code !== "gone-host") throw e;
      // Irie restarted, or forgot these terms: read them again, then ask.
      const again = await ask({ op: "terms.read", url: S.retry.url, title: S.retry.title, html: S.retry.html, want: S.retry.want });
      S.doc = again.doc;
      r = await once();
    }
    item.answer = r.answer;
    if (r.code === "model") item.note = r.error;
  } catch (r) {
    item.note = r.error || String(r);
  }
  item.writing = false;
  renderThread(true);
}

// ---- Drawing ------------------------------------------------------------------------

function render() {
  const main = $("main");
  const state = $("state");
  state.textContent = "";
  $("chat").hidden = S.view !== "doc";
  let body;
  switch (S.view) {
    case "connecting":
      state.textContent = "Connecting…";
      body = h("div", { class: "card reading" }, h("div", { class: "spinner" }), h("div", {}, "Finding " + B.name + " on this computer…"));
      break;
    case "missing":
      state.textContent = "Not set up";
      body = missingView();
      break;
    case "off":
      state.textContent = "Off";
      body = offView();
      break;
    case "idle":
      body = idleView();
      break;
    case "reading":
      body = h("div", { class: "card reading" }, h("div", { class: "spinner" }), h("div", {}, "Reading ", h("b", {}, S.reading), "’s terms on this computer…"));
      break;
    case "notfound":
      body = notFoundView();
      break;
    case "error":
      body = errorView();
      break;
    case "doc":
      body = docView(S.doc);
      break;
  }
  const top = [];
  if (S.update) {
    top.push(
      h("div", { class: "card small" }, "This extension isn't the one " + B.name + " has now. ",
        h("button", { class: "link-button", onclick: update }, "Update it")),
    );
  }
  if (S.note) top.push(h("div", { class: "card small muted" }, S.note));
  if (S.otherTab && S.view === "doc") {
    top.push(h("div", { class: "card small muted" }, "You're on another tab now. Press " + B.name + "’s button there to read its terms."));
  }
  main.replaceChildren(h("div", { class: "stack" }, top, body));
  if (S.view === "doc") {
    renderSuggest();
    renderModel();
  }
}

function cmd(text) {
  const code = h("code", {}, text);
  const copy = h("button", { class: "button is-small", type: "button" }, "Copy");
  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(text);
      copy.textContent = "Copied";
    } catch {
      copy.textContent = "Select it";
    }
    setTimeout(() => (copy.textContent = "Copy"), 1600);
  });
  return h("div", { class: "cmd" }, code, copy);
}

function missingView() {
  return h("div", { class: "card" },
    h("p", { class: "lead" }, B.name + " isn't set up in this browser yet"),
    h("p", { class: "muted" }, "This panel reads terms with " + B.name + " on your computer, so nothing you read is sent anywhere."),
    h("ol", { class: "steps" },
      h("li", {}, "In a terminal, run:", cmd(B.slug + " setup extension")),
      h("li", {}, "Then press " + B.name + "’s button in the toolbar again.")),
    h("p", { class: "small muted" }, "No " + B.name + " yet? It's free: ", outLink(B.site, hostOf(B.site))));
}

function offView() {
  return h("div", { class: "card" },
    h("p", { class: "lead" }, "Terms in short is off"),
    h("p", { class: "muted" }, S.why || "Its plugin is off."),
    cmd(B.slug + " plugins on extension"));
}

function addressForm(label) {
  const input = h("input", { class: "field", type: "url", placeholder: "example.com or its terms’ address", "aria-label": label });
  const form = h("form", { class: "row" }, input, h("button", { class: "button is-prominent", type: "submit" }, "Read"));
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    let v = input.value.trim();
    if (!v) return;
    if (!/^https?:\/\//i.test(v)) v = "https://" + v;
    if (!web(v)) return;
    S.terms = null;
    read({ url: v });
  });
  return form;
}

function idleView() {
  return h("div", { class: "card" },
    h("p", { class: "lead" }, "What are you agreeing to?"),
    h("p", { class: "muted" }, "Press " + B.name + "’s button in the toolbar on any site, and it reads the site’s terms of service here: what they mean for you, in plain words, with the clause behind each point. Then ask it anything about them."),
    h("p", { class: "small muted" }, "Or give an address:"),
    addressForm("The address of a site or its terms"));
}

function notFoundView() {
  return h("div", { class: "card" },
    h("p", { class: "lead" }, "No terms of service found here"),
    h("p", { class: "muted" }, "Open the site’s Terms of Service, often linked at the bottom of its pages, and press " + B.name + "’s button there, or give their address."),
    S.others.length
      ? h("div", { class: "links" },
          h("div", { class: "small muted" }, "Links on this page that may be them:"),
          S.others.map((l) => h("button", { class: "button", type: "button", onclick: () => read({ url: l.url }) }, l.text || hostOf(l.url))))
      : null,
    h("div", { class: "doc-other" }, addressForm("The address of the terms")));
}

function errorView() {
  return h("div", { class: "card" },
    h("p", { class: "lead" }, "That didn't work"),
    h("p", { class: "muted" }, S.error),
    S.retry ? h("div", { class: "doc-other" }, h("button", { class: "button", type: "button", onclick: () => read(S.retry) }, "Try again")) : null,
    h("div", { class: "doc-other" }, addressForm("The address of the terms")));
}

function kindName(d) {
  if (d.kind === "privacy") return d.title || "Privacy policy";
  return d.title || "Terms of service";
}

function docView(d) {
  const facts = [thousands(d.words) + " words, about " + d.minutes + " minute" + (d.minutes === 1 ? "" : "s") + " to read"];
  if (d.reader) facts.push("written for " + d.reader);
  if (d.updated) facts.push("updated " + d.updated);
  if (d.company) facts.push("with " + d.company);
  const other = [];
  if (d.privacy && d.kind === "terms" && d.privacy !== d.url) {
    other.push(h("button", { class: "button is-small", type: "button", onclick: () => {
      S.terms = S.retry;
      read({ url: d.privacy, want: "privacy" });
    } }, "Read their privacy policy"));
  }
  if (S.terms && d.kind === "privacy") {
    const back = S.terms;
    other.push(h("button", { class: "button is-small", type: "button", onclick: () => {
      S.terms = null;
      read(back);
    } }, "Back to their terms"));
  }
  const warn = [];
  if (!d.english) warn.push(h("div", { class: "warn" }, "These read as another language than English, which " + B.name + "’s reading doesn't know yet, so it may miss what matters."));
  else if (d.kind === "other") warn.push(h("div", { class: "warn" }, "This page doesn't read as terms of service or a privacy policy, so what follows may not mean much."));
  const head = h("section", { class: "card" },
    h("div", { class: "doc-site" }, d.site || kindName(d)),
    h("div", { class: "doc-title" }, d.url ? outLink(d.url, kindName(d)) : kindName(d)),
    h("div", { class: "doc-facts" }, facts.join(" · ")),
    warn,
    h("div", { class: "tally" }, VERDICTS.filter((v) => (d.count || {})[v]).map((v) =>
      h("button", { class: "count v-" + v, type: "button", onclick: () => {
        const g = document.getElementById("g-" + v);
        if (g) g.scrollIntoView({ behavior: "smooth", block: "start" });
      } }, h("span", { class: "mark", "aria-hidden": "true" }, GLYPH[v]), h("b", {}, String(d.count[v])), " " + WORDS[v].toLowerCase()))),
    other.length ? h("div", { class: "doc-other" }, other) : null);

  const groups = VERDICTS.map((v) => {
    const ps = (d.points || []).filter((p) => p.verdict === v);
    if (!ps.length) return null;
    return h("section", { id: "g-" + v },
      h("h2", { class: "group-head v-" + v }, h("span", { class: "mark", "aria-hidden": "true" }, GLYPH[v]), WORDS[v], h("span", { class: "n" }, "· " + ps.length)),
      ps.map(pointView));
  });
  if (!(d.points || []).length) {
    groups.push(h("div", { class: "card muted" }, B.name + "’s reading found nothing it knows in these terms. You can still ask about them below."));
  }
  const missing = (d.missing || []).length
    ? h("section", { class: "card" },
        h("h2", { class: "group-head", style: "margin-top:0" }, "Not in these terms"),
        h("div", { class: "missing" }, d.missing.map((g) => h("div", {}, h("span", { class: "mark", "aria-hidden": "true" }, "?"), h("span", {}, g.says)))))
    : null;
  const fine = h("p", { class: "fine" }, "Not legal advice: a reading of the text by " + B.name + "’s own code, on this computer. Check what matters to you in the words themselves.");
  return [head, groups, missing, fine, h("div", { class: "thread", id: "thread" })];
}

function pointView(p) {
  const meta = [];
  if (p.section) meta.push(h("span", {}, "From “" + p.section + "”"));
  if (p.link && web(p.link)) meta.push(outLink(p.link, "Show in the terms ↗"));
  meta.push(h("button", { class: "link-button", type: "button", onclick: () => question("What does “" + p.title + "” mean for me?") }, "Ask about this"));
  return h("article", { class: "point v-" + p.verdict },
    h("span", { class: "mark", title: WORDS[p.verdict], "aria-label": WORDS[p.verdict] }, GLYPH[p.verdict]),
    h("div", { class: "point-title" }, p.title),
    h("div", { class: "point-says" }, p.says),
    h("details", { class: "point-more" },
      h("summary", {}, p.also ? "The words, and " + p.also + " more like them" : "The words"),
      h("blockquote", { class: "quote" }, p.quote),
      h("div", { class: "quote-meta" }, meta)));
}

function renderThread(scroll) {
  const t = $("thread");
  if (!t) return;
  t.replaceChildren(...S.thread.map((item) => {
    const a = item.answer;
    const says = a ? a.says : item.says;
    const kids = [h("div", { class: "msg-says" }, says || (item.writing ? "" : item.note || "No answer."))];
    if (a && a.quotes && a.quotes.length) {
      a.quotes.forEach((q, i) => {
        const meta = [];
        if (a.by !== "code") meta.push(h("span", {}, "[" + (i + 1) + "]"));
        if (q.section) meta.push(h("span", {}, "From “" + q.section + "”"));
        if (q.link && web(q.link)) meta.push(outLink(q.link, "Show in the terms ↗"));
        kids.push(h("blockquote", { class: "quote" }, q.text), h("div", { class: "quote-meta" }, meta));
      });
    }
    if (!item.writing && a) {
      kids.push(h("div", { class: "msg-by" }, a.by === "code"
        ? "Found by " + B.name + "’s own code, on this computer."
        : "Worded by " + a.by + ", a model on " + (S.model.where || "this computer") + ", from the passages quoted."));
    }
    if (!item.writing && item.note && a) kids.push(h("div", { class: "msg-by" }, item.note));
    return h("div", { class: "stack" },
      h("div", { class: "msg-q" }, item.q),
      h("div", { class: "msg-a" + (item.writing ? " is-writing" : "") }, kids));
  }));
  if (scroll) t.lastElementChild && t.lastElementChild.scrollIntoView({ block: "end", behavior: "smooth" });
  $("send").disabled = S.thread.some((i) => i.writing);
}

// update has Irie write its build of the extension into its folder, then
// reloads the extension, which takes it.
async function update() {
  try {
    await ask({ op: "update" });
    ext.runtime.reload();
  } catch (r) {
    S.note = r.error || String(r);
    render();
  }
}

function renderSuggest() {
  $("suggest").replaceChildren(...S.suggest.map((s) => h("button", { class: "chip", type: "button", onclick: () => question(s) }, s)));
  renderThread(false);
}

function renderModel() {
  const row = $("model-row");
  row.hidden = !S.model.name;
  $("model").checked = S.model.on;
  $("model-label").textContent = "Word answers with " + S.model.name + ", a model on " + (S.model.where || "this computer");
}

// ---- Wiring ----------------------------------------------------------------------------

function wire() {
  const box = $("question");
  const grow = () => {
    box.style.height = "auto";
    box.style.height = Math.min(box.scrollHeight, 120) + "px";
  };
  box.addEventListener("input", grow);
  box.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      $("ask").requestSubmit();
    }
  });
  $("ask").addEventListener("submit", (e) => {
    e.preventDefault();
    if ($("send").disabled) return;
    const q = box.value;
    box.value = "";
    grow();
    question(q);
  });
  $("model").addEventListener("change", (e) => {
    S.model.on = e.target.checked;
    ext.storage.local.set({ model: S.model.on }).catch(() => {});
  });
  ext.runtime.onMessage.addListener((m) => {
    if (m && m.type === "pressed") readPressed(m.pressed);
  });
  if (ext.tabs && ext.tabs.onActivated) {
    ext.tabs.onActivated.addListener((info) => {
      if (S.windowId != null && info.windowId !== S.windowId) return;
      const other = S.tabId != null && info.tabId !== S.tabId;
      if (other !== S.otherTab) {
        S.otherTab = other;
        render();
      }
    });
  }
}

async function start() {
  $("brand").textContent = B.name;
  document.title = B.name;
  wire();
  render();
  try {
    const w = await ext.windows.getCurrent();
    S.windowId = w.id;
  } catch {}
  try {
    const got = await ext.storage.local.get("model");
    S.model.on = !!got.model;
  } catch {}
  const c = await connect();
  if (c.missing) {
    S.view = "missing";
    render();
    return;
  }
  irie.hello = c.hello;
  S.update = !!c.hello.update;
  if (!c.hello.on) {
    S.view = "off";
    S.why = c.hello.why;
    render();
    return;
  }
  S.view = "idle";
  render();
  let pressed = null;
  try {
    pressed = (await ext.storage.session.get("pressed")).pressed;
  } catch {}
  readPressed(pressed);
  ask({ op: "model" })
    .then((r) => {
      S.model.name = r.model || "";
      S.model.where = r.where || "";
      if (S.view === "doc") renderModel();
    })
    .catch(() => {});
}

start();
