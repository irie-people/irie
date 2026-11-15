// The slot's terminal, drawn by xterm.js: a tmux client of Irie's server in
// Linux, as the Mac app's terminal is (docs/MAC.md §2). The core keeps the
// slot zoomed while the app watches, so the client shows only the session
// on show, and `show` swaps sessions in place. Its ground and ink are the
// Mac's. It fits its frame at once, and tells tmux its size once a change
// settles, so a drag resizes the agents once, not at every step.

import { openTerm, host } from './net.js';

/** Irie's warm near-black and cream (Theme.swift, TerminalColors.standard), and a calm 16-colour set. */
const THEME = {
  background: '#14110D',
  foreground: '#EEE5D5',
  cursor: '#EEE5D5',
  cursorAccent: '#14110D',
  selectionBackground: '#3F638B',
  selectionInactiveBackground: '#33465C',
  black: '#1D1F21', red: '#CC6666', green: '#B5BD68', yellow: '#F0C674',
  blue: '#81A2BE', magenta: '#B294BB', cyan: '#8ABEB7', white: '#C5C8C6',
  brightBlack: '#666666', brightRed: '#D54E53', brightGreen: '#B9CA4A', brightYellow: '#E7C547',
  brightBlue: '#7AA6DA', brightMagenta: '#C397D8', brightCyan: '#70C0B1', brightWhite: '#EAEAEA',
};

const FONT = '"Cascadia Mono", "Cascadia Code", Consolas, "Courier New", monospace';
const SIZE_KEY = 'irie.termFont';
const DEFAULT_SIZE = 14;

