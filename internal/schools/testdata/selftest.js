// Runs after the page's own script, in the page's test (page_test.go): it
// drives the page from inside, as a person would, and writes what it found
// into a pre element that the test reads back from the page's DOM.
(async () => {
  const out = { checks: [], errors: window.__errors };
  const check = (name, ok, got) => out.checks.push({ name, ok: !!ok, got: got === undefined ? '' : JSON.stringify(got) });
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const key = k => document.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }));
  const rows = () => [...document.querySelectorAll('#card .parts tbody .row')];
  try {
    const cs = window.commonSchools;
    const card = document.getElementById('card');
    const trail = document.getElementById('trail');
    const reader = document.getElementById('reader');
    check('the page starts on the first pie', cs && cs.route().id === 'began', cs && cs.route());
    const pie = cs.pie();
    check('the first pie has the six groups of 1922', pie && pie.slices.length === 6, pie && pie.slices.length);
    const whole = pie.slices.reduce((a, s) => a + s.share, 0);
    check('its slices make a whole', Math.abs(whole - 1) < 1e-9, whole);
    check('it is drawn', document.querySelectorAll('#pie path.slice').length === 6, document.querySelectorAll('#pie path.slice').length);
    check('the table has a row for each slice', rows().length === 6, rows().length);

    rows().find(r => r.textContent.startsWith('Section 16')).click();
    await wait(60);
    check('a row opens its slice as a pie', cs.route().id === 'began/section-16' && cs.pie().id === 'began/section-16', cs.route());
    check('the trail says where', /The 48 states of 1922/.test(trail.textContent) && /Section 16/.test(trail.textContent), trail.textContent);

    document.querySelector('#pie path.slice').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await wait(60);
    check('a slice opens too', cs.route().id === 'began/section-16/east-north-central', cs.route());

    cs.go('began/section-16/east-north-central/ohio');
    await wait(60);
    check('a state opens into its lands by kind', cs.pie() && cs.pie().id.endsWith('/ohio') && cs.pie().slices.length === 4, cs.pie());
    check('its card gives the act behind it', /Ohio Enabling Act/.test(card.textContent), card.textContent.slice(0, 200));

    key('Escape');
    await wait(60);
    check('Esc goes up a step', cs.route().id === 'began/section-16/east-north-central', cs.route());

    cs.go('began/tennessee');
    await wait(60);
    check('a slice with no parts is picked in its pie', cs.pie().id === 'began' && cs.pie().pick === 'began/tennessee', cs.pie());
    check('its card says what it is', /24,000/.test(card.textContent), card.textContent.slice(0, 200));

    history.back();
    await wait(150);
    check('Back goes back', cs.route().id === 'began/section-16/east-north-central', cs.route());
    history.forward();
    await wait(150);
    check('Forward goes forward', cs.route().id === 'began/tennessee', cs.route());

    cs.go('text/land-ordinance');
    await wait(60);
    check('a text opens to its words', !reader.hidden && reader.querySelectorAll('p').length > 3, reader.hidden);
    const mark = reader.querySelector('mark');
    check('its passage about schools is lit', mark && /16/.test(mark.textContent), mark && mark.textContent);
    check('its card gives its links and fingerprint', /SHA-512 [0-9a-f]{128}/.test(card.textContent), card.textContent.slice(0, 200));

    key('1');
    await wait(60);
    check('1 starts again from the first pie', cs.route().id === 'began' && reader.hidden, cs.route());

    rows()[0].focus();
    rows()[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    check('↓ moves to the next row', document.activeElement === rows()[1], document.activeElement && document.activeElement.textContent);
    check('a row in focus lights its slice', document.querySelectorAll('#pie path.slice.dim').length === 5, document.querySelectorAll('#pie path.slice.dim').length);

    key('?');
    check('? shows the keys', !document.getElementById('keys').hidden);
    key('Escape');
    check('Esc puts them away', document.getElementById('keys').hidden);
  } catch (e) {
    out.errors.push('selftest: ' + ((e && e.stack) || e));
  }
  const pre = document.createElement('pre');
  pre.id = 'selftest';
  pre.textContent = JSON.stringify(out);
  document.body.appendChild(pre);
})();
