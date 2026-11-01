(function () {
  var ua = navigator.userAgent;
  var ios = /iPhone|iPad|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  if (ios && /CriOS|FxiOS|EdgiOS|OPiOS|GSA\//.test(ua)) document.getElementById('safari').className = 'warn on';
  if (!ios) {
    document.getElementById('elsewhere').hidden = false;
    document.getElementById('here').textContent = location.href.replace(/[?#].*$/, '');
  }
  var https = 'https://' + location.hostname + ':7387/';
  function use(c) {
    if (c.name) {
      document.getElementById('name').textContent = c.name;
      var all = document.querySelectorAll('.nm');
      for (var i = 0; i < all.length; i++) all[i].textContent = c.name;
      document.title = 'Set up Irie for ' + c.name;
    }
    if (c.https) https = c.https;
    if (c.profile) document.getElementById('profile').setAttribute('href', c.profile);
    document.getElementById('open').setAttribute('href', https);
    document.getElementById('url').textContent = https.replace(/\/$/, '');
  }
  use({});
  fetch('/setup.json', { cache: 'no-store' }).then(function (r) { return r.json(); }).then(use).catch(function () {});

  // The certificate works once the app's port answers over https: then step 1 is done.
  var trusted = false;
  function check() {
    if (trusted || document.visibilityState !== 'visible') return;
    fetch(https.replace(/\/?$/, '/') + 'api/info', { cache: 'no-store', mode: 'cors' }).then(function (r) {
      if (!r.ok) return;
      trusted = true;
      document.getElementById('s1').className = 'step done';
      document.getElementById('trust').lastChild.textContent = 'This iPhone trusts it.';
      document.getElementById('open').className = 'btn';
      document.getElementById('profile').className = 'btn quiet';
    }).catch(function () {});
  }
  setInterval(check, 3000);
  document.addEventListener('visibilitychange', check);
  setTimeout(check, 400);
})();
