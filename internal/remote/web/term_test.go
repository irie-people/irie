package web

import "testing"

// wrapChecks runs in Node against js/term.js: how the Term view wraps a
// terminal's lines to read them on a phone upright (docs/PHONE.md §4).
const wrapChecks = `
import { drawLine } from './js/term.js';
const BLANK = Object.freeze({ fg: '', bg: '', b: false, d: false, i: false, u: false, s: false, inv: false, hid: false });
const text = (html) => html.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
const lines = (t, W, cx = -1) => text(drawLine(t, BLANK, cx, W).html).split('\n');
const cells = (l) => [...l].reduce((n, c) => n + (/[　-鿿]/.test(c) ? 2 : 1), 0);
const fail = [];
const check = (name, ok, got) => { if (!ok) fail.push(name + ': ' + JSON.stringify(got)); };

const words = '⏺ ' + 'the agent reads the parser tests and runs them twenty times '.repeat(3);
let got = lines(words, 40);
check('wraps to the width', got.length > 1 && got.every((l) => cells(l) <= 40), got);
check('goes on under its mark', got.slice(1).every((l) => l.startsWith('  ') && l[2] !== ' '), got);
check('breaks at spaces', got.every((l) => !l.endsWith(' ')) && got.join(' ').replace(/ +/g, ' ').trim() === words.trim().replace(/ +/g, ' '), got);

got = lines('  ⎿  Read ' + 'one more test file '.repeat(5), 30);
check('keeps ⎿ with its text', got.slice(1).every((l) => l.startsWith('     ') && l[5] !== ' '), got);

got = lines('─'.repeat(141), 40);
check('cuts a rule at the width', got.length === 1 && got[0] === '─'.repeat(40), got);

got = lines('│ Do you want to proceed?' + ' '.repeat(110) + '│', 40);
check('drops a box\'s right edge', got.length === 1 && got[0] === '│ Do you want to proceed?', got);

got = lines('│ Do you want to proceed?' + ' '.repeat(10) + '│', 40);
check('keeps a box that fits', got.length === 1 && got[0].endsWith('│'), got);

got = lines('│ ' + 'yes and do not ask again for npm commands '.repeat(3) + ' '.repeat(10) + '│', 40);
check('draws a box\'s side again', got.slice(1).every((l) => l.startsWith('│ ')), got);

got = lines('日本語の説明'.repeat(6), 11);
check('keeps wide cells whole', got.every((l) => cells(l) <= 11), got);

const prompt = drawLine('❯ ' + ' '.repeat(100), BLANK, 2, 40).html;
check('keeps the cursor', /tcur/.test(prompt) && !prompt.includes('\n'), prompt);

const red = drawLine('\x1b[31m' + 'all of it red '.repeat(8), BLANK, -1, 30).html;
check('carries the colour onto each line', red.split('\n').every((l) => l.startsWith('<span style="color:var(--t1);">')), red);

const whole = drawLine('ab  ' + '─'.repeat(141) + '  ', BLANK, -1, 0).html;
check('draws whole lines as they are', !whole.includes('\n') && whole.endsWith('  '), whole);

if (fail.length) { console.log(fail.join('\n')); process.exit(1); }
console.log('ok');
`

func TestTheTermViewWrapsLinesToReadThem(t *testing.T) {
	runInNode(t, wrapChecks)
}
