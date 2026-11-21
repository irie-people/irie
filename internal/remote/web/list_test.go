package web

import "testing"

// listChecks runs in Node against js/list.js: the phone's list holds the
// same sessions as the Mac's sidebar, in the same order (docs/PHONE.md §4).
const listChecks = `
import { arrange, screenSpace, shownSpace, pickSpace, spaceKey } from './js/list.js';
const fail = [];
const check = (name, ok, got) => { if (!ok) fail.push(name + ': ' + JSON.stringify(got)); };
const row = (id, o = {}) => ({ id, name: id, state: 'idle', ...o });
const groups = [{ id: 'g1', name: 'Work' }, { id: 'g2', name: 'Sidebar', parent: 'g1' }, { id: 'g3', name: 'Play' }];
const pic = (entries) => entries.map((e) => (e.s ? e.s.id : '#' + e.title) + '@' + e.depth).join(' ');
const rows = [row('a'), row('b', { group: 'g1', state: 'needs_you' }), row('c', { pinned: true }), row('d', { group: 'g2' }),
  row('e'), row('f', { group: 'g1' }), row('g', { group: 'g3' }), row('x', { group: 'gone' })];

// Store.swift's drawnGroups and Groups.swift's listGroups, the List view.
let got = arrange(rows, groups);
check('lays the list out as the Mac does', pic(got) === '#Pinned@0 c@1 a@0 #Work@0 b@1 #Sidebar@1 d@2 f@1 e@0 #Play@0 g@1 x@0', pic(got));
const work = got.find((e) => e.key === 'g:g1');
check('counts a group with the groups inside it', work.n === 3 && work.loud === 1 && work.open, work);
got = arrange(rows, groups, (k) => k === 'g:g1');
check('folds a group, inner groups and all', pic(got) === '#Pinned@0 c@1 a@0 #Work@0 e@0 #Play@0 g@1 x@0' && !got.find((e) => e.key === 'g:g1').open, pic(got));
check('draws no head without pins or groups', pic(arrange([row('a'), row('b')], [])) === 'a@0 b@0', pic(arrange([row('a'), row('b')], [])));

// The screen's space: that of the session it shows (Spaces.swift).
check('takes the shown session\'s space', screenSpace([row('a'), row('b', { shown: true, space: 's2' })]) === 's2');
check('takes the first space as ""', screenSpace([row('a', { shown: true })]) === '');
check('knows none while none shows', screenSpace([row('a'), row('p', { shown: true, host: 'studio' })]) === null);

const list = [{ id: 's1', name: 'One' }, { id: 's2', name: 'Two' }, { id: 's3', name: 'Three' }];
check('keys the first space ""', spaceKey(list, 0) === '' && spaceKey(list, 2) === 's3');
const st = {};
check('follows the screen', shownSpace(st, list, 's2') === 's2', st);
pickSpace(st, 's3', 's2');
check('keeps a pick while the screen stays', shownSpace(st, list, 's2') === 's3' && shownSpace(st, list, 's2') === 's3', st);
check('follows the screen when it goes elsewhere', shownSpace(st, list, '') === '' && st.pick === null, st);
pickSpace(st, 's2', null);
check('keeps a pick while nothing shows', shownSpace(st, list, null) === 's2', st);
pickSpace(st, '', null);
check('takes the screen\'s own space as no pick', st.pick === null && shownSpace(st, list, null) === '', st);
check('shows the first for a space gone', shownSpace({ pick: 'gone', pickAt: null }, list, null) === '');

if (fail.length) { console.log(fail.join('\n')); process.exit(1); }
console.log('ok');
`

func TestThePhoneListsTheSessionsTheMacDoes(t *testing.T) {
	runInNode(t, listChecks)
}