function b64ToBytes(b64) {
  if (Uint8Array.fromBase64) return Uint8Array.fromBase64(b64);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function bytesToB64(s) {
  // A binary string, as xterm.js's onBinary gives: one byte per char.
  return btoa(s);
}

function utf8(b64) {
  try {
    return new TextDecoder().decode(b64ToBytes(b64));
  } catch {
    return '';
  }
}

export class SlotTerminal {
  /**
   * el is the frame the terminal fills. keys(e) is asked first about each
   * key: true when the app took it, so the terminal never sees it.
   * onNote(text) says a short thing at the terminal's foot, such as Copied.
   */
  constructor(el, { keys, onNote, onState } = {}) {
    this.el = el;
    this.appKeys = keys || (() => false);
    this.onNote = onNote || (() => {});
    this.onState = onState || (() => {});
    this.hold = false;
    this.state = 'detached';
    let size = DEFAULT_SIZE;
    try {
      size = Math.min(32, Math.max(9, Number(localStorage.getItem(SIZE_KEY)) || DEFAULT_SIZE));
    } catch {}
    const T = window.Terminal;
    this.term = new T({
      fontFamily: FONT,
      fontSize: size,
      lineHeight: 1.1,
      cursorBlink: true,
      allowProposedApi: true,
      scrollback: 2000,
      theme: THEME,
      drawBoldTextInBrightColors: false,
      rightClickSelectsWord: false,
      smoothScrollDuration: 0,
    });
    this.fit = new window.FitAddon.FitAddon();
    this.term.loadAddon(this.fit);
    try {
      const u = new window.Unicode11Addon.Unicode11Addon();
      this.term.loadAddon(u);
      this.term.unicode.activeVersion = '11';
    } catch {}
    this.term.open(el);
    this.useWebGL();
    this.term.attachCustomKeyEventHandler((e) => this.key(e));
    this.term.onData((d) => this.link && this.link.keys(d));
    this.term.onBinary((d) => this.link && this.link.bytes(bytesToB64(d)));
    // tmux copies with OSC 52 when a drag in it ends: the selection goes to
    // Windows' clipboard, as a copy from any terminal does.
    this.term.parser.registerOscHandler(52, (data) => {
      const i = data.indexOf(';');
      const payload = i >= 0 ? data.slice(i + 1) : '';
      if (payload && payload !== '?') {
        const text = utf8(payload);
        if (text.trim()) {
          host.copy(text);
          this.onNote(copiedWords(text));
        }
      }
      return true;
    });
    // Letting go of a drag of xterm.js's own selection (Shift while tmux
    // takes the mouse) copies it, as the Mac app does.
    el.addEventListener('mouseup', () => setTimeout(() => this.copySelection(true), 0));
    el.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      this.menu && this.menu(e, this);
    });
    el.addEventListener('wheel', (e) => {
      if (e.ctrlKey) {
        e.preventDefault();
        this.zoom(e.deltaY < 0 ? 1 : -1);
      }
    }, { passive: false });
    this.observer = new ResizeObserver(() => this.layout());
    this.observer.observe(el);
    this.layout();
  }

  useWebGL() {
    try {
      const gl = new window.WebglAddon.WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      this.term.loadAddon(gl);
    } catch {
      // The DOM renderer draws it instead.
    }
  }

  /** Attaches to the slot through the app. */
  attach() {
    if (this.link) return;
    const { cols, rows } = this.term;
    this.link = openTerm(cols, rows, (m) => {
      if (m.out) this.term.write(b64ToBytes(m.out));
      if (m.state) {
        this.state = m.state;
        this.onState(m.state, m.text || '');
        if (m.state === 'attached') this.sent = `${this.term.cols}x${this.term.rows}`;
      }
    });
    this.sent = `${cols}x${rows}`;
  }

  detach() {
    if (this.link) this.link.close();
    this.link = null;
  }

  /** Fits the frame now; tmux hears the size once it settles. */
  layout() {
    if (this.hold || !this.el.offsetWidth || !this.el.offsetHeight) return;
    let d;
    try {
      d = this.fit.proposeDimensions();
    } catch {
      return;
    }
    if (!d || !Number.isFinite(d.cols) || !Number.isFinite(d.rows)) return;
    const cols = Math.max(20, d.cols), rows = Math.max(5, d.rows);
    if (cols !== this.term.cols || rows !== this.term.rows) this.term.resize(cols, rows);
    clearTimeout(this.sizeTimer);
    this.sizeTimer = setTimeout(() => this.sendSize(), 180);
  }

  sendSize() {
    const key = `${this.term.cols}x${this.term.rows}`;
    if (this.link && key !== this.sent) {
      this.sent = key;
      this.link.size(this.term.cols, this.term.rows);
    }
  }

  /** While held, as during a drag of the sidebar's edge, the terminal keeps its size. */
  setHold(on) {
    this.hold = on;
    if (!on) this.layout();
  }

  focus() {
    this.term.focus();
  }

  hasFocus() {
    return !!this.term.textarea && document.activeElement === this.term.textarea;
  }

  /** Makes the text bigger (1), smaller (-1) or the default size (0). */
  zoom(step) {
    const now = this.term.options.fontSize;
    const next = step === 0 ? DEFAULT_SIZE : Math.min(32, Math.max(9, now + step));
    if (next === now) return;
    this.term.options.fontSize = next;
    try {
      localStorage.setItem(SIZE_KEY, String(next));
    } catch {}
    this.layout();
  }

  copySelection(quiet) {
    const text = this.term.getSelection();
    if (!text || !text.trim()) return false;
    host.copy(text);
    if (!quiet || text.includes('\n') || text.length > 0) this.onNote(copiedWords(text));
    return true;
  }

  async paste() {
    try {
      const text = await navigator.clipboard.readText();
      if (text) this.term.paste(text);
    } catch {
      this.onNote('Press Ctrl+V to paste.');
    }
  }

  selectAll() {
    this.term.selectAll();
  }

  /** The terminal's keys: the app's own first, then copy and paste, then the agent's. */
  key(e) {
    if (e.type !== 'keydown') return true;
    if (this.appKeys(e)) return false;
    const k = e.key.toLowerCase();
    const ctrl = e.ctrlKey && !e.altKey && !e.metaKey;
    // Ctrl+C copies while text is selected, and is the agent's otherwise.
    if (ctrl && !e.shiftKey && k === 'c' && this.term.hasSelection()) {
      this.copySelection(false);
      this.term.clearSelection();
      e.preventDefault();
      return false;
    }
    if (ctrl && e.shiftKey && k === 'c') {
      this.copySelection(false);
      e.preventDefault();
      return false;
    }
    // Ctrl+V and Ctrl+Shift+V paste: the browser's paste reaches xterm.js,
    // which pastes it bracketed, as tmux asks.
    if (ctrl && k === 'v') return false;
    return true;
  }

  dispose() {
    this.detach();
    this.observer.disconnect();
    this.term.dispose();
  }
}

function copiedWords(text) {
  const lines = text.replace(/\n+$/, '').split('\n').length;
  return lines > 1 ? `Copied ${lines} lines` : 'Copied';
}
