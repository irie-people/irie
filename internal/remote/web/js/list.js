// The phone's list of a machine's sessions, as the Mac's sidebar draws it:
// one space at a time (mac/Sources/Irie/Spaces.swift), and in it Pinned
// first, then each of your groups where the first of its cards is, the
// groups inside it within it, the cards of none between them (Store.swift's
// drawnGroups and Groups.swift's listGroups, the List view); Parked after.
// The space shown is the one the machine's own screen shows, the space of
// the session shown there, unless the phone picked another; when the screen
// goes to another space, the phone follows it again.

import * as L from './look.js';

/** The key of space i of a header's list, as rows give their space: "" for the first. */
export const spaceKey = (list, i) => (i === 0 ? '' : list[i].id);

/** The space a machine's own screen shows: that of the session shown there; null while none shows. */
export function screenSpace(rows) {
  for (const s of rows) if (s.shown && !s.host) return s.space || '';
  return null;
}

/**
 * The space to show, of the header's list, given what the screen shows now
 * (null: no session shows there). st keeps {pick, pickAt, lastScreen}
 * between calls: the space picked on the phone, the screen's space when it
 * was picked, and the screen's last.
 */
export function shownSpace(st, list, screen) {
  if (screen != null) st.lastScreen = screen;
  // The screen went to another space: the phone follows it again.
  if (st.pick != null && screen !== st.pickAt) st.pick = null;
  const key = st.pick ?? st.lastScreen ?? '';
  return list.some((sp, i) => spaceKey(list, i) === key) ? key : '';
}

/** Picks a space on the phone: the screen's own is no pick, and following it goes on. */
export function pickSpace(st, key, screen) {
  st.pick = key === (screen ?? st.lastScreen ?? '') ? null : key;
  st.pickAt = screen;
}

/**
 * The rows of the space shown, not parked, as the Mac's List view draws
 * them: entries of {s, depth} for a card and {key, title, depth, n, loud,
 * open} for a head, Pinned's ("pinned") or one of your groups' ("g:" and
 * its id). groups are the header's; folded(key) says whether the person
 * folded a head.
 */
export function arrange(rows, groups, folded = () => false) {
  const out = [];
  const loudOf = (list) => list.filter((s) => L.loud(L.look(s))).length;
  const pinned = rows.filter((s) => s.pinned);
  if (pinned.length) {
    const open = !folded('pinned');
    out.push({ key: 'pinned', title: 'Pinned', depth: 0, n: pinned.length, loud: loudOf(pinned), open });
    // Under its head, as a group's cards, so the cards after it read as the list's own.
    if (open) for (const s of pinned) out.push({ s, depth: 1 });
  }
  const byID = new Map((groups || []).map((g) => [g.id, g]));
  // A group's path, the outermost first; a group the header doesn't know is none.
  const path = (id) => {
    const p = [];
    const seen = new Set();
    for (let g = byID.get(id); g && !seen.has(g.id); g = g.parent ? byID.get(g.parent) : null) {
      seen.add(g.id);
      p.unshift(g);
    }
    return p;
  };
  const top = [];
  const boxes = new Map();
  for (const s of rows) {
    if (s.pinned) continue;
    const p = path(s.group);
    if (!p.length) {
      top.push({ s });
      continue;
    }
    let parent = null;
    for (const g of p) {
      let b = boxes.get(g.id);
      if (!b) {
        b = { g, items: [], cards: [] };
        boxes.set(g.id, b);
        (parent ? parent.items : top).push({ box: b });
      }
      b.cards.push(s);
      parent = b;
    }
    parent.items.push({ s });
  }
  const walk = (items, depth) => {
    for (const it of items) {
      if (it.s) {
        out.push({ s: it.s, depth });
        continue;
      }
      const b = it.box;
      const key = 'g:' + b.g.id;
      const open = !folded(key);
      out.push({ key, title: b.g.name || 'Group', depth, n: b.cards.length, loud: loudOf(b.cards), open });
      if (open) walk(b.items, depth + 1);
    }
  };
  walk(top, 0);
  return out;
}
