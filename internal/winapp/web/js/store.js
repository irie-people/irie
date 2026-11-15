// What the core has said: its rows, header, history, the sessions outside
// Irie and the closed ones, from the frames of the app's subscription
// (DESIGN §2.4). The order is the core's one order; nothing here moves a
// card (DESIGN §4.1).

import * as L from '../shared/look.js';

export const S = {
  rows: new Map(),
  header: {},
  outside: [],
  outsideProblem: '',
  closed: [],
  undo: '',
  redo: '',
  loaded: false,
};

/** Takes one frame; says what it changed: 'rows', 'header', 'history', 'outside', 'closed' or ''. */
export function apply(f) {
  switch (f && f.t) {
    case 'snapshot':
      S.rows = new Map((f.sessions || []).map((s) => [s.id, s]));
      S.header = f.header || {};
      S.loaded = true;
      return 'rows';
    case 'upsert':
      if (f.session) S.rows.set(f.session.id, f.session);
      return 'rows';
    case 'remove':
      S.rows.delete(f.id);
      return 'rows';
    case 'header':
      S.header = f.header || {};
      return 'header';
    case 'history':
      S.undo = f.undo || '';
      S.redo = f.redo || '';
      return 'history';
    case 'outside':
      S.outside = f.outside || [];
      S.outsideProblem = f.problem || '';
      return 'outside';
    case 'closed':
      S.closed = f.closed || [];
      return 'closed';
  }
  return '';
}

/** The rows in the core's order. */
export function ordered() {
  return [...S.rows.values()].sort((a, b) => (a.order ?? 0) - (b.order ?? 0) || String(a.id).localeCompare(String(b.id)));
}

/** The rows of one space ("" is the first), in order. */
export function inSpace(space) {
  return ordered().filter((s) => (s.space || '') === (space || ''));
}

/** A row's words for the find field: name, branch, repo, profile, detail. */
export function matches(s, q) {
  if (!q) return true;
  const hay = [s.name, s.branch, s.repo, s.worktree, s.cwd, s.profile, s.model, s.detail, s.host].filter(Boolean).join(' ').toLowerCase();
  return q.toLowerCase().split(/\s+/).filter(Boolean).every((w) => hay.includes(w));
}

/** Red: the row needs you. */
export const needsYou = (s) => L.loud(L.look(s));

/**
 * The List view's sections, as the Mac app draws them: Pinned, then the
 * rest in order with your groups where their first session is, then
 * Parked, then a section for each paired machine. Each section is
 * {key, title, rows, fold, groups}; a group's rows are nested items.
 */
export function listSections(rows, groups) {
  const pinned = [], rest = [], parked = [], hosts = new Map();
  for (const s of rows) {
    if (s.parked) parked.push(s);
    else if (s.host) {
      if (!hosts.has(s.host)) hosts.set(s.host, []);
      hosts.get(s.host).push(s);
    } else if (s.pinned) pinned.push(s);
    else rest.push(s);
  }
  const out = [];
  if (pinned.length) out.push({ key: 'pinned', title: 'Pinned', items: tree(pinned, groups) });
  out.push({ key: 'main', title: '', items: tree(rest, groups) });
  out.push({ key: 'parked', title: 'Parked', items: parked.map(row), foldDefault: true });
  for (const [h, list] of hosts) out.push({ key: 'host:' + h, title: 'On ' + h, items: tree(list, groups) });
  return out;
}

/**
 * The Repo view's sections: each repo by its folder's name, in the order of
 * its first session, then Not in a repo, then Parked and the machines.
 */
export function repoSections(rows) {
  const repos = new Map(), none = [], parked = [], hosts = new Map();
  for (const s of rows) {
    if (s.parked) {
      parked.push(s);
      continue;
    }
    if (s.host) {
      if (!hosts.has(s.host)) hosts.set(s.host, []);
      hosts.get(s.host).push(s);
      continue;
    }
    const repo = s.repo || '';
    if (!repo) {
      none.push(s);
      continue;
    }
    if (!repos.has(repo)) repos.set(repo, []);
    repos.get(repo).push(s);
  }
  // Two repos of one name show their folder too.
  const names = new Map();
  for (const r of repos.keys()) {
    const n = L.base(r);
    names.set(n, (names.get(n) || 0) + 1);
  }
  const out = [];
  for (const [r, list] of repos) {
    const n = L.base(r);
    const title = names.get(n) > 1 ? `${n} · ${r.replace(/\/[^/]*$/, '')}` : n;
    out.push({ key: 'repo:' + r, title, items: list.map(row) });
  }
  if (none.length) out.push({ key: 'repo:', title: 'Not in a repo', items: none.map(row) });
  out.push({ key: 'parked', title: 'Parked', items: parked.map(row), foldDefault: true });
  for (const [h, list] of hosts) out.push({ key: 'host:' + h, title: 'On ' + h, items: list.map(row) });
  return out;
}

const row = (s) => ({ kind: 'row', s });

/**
 * Rows with your groups nested where their first row is (DESIGN §4.1): a
 * group of the header's Groups, inside its parent, holds its rows in
 * order.
 */
function tree(rows, groups) {
  const byID = new Map((groups || []).map((g) => [g.id, g]));
  if (!byID.size) return rows.map(row);
  const root = { kind: 'root', items: [] };
  const nodes = new Map();
  const node = (id) => {
    if (!id || !byID.has(id)) return root;
    if (nodes.has(id)) return nodes.get(id);
    const g = byID.get(id);
    const n = { kind: 'group', g, items: [] };
    nodes.set(id, n);
    // Placed in its parent the first time one of its rows comes.
    node(g.parent).items.push(n);
    return n;
  };
  for (const s of rows) node(s.group).items.push(row(s));
  return root.items;
}

/** How many of these rows need you, counting into groups. */
export function loudIn(items) {
  let n = 0;
  for (const it of items) {
    if (it.kind === 'row') n += needsYou(it.s) ? 1 : 0;
    else n += loudIn(it.items);
  }
  return n;
}

export function countIn(items) {
  let n = 0;
  for (const it of items) n += it.kind === 'row' ? 1 : countIn(it.items);
  return n;
}

/** The title bar's counts: need you, working, to verify, paused, frozen; parked rows are not counted. */
export function counts(rows) {
  const c = { needs: 0, working: 0, verify: 0, paused: 0, frozen: 0, first: {} };
  for (const s of rows) {
    if (s.parked) continue;
    const lk = L.look(s);
    let key = '';
    if (L.loud(lk)) key = 'needs';
    else if (lk === 'working' || (s.subs && (lk === 'done' || lk === 'idle'))) key = 'working';
    else if (lk === 'done') key = 'verify';
    else if (lk === 'paused' || lk === 'pausing') key = 'paused';
    else if (lk === 'frozen') key = 'frozen';
    if (!key) continue;
    c[key]++;
    if (!c.first[key]) c.first[key] = s.id;
  }
  return c;
}

/** The ids of the rows shown, in the order a keyboard walks them. */
export function walk(sections, folded) {
  const out = [];
  const visit = (items) => {
    for (const it of items) {
      if (it.kind === 'row') out.push(it.s.id);
      else if (!folded('group:' + it.g.id)) visit(it.items);
    }
  };
  for (const sec of sections) {
    if (sec.title && folded(sec.key, sec.foldDefault)) continue;
    visit(sec.items);
  }
  return out;
}
