/* Preconfig Doctor live demo. The engine (engine.v2.js) is the engine's own Go code
 * compiled to WebAssembly; doctor-cases.v1.js holds logs recorded on a clean machine.
 * Nothing on this page talks to a server. */
(function () {
  'use strict';

  var DATA = window.DOCTOR_CASES || { cases: [] };
  var $ = function (id) { return document.getElementById(id); };
  var engine = null;
  var current = null;     // the preset in use, or null for a pasted log
  var last = null;        // the last diagnosis
  var fixedFiles = [];

  var WHERE = {
    spec: 'in preconfig.yaml',
    network: 'the network',
    repository: 'the repository',
    secret: 'a secret',
    code: 'the code',
    platform: 'the machine',
    unknown: 'unknown',
    passed: 'nothing to fix'
  };

  function text(el, s) { el.textContent = s == null ? '' : String(s); }

  function el(tag, cls, s) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (s != null) e.textContent = s;
    return e;
  }

  function lineCount(s) {
    if (!s) return 0;
    var n = s.split('\n').length;
    if (s.charAt(s.length - 1) === '\n') n--;
    return n;
  }

  function fmt(n) { return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }

  /* ---- presets ---------------------------------------------------------------------- */
  function renderPresets() {
    var box = $('presets');
    DATA.cases.forEach(function (c, i) {
      var b = el('button', 'preset');
      b.type = 'button';
      b.appendChild(document.createTextNode(c.label + ' '));
      b.appendChild(el('small', null, c.sub));
      b.addEventListener('click', function () { choose(i); });
      box.appendChild(b);
    });
    var own = el('button', 'preset', 'Paste your own log');
    own.type = 'button';
    own.addEventListener('click', function () { choose(-1); });
    box.appendChild(own);
  }

  function choose(i) {
    var buttons = $('presets').querySelectorAll('.preset');
    for (var k = 0; k < buttons.length; k++) buttons[k].classList.toggle('on', k === (i < 0 ? buttons.length - 1 : i));
    current = i >= 0 ? DATA.cases[i] : null;
    $('log-text').value = current ? current.log : '';
    $('spec-text').value = current ? current.spec : '';
    $('spec-box').open = false;
    showMeta();
    hideFix();
    if (current) {
      diagnose();
    } else {
      clearResult();
      $('log-text').focus();
    }
  }

  // A recorded log goes to the engine as the machine printed it. The text box
  // turns its carriage returns into line breaks, so it is read only once edited.
  function logText() { return current ? current.log : $('log-text').value; }

  function showMeta() {
    var s = logText();
    var meta = fmt(lineCount(s)) + ' lines';
    if (current) meta += ' · ' + current.made;
    text($('log-meta'), meta);
  }

  /* ---- diagnosis -------------------------------------------------------------------- */
  function clearResult() {
    last = null;
    $('dx-card').hidden = true;
    $('dx-empty').hidden = false;
    text($('n-lines'), fmt(lineCount(logText())));
    text($('n-evidence'), '0');
    text($('n-where'), 'none yet');
    $('where-box').className = 'counter';
    text($('dx-time'), '');
  }

  function diagnose() {
    if (!engine) return;
    var log = logText();
    var spec = $('spec-text').value;
    var t0 = performance.now();
    var res = engine.doctor(log, spec);
    var ms = performance.now() - t0;
    var d = res.diagnosis;
    last = d;
    hideFix();
    text($('dx-time'), 'Diagnosed in ' + (ms < 10 ? ms.toFixed(1) : Math.round(ms)) + ' ms in this page.');
    text($('n-lines'), fmt(d.lines));
    text($('n-evidence'), fmt((d.evidence || []).length));
    var cat = d.outcome === 'passed' ? 'passed' : (d.category || 'unknown');
    text($('n-where'), WHERE[cat] || cat);
    $('where-box').className = 'counter cat-' + cat;

    $('dx-empty').hidden = true;
    $('dx-card').hidden = false;
    if (d.outcome === 'passed') {
      text($('dx-step'), d.log + ' log');
      text($('dx-cause'), 'The log shows the setup finishing. Nothing to fix.');
    } else if (d.outcome === 'unclear') {
      text($('dx-step'), d.log + ' log');
      text($('dx-cause'), d.cause);
    } else {
      text($('dx-step'), (d.step || 'the log') + (d.exitCode ? '  ·  exit code ' + d.exitCode : ''));
      text($('dx-cause'), d.cause);
    }
    var ev = $('dx-evidence');
    ev.innerHTML = '';
    (d.evidence || []).forEach(function (l) {
      var li = el('li', /FAILED/.test(l.text) ? 'fail' : '');
      li.appendChild(el('span', 'ln', String(l.n)));
      li.appendChild(el('span', 'tx', l.text));
      ev.appendChild(li);
    });
    ev.hidden = !(d.evidence || []).length;

    var changes = d.changes || [];
    $('dx-fix').hidden = changes.length === 0;
    var ul = $('dx-changes');
    ul.innerHTML = '';
    changes.forEach(function (c) { ul.appendChild(el('li', null, describe(c))); });
    $('btn-fix').disabled = !$('spec-text').value.trim();
    $('btn-fix').title = $('spec-text').value.trim() ? '' : 'Add the preconfig.yaml the setup was built from, to apply the fix';

    $('dx-advice').hidden = !d.advice;
    text($('dx-advice'), d.advice || '');
    text($('dx-rule'), d.rule ? 'Rule ' + d.rule + ', read from a ' + d.log + ' log.' : (d.outcome === 'failed' ? 'No rule explains this log.' : ''));
  }

  function describe(c) {
    switch (c.op) {
      case 'add-package': return 'Add ' + c.value + ' to packages';
      case 'replace-package': return 'Replace ' + c.old + ' with ' + c.value + ' in packages';
      case 'add-service':
        return 'Add ' + c.key + ' ' + c.value + ' under services' + (c.user ? ', with user ' + c.user + ' and database ' + c.database : '');
      case 'set-runtime': return 'Set runtimes.' + c.key + ' to ' + c.value;
      case 'add-tool': return 'Add ' + c.value + ' to tools';
      case 'add-env': return 'Add ' + c.key + ' under env';
      case 'set-env': return 'Set ' + c.key + ' under env';
      case 'add-secret': return 'Add ' + c.key + ' under secrets';
    }
    return c.op;
  }

  /* ---- the fix ---------------------------------------------------------------------- */
  function hideFix() {
    $('fix-out').hidden = true;
    fixedFiles = [];
  }

  function applyFix() {
    if (!last || !(last.changes || []).length) return;
    var res = engine.doctorFix($('spec-text').value, last.changes);
    if (res.error) {
      $('dx-advice').hidden = false;
      text($('dx-advice'), 'The change wasn\'t applied: ' + res.error);
      return;
    }
    $('spec-text').value = res.spec;
    renderDiff($('spec-diff'), res.diff);
    fixedFiles = res.files || [];
    text($('fix-sub'), 'the change Doctor wrote; every platform\'s file follows from it');
    renderTabs();
    renderProof();
    $('fix-out').hidden = false;
    $('fix-out').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  function renderDiff(pre, diff) {
    pre.innerHTML = '';
    diff.split('\n').forEach(function (l) {
      if (l === '') return;
      var cls = 'ln';
      if (/^(\+\+\+|---)/.test(l)) cls = 'fh';
      else if (/^@@/.test(l)) cls = 'hunk';
      else if (l.charAt(0) === '+') cls = 'add';
      else if (l.charAt(0) === '-') cls = 'rm';
      pre.appendChild(el('span', cls, l));
    });
  }

  function renderTabs() {
    var tabs = $('file-tabs');
    tabs.innerHTML = '';
    fixedFiles.forEach(function (f, i) {
      var b = el('button', i === 0 ? 'on' : '', f.path);
      b.type = 'button';
      b.setAttribute('role', 'tab');
      b.addEventListener('click', function () {
        var all = tabs.querySelectorAll('button');
        for (var k = 0; k < all.length; k++) all[k].classList.toggle('on', k === i);
        text($('file-view'), f.content);
      });
      tabs.appendChild(b);
    });
    text($('file-view'), fixedFiles.length ? fixedFiles[0].content : '');
  }

  function renderProof() {
    var box = $('proof');
    box.innerHTML = '';
    var p = current && current.proof;
    if (!p || !(p.rounds || []).length || !samePlan(p.rounds[0].changes, last.changes)) {
      box.hidden = true;
      return;
    }
    box.hidden = false;
    box.className = 'proof' + (p.ready ? '' : ' partial');
    var r = p.rounds;
    var docker = current.kind === 'docker';
    var when = DATA.provenLong ? ' on ' + DATA.provenLong : '';
    var lead = el('p');
    lead.style.margin = '0';
    if (r.length === 1 && p.ready) {
      lead.appendChild(el('b', null, 'Proven on a clean machine. '));
      lead.appendChild(document.createTextNode(docker
        ? 'With this change, Cursor\'s Dockerfile was built again from a clean Ubuntu 24.04 image' + when + ', and the build finished in ' + r[0].seconds + ' seconds.'
        : 'With this change, preconfig verify ran again on a clean Ubuntu 24.04 machine' + when + ': READY in ' + r[0].seconds + ' seconds.'));
      box.appendChild(lead);
      return;
    }
    lead.appendChild(el('b', null, p.ready ? 'Proven on a clean machine, in ' + r.length + ' rounds. ' : 'Proven on a clean machine: the failure is gone. '));
    lead.appendChild(document.createTextNode(p.ready
      ? 'Each run' + when + ' got further, and Doctor read the next failure the same way:'
      : 'The next run' + when + ' got further and stopped on something else:'));
    box.appendChild(lead);
    var ol = el('ol');
    r.forEach(function (x) {
      var what = (x.changes || []).map(describe).join('; ');
      var a = x.after || {};
      var after = a.passed ? (docker ? 'built in ' : 'READY in ') + x.seconds + ' s'
        : (a.same ? 'the next run stopped in the same step, on another cause' : 'the next run stopped at ' + a.step) + (a.rule ? ' (' + (/^D[0-9]+$/.test(a.rule) ? 'rule ' : '') + a.rule + ')' : '');
      ol.appendChild(el('li', null, what + ': ' + after));
    });
    box.appendChild(ol);
  }

  function samePlan(a, b) {
    return JSON.stringify(a || []) === JSON.stringify(b || []);
  }

  /* ---- start ------------------------------------------------------------------------ */
  function start() {
    var rec = document.querySelectorAll('[data-recorded]');
    for (var i = 0; i < rec.length; i++) text(rec[i], DATA.recordedLong || DATA.recordedAt || '');
    renderPresets();
    $('btn-diagnose').addEventListener('click', diagnose);
    $('btn-fix').addEventListener('click', applyFix);
    $('log-text').addEventListener('input', function () { current = null; showMeta(); });
    if (!window.PreconfigEngine) {
      $('engine-state').className = 'bad';
      text($('engine-state'), 'The engine didn\'t load. Try another browser, or allow scripts on this page.');
      return;
    }
    window.PreconfigEngine.load().then(function (e) {
      if (!e.doctor) throw new Error('this engine has no Doctor');
      engine = e;
      var s = $('engine-state');
      s.innerHTML = '';
      s.appendChild(el('span', 'ok', 'Engine running in this page'));
      s.appendChild(document.createTextNode(': preconfig ' + e.version + ', Doctor ' + e.doctorVersion + ', knowledge as of ' + e.knowledge + '.'));
      text($('foot-version'), 'Preconfig Doctor ' + e.doctorVersion);
      $('btn-diagnose').disabled = false;
      choose(0);
    }).catch(function (err) {
      $('engine-state').className = 'bad';
      text($('engine-state'), 'The engine didn\'t start: ' + (err && err.message ? err.message : err));
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
  else start();
})();
