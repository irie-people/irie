// Runs after the page's own script, in the page's test (page_test.go): it
// drives the page from inside, as a person would, and writes what it found
// into a pre element that the test reads back from the page's DOM.
(async () => {
  const out = { checks: [], errors: window.__errors };
  const check = (name, ok, got) => out.checks.push({ name, ok: !!ok, got: got === undefined ? '' : JSON.stringify(got) });
  const wait = ms => new Promise(r => setTimeout(r, ms));
  try {
    const cg = window.chartersGraph;
    const card = document.getElementById('card');
    const canvas = document.getElementById('map');
    check('the page starts', cg && cg.nodes.size > 1500, cg && cg.nodes.size);
    check('the overview card shows', card.querySelector('h1') && card.querySelector('h1').textContent === 'The charters');

    cg.pick('declaration', { fly: true });
    await wait(30);
    check('a pick goes in the address', location.hash === '#declaration', location.hash);
    check('a text gets its card', card.querySelector('h1').textContent === 'The Declaration of Independence', card.querySelector('h1').textContent);
    const L = cg.lit();
    check('a text lights the texts it drew on and that drew on it', L.texts.has('virginia') && L.texts.has('articles') && L.texts.has('seneca'), [...L.texts]);

    cg.pick('rights/amendments/amendment-viii', { fly: true });
    await wait(30);
    check('a part lights the passages its links quote', cg.lit().callouts.has('english-rights'), [...cg.lit().callouts.keys()]);
    check('a part says it is the Constitution’s too', /The very same words as/.test(card.textContent));
    check('a part shows its words', /Excessive bail shall not be required/.test(card.querySelector('.words').textContent));

    cg.pick('idea/jury', { fly: true });
    await wait(30);
    check('an idea lights the texts that name it', cg.lit().texts.has('constitution') && cg.lit().badges.get('rights') >= 3, [...cg.lit().badges]);

    history.back();
    await wait(150);
    check('Back goes back', cg.current() === 'rights/amendments/amendment-viii', cg.current());
    history.forward();
    await wait(150);
    check('Forward goes forward', cg.current() === 'idea/jury', cg.current());

    cg.search('chuse');
    check('search finds the parchment’s “chuse”', cg.found().some(r => r.id.startsWith('constitution/article-i')), cg.found().map(r => r.id));
    cg.search('cruel and unusual');
    check('search finds 1689’s “cruell and unusuall”', cg.found().some(r => r.id.startsWith('english-rights')), cg.found().map(r => r.id));
    check('search finds an idea by its words', cg.found().some(r => r.id === 'idea/punishment'), cg.found().map(r => r.id));
    const hit = cg.found().findIndex(r => r.block !== undefined && r.id.startsWith('english-rights'));
    cg.choose(hit);
    await wait(30);
    check('a passage found opens with its words lit', card.querySelector('.lit mark') !== null, cg.current());

    cg.pick('declaration');
    canvas.focus();
    canvas.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await wait(30);
    check('an arrow walks the graph', cg.current() && cg.current() !== 'declaration', cg.current());

    const t = cg.nodes.get('constitution');
    cg.go({ x: t.x, y: t.y, k: 1.5 });
    cg.pick(null);
    const r = canvas.getBoundingClientRect();
    const cx = r.left + cg.sx(t.x) + 3, cy = r.top + cg.sy(t.y) + 2;
    canvas.dispatchEvent(new PointerEvent('pointerdown', { clientX: cx, clientY: cy, pointerId: 1, bubbles: true, pointerType: 'mouse' }));
    canvas.dispatchEvent(new PointerEvent('pointerup', { clientX: cx, clientY: cy, pointerId: 1, bubbles: true, pointerType: 'mouse' }));
    check('a click picks the nearest text', cg.current() === 'constitution', cg.current());

    cg.go(cg.fitView());
    const k0 = cg.cam.k;
    canvas.dispatchEvent(new WheelEvent('wheel', { deltaY: -50, ctrlKey: true, clientX: r.left + 100, clientY: r.top + 100, bubbles: true, cancelable: true }));
    check('a pinch zooms', Math.abs(cg.cam.k / k0 - Math.exp(0.5)) < 0.01, cg.cam.k / k0);
    const x0 = cg.cam.x;
    canvas.dispatchEvent(new WheelEvent('wheel', { deltaX: 40, deltaY: 0, clientX: r.left + 100, clientY: r.top + 100, bubbles: true, cancelable: true }));
    check('a scroll pans', cg.cam.x > x0, cg.cam.x - x0);

    cg.pick('constitution', { render: true });
    await wait(30);
    const row = card.querySelector('[role="tree"] [role="treeitem"][aria-expanded="false"]');
    row.focus();
    row.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    check('→ opens a row of the tree', row.getAttribute('aria-expanded') === 'true' && row.querySelector('[role="group"]') !== null);

    for (let k = 0.02; k < 5; k *= 1.4) { cg.cam.k = k; cg.drawNow(); }
    check('it draws at every zoom', true);
  } catch (e) {
    out.errors.push('selftest: ' + (e && e.stack ? e.stack : e));
  }
  const pre = document.createElement('pre');
  pre.id = 'selftest';
  pre.textContent = JSON.stringify(out);
  document.body.appendChild(pre);
})();
