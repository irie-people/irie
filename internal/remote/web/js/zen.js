// Zen on the phone (docs/proposals/zen.md §10, step 4): the session's
// conversation alone, a soft line when another session would like the
// person, and what zen offers, one thing at a time, with its two answers.
// The core keeps the rules (internal/core/zen.go): the phone says it is in
// zen, as the client "phone", says so again every minute while it shows,
// and draws what its machine's header says.

/** The client the phone holds zen as. */
export const CLIENT = 'phone';

/** How often the phone says again that it is in zen (ipc.ZenRenew). */
export const RENEW_MS = 60 * 1000;

/** What zen offers now on a machine, from its header: null for nothing. */
export function offer(header) {
  const z = header && header.zen;
  return (z && z.offer) || null;
}

/** Whether a machine's core keeps zen for each client. */
export function served(header) {
  return !!(header && header.zen);
}

/** The sessions but the one shown that would like the person, as the Mac's soft line counts them. */
export function others(sessions, shown) {
  const loud = new Set(['needs_you', 'stalled', 'failed']);
  return [...sessions].filter((s) => s.id !== shown && loud.has(s.state));
}

/** The soft line's words for n sessions, or '' for none. */
export function othersWords(n) {
  if (n <= 0) return '';
  return n === 1 ? 'Another session would like you' : `${n} other sessions would like you`;
}

/** The op's arguments that say zen started (on) or ended on the phone, showing session sid. */
export function say(on, sid) {
  return on ? { on: true, client: CLIENT, session: sid } : { on: false, client: CLIENT };
}

/** The op's arguments that answer offer o: its first button (yes) or its second. */
export function answer(o, yes) {
  return { offer: o.id, answer: yes ? 'yes' : 'no' };
}
