package web

import "testing"

// zenChecks runs in Node against js/zen.js: the phone says it is in zen as
// the client "phone", answers what zen offers, and counts the sessions that
// would like the person as the Mac's soft line does (docs/proposals/zen.md).
const zenChecks = `
import { CLIENT, RENEW_MS, offer, served, others, othersWords, say, answer } from './js/zen.js';
const fail = [];
const check = (name, ok, got) => { if (!ok) fail.push(name + ': ' + JSON.stringify(got)); };
check('holds zen as the phone', CLIENT === 'phone' && JSON.stringify(say(true, 's1')) === '{"on":true,"client":"phone","session":"s1"}', say(true, 's1'));
check('ends only its own zen', JSON.stringify(say(false)) === '{"on":false,"client":"phone"}', say(false));
check('says it again every minute, inside the lapse', RENEW_MS === 60000, RENEW_MS);
const o = { id: 'eyes', kind: 'learn', text: 'Look far away.', yes: 'Done', no: 'Not Now' };
check('reads the offer', offer({ zen: { offer: o } }) === o && offer({ zen: {} }) === null && offer({}) === null && offer(null) === null);
check('answers yes and no', JSON.stringify(answer(o, true)) === '{"offer":"eyes","answer":"yes"}' && answer(o, false).answer === 'no');
check('knows a core with zen', served({ zen: { profile: {} } }) && !served({}) && !served(null));
const rows = [{ id: 'a', state: 'working' }, { id: 'b', state: 'needs_you' }, { id: 'c', state: 'failed' }, { id: 'd', state: 'done' }, { id: 'e', state: 'stalled' }];
check('counts the others that would like you', others(rows, 'b').map((s) => s.id).join() === 'c,e', others(rows, 'b'));
check('words the soft line', othersWords(0) === '' && othersWords(1) === 'Another session would like you' && othersWords(3) === '3 other sessions would like you');
if (fail.length) { console.log(fail.join('\n')); process.exit(1); }
console.log('ok');
`

func TestThePhoneSaysAndDrawsZenAsTheCoreKeepsIt(t *testing.T) {
	runInNode(t, zenChecks)
}
