// The welcome: what this computer still lacks before Irie can run, as
// `irie setup` on Windows offers it (package wsl, docs/WINDOWS.md), a step
// a button. A fix that asks something, such as Linux's user name and
// password, opens a console window of its own; the page waits for it and
// looks again.

import { h, ICON } from '../shared/ui.js';
import * as L from '../shared/look.js';
import { host } from './net.js';

/** Puts kids in el in place of what it holds, leaving out null and false, as h() does. */
const fill = (el, ...kids) => el.replaceChildren(...kids.flat(Infinity).filter((k) => k != null && k !== false));

const GUIDE = 'https://github.com/irie-people/irie/blob/main/docs/WINDOWS.md';

function button(step, distro) {
  switch (step.id) {
    case 'wsl': return 'Install WSL';
    case 'distro': return `Install ${distro || 'Ubuntu'}`;
    case 'version': return 'Move it to WSL 2';
    case 'install': return `Install Irie in ${distro || 'Linux'}`;
    case 'keep': return 'Keep Linux running';
    case 'shortcut': return 'Add to the Start menu';
    case 'startup': return 'Start at sign-in';
  }
  return '';
}

/** What a fix does that the person sees, said before they press. */
function what(step) {
  switch (step.id) {
    case 'wsl': return 'Windows asks an administrator’s OK, then needs a restart.';
    case 'distro': return 'A window opens and asks you to choose a user name and password for Linux. Type exit there when it is done.';
    case 'install': return 'A window opens with Irie’s installer, which asks before each thing it installs: tmux, Claude Code and its sign-in.';
    case 'version': return 'It keeps its files, and takes a few minutes.';
  }
  return '';
}

export function renderSetup(el, setup, link) {
  const restartAfter = renderSetup.restart;
  const steps = (setup && setup.steps) || [];
  const blocks = steps.filter((s) => s.blocks);
  const extras = steps.filter((s) => !s.blocks && button(s));
  const fixing = setup && setup.fixing;
  const first = blocks[0];
  const kids = [
    h('div', { class: 'w-mark', html: L.sun('sun huge') }),
    h('h1', { class: 'w-title', text: 'Irie' }),
    h('p', { class: 'w-tag', text: 'Everything irie.' }),
  ];
  if (!setup) {
    kids.push(h('p', { class: 'w-lead' }, h('span', { class: 'spin-ring', html: ICON.ring }), ' Looking at this computer…'));
  } else if (setup.build && setup.build < 19041) {
    kids.push(h('p', { class: 'w-lead', text: first ? first.need : 'This Windows is too old for WSL 2.' }));
  } else {
    kids.push(h('p', { class: 'w-lead', text: 'Irie runs your agents inside WSL 2, Windows’ own Linux. A few things to set up first; each asks before it does anything.' }));
    const list = h('ol', { class: 'w-steps' });
    blocks.forEach((s, i) => {
      const busy = fixing === s.id;
      const can = button(s, setup.distro) && (!fixing || busy);
      list.append(h('li', { class: 'w-step' + (s === first ? ' now' : '') + (busy ? ' busy' : '') },
        h('span', { class: 'w-num', text: String(i + 1) }),
        h('div', { class: 'w-body' },
          h('div', { class: 'w-need', text: s.need }),
          s === first && what(s) ? h('div', { class: 'w-what', text: what(s) }) : null,
          busy ? h('div', { class: 'w-busy' }, h('span', { class: 'spin-ring', html: ICON.ring }), ' Working on it… a window may ask you something: answer it there.') : null),
        s === first && can ? h('button', {
          class: 'btn primary', disabled: !!fixing, text: button(s, setup.distro),
          onclick: () => {
            renderSetup.restart = !!s.restart;
            host.fix(s.id);
          },
        }) : null));
    });
    if (blocks.length) kids.push(list);
    if (restartAfter && blocks.some((s) => s.id === 'wsl' || s.id === 'distro')) {
      kids.push(h('p', { class: 'w-restart', text: 'Once WSL is installed, restart Windows, then open Irie again to go on.' }));
    }
    if (extras.length) {
      kids.push(h('h2', { class: 'w-sub', text: 'And if you like' }));
      kids.push(h('ul', { class: 'w-extras' }, ...extras.map((s) => h('li', { class: 'w-extra' },
        h('div', { class: 'w-need', text: s.ask || s.need }),
        h('button', { class: 'btn small', disabled: !!fixing, text: fixing === s.id ? 'Working…' : button(s), onclick: () => host.fix(s.id) })))));
    }
    if (setup.error) kids.push(h('p', { class: 'w-error', text: setup.error }));
  }
  kids.push(h('div', { class: 'w-foot' },
    h('button', { class: 'btn', text: 'Look again', onclick: () => host.retry() }),
    h('button', { class: 'linkish', text: 'How Irie runs on Windows', onclick: () => host.open(GUIDE) })));
  fill(el, h('div', { class: 'w-card' }, ...kids));
}
