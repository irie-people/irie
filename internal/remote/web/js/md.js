// A small, safe Markdown renderer for the agent's replies. Everything is
// escaped first; only the tags written here come out: paragraphs, headings,
// bold, italic, strikethrough, code, fenced code blocks, lists, blockquotes,
// rules, http(s) links and simple tables. Nothing the agent writes becomes
// an attribute but a link's http(s) address, which is escaped too.

const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

/** Text made safe to put in HTML, in an element or an attribute. */
export const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ESC[c]);

/** Markdown to HTML. */
export function md(source) {
  const text = String(source ?? '').replace(/\r\n?/g, '\n').replace(/\u0000/g, '');
  return blocks(text.split('\n'));
}

const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([^`\s]*)/;
const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const RULE = /^ {0,3}([-*_])(?:\s*\1){2,}\s*$/;
const QUOTE = /^ {0,3}>/;
const ITEM = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const TABLE_RULE = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/;

function blocks(lines) {
  let out = '';
  let para = [];
  const flush = () => {
    if (para.length) out += '<p>' + para.map(inline).join('<br>') + '</p>';
    para = [];
  };
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    let m = line.match(FENCE);
    if (m) {
      flush();
      const fence = m[1], lang = m[2];
      const body = [];
      i++;
      while (i < lines.length && !isClosing(lines[i], fence)) body.push(lines[i++]);
      i++;
      out += `<div class="code"${lang ? ` data-lang="${esc(lang)}"` : ''}><pre><code>${esc(body.join('\n'))}</code></pre></div>`;
      continue;
    }
    if (!line.trim()) {
      flush();
      i++;
      continue;
    }
    m = line.match(HEADING);
    if (m) {
      flush();
      // A phone has little room: # and ## are the largest heading, then smaller.
      const tag = m[1].length <= 2 ? 'h3' : m[1].length === 3 ? 'h4' : 'h5';
      out += `<${tag}>${inline(m[2])}</${tag}>`;
      i++;
      continue;
    }
    if (RULE.test(line)) {
      flush();
      out += '<hr>';
      i++;
      continue;
    }
    if (QUOTE.test(line)) {
      flush();
      const q = [];
      while (i < lines.length && QUOTE.test(lines[i])) q.push(lines[i++].replace(/^ {0,3}> ?/, ''));
      out += '<blockquote>' + blocks(q) + '</blockquote>';
      continue;
    }
    if (line.includes('|') && i + 1 < lines.length && lines[i + 1].includes('-') && TABLE_RULE.test(lines[i + 1])) {
      flush();
      const head = cells(line), aligns = cells(lines[i + 1]).map(align);
      i += 2;
      const rows = [];
      while (i < lines.length && lines[i].includes('|') && lines[i].trim()) rows.push(cells(lines[i++]));
      out += table(head, aligns, rows);
      continue;
    }
    if (ITEM.test(line)) {
      flush();
      const [html, next] = list(lines, i);
      out += html;
      i = next;
      continue;
    }
    para.push(line.trim());
    i++;
  }
  flush();
  return out;
}

function isClosing(line, fence) {
  const t = line.trim();
  return t.length >= fence.length && t === fence[0].repeat(t.length);
}

function cells(line) {
  let t = line.trim();
  if (t.startsWith('|')) t = t.slice(1);
  if (t.endsWith('|') && !t.endsWith('\\|')) t = t.slice(0, -1);
  // Split on every | that no backslash escapes, by hand: Safari before
  // 16.4 can't parse a lookbehind, and would fail the whole module.
  const out = [];
  let cell = '';
  for (let i = 0; i < t.length; i++) {
    if (t[i] === '\\' && t[i + 1] === '|') {
      cell += '|';
      i++;
    } else if (t[i] === '|') {
      out.push(cell.trim());
      cell = '';
    } else {
      cell += t[i];
    }
  }
  out.push(cell.trim());
  return out;
}

function align(rule) {
  const l = rule.startsWith(':'), r = rule.endsWith(':');
  return l && r ? 'c' : r ? 'r' : '';
}

function table(head, aligns, rows) {
  const cell = (tag, text, k) => `<${tag}${aligns[k] ? ` class="al-${aligns[k]}"` : ''}>${inline(text)}</${tag}>`;
  let html = '<div class="tbl"><table><thead><tr>' + head.map((c, k) => cell('th', c, k)).join('') + '</tr></thead><tbody>';
  for (const r of rows) {
    html += '<tr>' + head.map((_, k) => cell('td', r[k] ?? '', k)).join('') + '</tr>';
  }
  return html + '</tbody></table></div>';
}

function list(lines, i) {
  const items = [];
  while (i < lines.length) {
    const line = lines[i];
    const m = line.match(ITEM);
    if (m) {
      items.push({ indent: m[1].replace(/\t/g, '    ').length, ordered: /\d/.test(m[2]), start: parseInt(m[2], 10), text: [m[3]] });
      i++;
      continue;
    }
    if (!line.trim()) {
      // A blank line goes on with the list when an item or an indented line follows.
      const next = lines[i + 1];
      if (next !== undefined && (ITEM.test(next) || /^\s{2,}\S/.test(next))) {
        i++;
        continue;
      }
      break;
    }
    if (/^\s{2,}\S/.test(line) && items.length) {
      items[items.length - 1].text.push(line.trim());
      i++;
      continue;
    }
    break;
  }
  return [nest(items), i];
}

function nest(items) {
  let html = '';
  const stack = [];
  for (const it of items) {
    const tag = it.ordered ? 'ol' : 'ul';
    while (stack.length && it.indent < stack[stack.length - 1].indent) html += `</li></${stack.pop().tag}>`;
    const top = stack[stack.length - 1];
    if (!top || it.indent > top.indent) {
      html += `<${tag}${it.ordered && it.start !== 1 ? ` start="${it.start}"` : ''}>`;
      stack.push({ indent: it.indent, tag });
    } else if (top.tag !== tag) {
      html += `</li></${stack.pop().tag}><${tag}${it.ordered && it.start !== 1 ? ` start="${it.start}"` : ''}>`;
      stack.push({ indent: it.indent, tag });
    } else {
      html += '</li>';
    }
    let first = it.text[0];
    let box = '';
    const task = first.match(/^\[( |x|X)\]\s+(.*)$/);
    if (task) {
      box = `<span class="task${task[1] === ' ' ? '' : ' checked'}" aria-label="${task[1] === ' ' ? 'not done' : 'done'}"></span>`;
      first = task[2];
    }
    html += `<li${box ? ' class="has-task"' : ''}>` + box + [first, ...it.text.slice(1)].map(inline).join('<br>');
  }
  while (stack.length) html += `</li></${stack.pop().tag}>`;
  return html;
}

const LINK = (url, text) => `<a href="${url}" target="_blank" rel="noopener noreferrer">${text}</a>`;

/** ASCII punctuation: what a backslash escapes. */
const PUNCT = /[!-/:-@[-`{-~]/;

/** One line's inline Markdown, escaped. */
export function inline(source) {
  const s = String(source ?? '').replace(/\u0000/g, '');
  const keep = [];
  const stash = (html) => `\u0000${keep.push(html) - 1}\u0000`;
  // A kept piece may hold others, as a link's text holds code: expand until none is left.
  const restore = (x) => {
    for (let k = 0; k < 8 && x.includes('\u0000'); k++) x = x.replace(/\u0000(\d+)\u0000/g, (_, n) => keep[+n] ?? '');
    return x;
  };
  // Backslash escapes and code spans, read left to right as CommonMark reads
  // them: an escaped backtick opens no code span, and nothing inside a code
  // span is Markdown, its backslashes included. Everything else is escaped.
  let t = '';
  let plain = '';
  const flush = () => {
    t += esc(plain);
    plain = '';
  };
  for (let i = 0; i < s.length;) {
    const c = s[i];
    if (c === '\\' && i + 1 < s.length && PUNCT.test(s[i + 1])) {
      flush();
      t += stash(esc(s[i + 1]));
      i += 2;
      continue;
    }
    if (c === '`') {
      let n = 1;
      while (s[i + n] === '`') n++;
      const close = closing(s, i + n, n);
      if (close < 0) {
        plain += '`'.repeat(n);
        i += n;
        continue;
      }
      let code = s.slice(i + n, close).replace(/\n/g, ' ');
      if (code.length > 2 && code[0] === ' ' && code[code.length - 1] === ' ' && code.trim()) code = code.slice(1, -1);
      flush();
      t += stash(`<code>${esc(code)}</code>`);
      i = close + n;
      continue;
    }
    plain += c;
    i++;
  }
  flush();
  // [text](https://…), whose text may hold code and escapes, and <https://…>.
  // No address takes a kept piece, so none is ever anything but escaped text.
  t = t.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)\u0000]+)\)/g, (_, text, url) => stash(LINK(url, restore(emphasis(text)))));
  t = t.replace(/&lt;(https?:\/\/[^\s&\u0000]+)&gt;/g, (_, url) => stash(LINK(url, url)));
  // Bare addresses, without the punctuation that ends a sentence.
  t = t.replace(/(^|[\s(]|&quot;|&#39;)(https?:\/\/[^\s\u0000]+)/g, (_, pre, url) => {
    let tail = '';
    const quote = url.search(/&(?:quot|#39|gt|lt);/);
    if (quote >= 0) {
      tail = url.slice(quote);
      url = url.slice(0, quote);
    }
    const end = url.match(/[.,:;!?)\]]+$/);
    if (end) {
      tail = end[0] + tail;
      url = url.slice(0, -end[0].length);
    }
    return pre + (/^https?:\/\/[^/]/.test(url) ? stash(LINK(url, url)) : url) + tail;
  });
  return restore(emphasis(t));
}

/** Where a run of exactly n backticks closes a code span, from from; -1 when none does. */
function closing(s, from, n) {
  for (let j = s.indexOf('`', from); j >= 0; j = s.indexOf('`', j)) {
    let m = 1;
    while (s[j + m] === '`') m++;
    if (m === n) return j;
    j += m;
  }
  return -1;
}

function emphasis(t) {
  return t
    .replace(/\*\*(?=\S)([\s\S]*?\S)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[^\w])__(?=\S)([\s\S]*?\S)__(?!\w)/g, '$1<strong>$2</strong>')
    .replace(/(^|[^\w*])\*(?=[^\s*])([^*]*?[^\s*])\*(?![\w*])/g, '$1<em>$2</em>')
    .replace(/(^|[^\w])_(?=[^\s_])([^_]*?[^\s_])_(?!\w)/g, '$1<em>$2</em>')
    .replace(/~~(?=\S)([\s\S]*?\S)~~/g, '<del>$1</del>');
}
