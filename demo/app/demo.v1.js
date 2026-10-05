/* Preconfiguration.com Alpha live demo (for preconfiguration.com/demo/): the page. Plain
 * JavaScript, no libraries. recordings.v1.js holds what the command-line engine printed on the
 * sample repositories and two verify runs on a clean machine; engine.v1.js holds the engine
 * itself. This file replays the runs step by step and lets you use the engine yourself. */
(function () {
  'use strict';

  var D = window.PRECONFIG_DEMO;
  var E = null;                 /* the engine, once loaded */
  var OUTRO_MS = 3400;          /* pause on each step's conclusion while playing */
  var LINE_MS = 55;             /* pace of command output */
  var SPEED = 5;                /* verify runs replay five times faster than recorded */

  /* ---- helpers ------------------------------------------------------------------------ */
  function $(id) { return document.getElementById(id); }
  function esc(s) {
    return String(s).replace(/[&<>"]/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]; });
  }
  function lines(text) {
    var ls = String(text || '').split('\n');
    if (ls.length && ls[ls.length - 1] === '') ls.pop();
    return ls;
  }
  function mmss(ms) { var s = Math.round(ms / 1000); return Math.floor(s / 60) + ':' + ('0' + (s % 60)).slice(-2); }
  function base(p) { return p.slice(p.lastIndexOf('/') + 1); }
  function plural(n, w) { return n + ' ' + w + (n === 1 ? '' : 's'); }
  function dateText(iso) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso || '');
    if (!m) return iso || '';
    var months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
    return months[+m[2] - 1] + ' ' + (+m[3]) + ', ' + m[1];
  }
  var SVGNS = 'http://www.w3.org/2000/svg';
  function svg(tag, attrs, parent) {
    var e = document.createElementNS(SVGNS, tag);
    for (var k in attrs) if (Object.prototype.hasOwnProperty.call(attrs, k)) e.setAttribute(k, attrs[k]);
    if (parent) parent.appendChild(e);
    return e;
  }
  function stext(parent, x, y, cls, s, anchor) {
    var t = svg('text', { x: x, y: y, 'class': cls }, parent);
    if (anchor) t.setAttribute('text-anchor', anchor);
    t.textContent = s;
    return t;
  }

  /* ---- the terminal ------------------------------------------------------------------- */
  var term = $('term');
  function termClear() { term.textContent = ''; }
  function termLine(text, cls) {
    var d = document.createElement('span');
    d.className = 'l ' + (cls || 'out');
    var cut = cls === 'yaml' ? text.indexOf('  # ') : -1;
    if (/^\s*#/.test(text) && cls === 'yaml') {
      d.className = 'l dim';
      d.textContent = text;
    } else if (cut >= 0) {
      d.className = 'l out';
      d.appendChild(document.createTextNode(text.slice(0, cut)));
      var c = document.createElement('span');
      c.className = 'dim';
      c.textContent = text.slice(cut);
      d.appendChild(c);
    } else {
      if (cls === 'yaml') d.className = 'l out';
      d.textContent = text === '' ? '​' : text;
    }
    term.appendChild(d);
    term.scrollTop = term.scrollHeight;
  }
  /* The colour of a line the command printed. */
  function lineClass(l) {
    if (/: error [A-Z]\d{3}: /.test(l)) return 'err';
    if (/: warning [A-Z]\d{3}: /.test(l)) return 'warn';
    if (/: note [A-Z]\d{3}: /.test(l)) return 'note';
    if (/^ {4}\S/.test(l)) return 'hint';
    if (/^wrote /.test(l)) return 'ok';
    if (/^unchanged /.test(l)) return 'dim';
    if (/^checked \d+ files?: 0 errors, 0 warnings/.test(l)) return 'good';
    if (/^checked \d+ files?: /.test(l)) return 'bad';
    if (/^Run with --diff/.test(l)) return 'dim';
    if (/^verify .*  READY  /.test(l)) return 'good';
    if (/^verify .*  NOT READY  /.test(l)) return 'bad';
    if (/^verify .*  ok     /.test(l)) return 'ok';
    if (/^verify .*  FAIL   /.test(l)) return 'err';
    if (/^verify .*  warn   /.test(l)) return 'warn';
    if (/^verify .*  have   /.test(l)) return 'have';
    if (/^ +\| E /.test(l)) return 'err';
    if (/^ +\|/.test(l)) return 'dim';
    if (/^ +the last lines it printed:/.test(l)) return 'note';
    return 'out';
  }

  /* ---- step 1: the repository --------------------------------------------------------- */
  var REPO_ABOUT = {
    '.python-version': 'the Python version',
    'pyproject.toml': 'the project and its test settings',
    'requirements.txt': 'the Python packages',
    'compose.yaml': 'the services the developers run locally',
    '.env.example': 'the environment variables, without real values',
    'README.md': 'how a person sets it up',
    'app/__init__.py': 'code',
    'app/orders.py': 'code: orders in PostgreSQL, counts cached in Redis',
    'tests/test_orders.py': 'the tests, which need both databases'
  };
  var repoLi = {};
  function repoReset() {
    var ul = $('repo-files');
    ul.textContent = '';
    repoLi = {};
    Object.keys(D.repo.files).forEach(function (p) {
      var li = document.createElement('li');
      li.innerHTML = '<span class="fn">' + esc(p) + '</span><span class="fd">' + esc(REPO_ABOUT[p] || '') + '</span>';
      ul.appendChild(li);
      repoLi[p] = li;
    });
    $('repo-verdict').hidden = true;
  }
  /* A line of the draft names its source in a comment; mark the file it came from. */
  function repoOnLine(l) {
    var cut = l.indexOf('  # ');
    if (cut < 0 || /^\s*#/.test(l)) return;
    var source = l.slice(cut + 4);
    var file = null;
    D.detect.json.found.forEach(function (f) { if (!file && source.indexOf(f) === 0) file = f; });
    if (!file && /requirements/.test(source)) file = 'requirements.txt';
    if (!file || !repoLi[file]) return;
    var li = repoLi[file];
    li.className = 'read';
    var pick = document.createElement('span');
    pick.className = 'pick';
    var key = l.slice(0, cut).trim();
    pick.textContent = '→ ' + key;
    li.appendChild(pick);
  }
  function repoFinish() {
    Object.keys(repoLi).forEach(function (p) {
      if (D.detect.json.found.indexOf(p) < 0) repoLi[p].className = 'skip';
    });
    var v = $('repo-verdict');
    v.hidden = false;
    v.className = 'verdict-line';
    v.textContent = 'detect read ' + plural(D.detect.json.found.length, 'file') + ' and skipped the code and the tests.';
    if (E) {
      var a = E.build(D.detect.json.spec), b = E.build(D.detect.teamSpec);
      var same = a.files.length === b.files.length && a.files.every(function (f, i) { return f.path === b.files[i].path && f.content === b.files[i].content; });
      v.textContent += same
        ? ' Built in this page, the draft gives the same ' + a.files.length + ' files, byte for byte, as the preconfig.yaml the team wrote by hand.'
        : ' Built in this page, the draft differs from the preconfig.yaml the team wrote.';
      if (!same) v.className = 'verdict-line warn';
    }
  }

  /* ---- step 2: one spec, five targets ------------------------------------------------- */
  var TARGETS = [
    { key: 'devcontainer', name: 'Dev container', sub: 'Codespaces, VS Code, editors', files: ['.devcontainer/devcontainer.json', '.devcontainer/compose.yaml'] },
    { key: 'copilot', name: 'GitHub Copilot', sub: 'cloud agent setup steps', files: ['.github/workflows/copilot-setup-steps.yml'] },
    { key: 'cursor', name: 'Cursor', sub: 'cloud agent environment', files: ['.cursor/environment.json', '.cursor/Dockerfile'] },
    { key: 'cloud-init', name: 'cloud-init', sub: 'a fresh VM', files: ['cloud-init.yaml'] },
    { key: 'script', name: 'Setup script', sub: 'Codex, Claude Code, Jules', files: ['.preconfig/setup.sh'] }
  ];
  var T = {};
  var written = {};
  function drawTargets() {
    var g = $('targets');
    g.textContent = '';
    var specLines = lines(D.build.spec).filter(function (l) { return l.trim() !== '' && !/^\s*#/.test(l); }).length;
    svg('rect', { x: 8, y: 125, width: 170, height: 80, rx: 12, 'class': 'src' }, g);
    stext(g, 93, 158, 'src-name', 'preconfig.yaml', 'middle');
    stext(g, 93, 180, 'src-sub', specLines + ' lines of settings', 'middle');
    svg('rect', { x: 212, y: 145, width: 116, height: 40, rx: 20, 'class': 'engine' }, g);
    stext(g, 270, 170, 'engine-name', 'preconfig build', 'middle');
    svg('path', { d: 'M178,165 L212,165', 'class': 'wire' }, g);
    TARGETS.forEach(function (t, i) {
      var y = 8 + i * 64;
      var grp = svg('g', { 'class': 't' }, g);
      svg('path', { d: 'M328,165 C360,165 360,' + (y + 26) + ' 392,' + (y + 26), 'class': 'wire' }, grp);
      svg('rect', { x: 392, y: y, width: 300, height: 54, rx: 10, 'class': 'box' }, grp);
      stext(grp, 404, y + 20, 'name', t.name);
      stext(grp, 404 + (t.name.length * 8.6) + 8, y + 20, 'sub', t.sub).setAttribute('style', 'font-family: var(--sans)');
      stext(grp, 404, y + 41, 'sub', t.files.map(base).join(', '));
      var size = stext(grp, 684, y + 41, 'size', '', 'end');
      T[t.key] = { g: grp, size: size, t: t };
    });
  }
  function targetsReset() {
    written = {};
    drawTargets();
    $('file-tabs').textContent = '';
    $('file-view').textContent = '';
    var n = $('build-here-note');
    n.className = 'muted small';
    n.textContent = 'The engine, compiled to WebAssembly, builds the same spec here and compares its seven files with the command line’s.';
  }
  function showFile(path, tabs, view, files) {
    var bs = tabs.children;
    for (var i = 0; i < bs.length; i++) bs[i].className = bs[i].getAttribute('data-path') === path ? 'on' : '';
    view.innerHTML = highlight(files[path] || '', path);
    view.scrollTop = 0;
  }
  function addTab(path, tabs, view, files) {
    var b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('data-path', path);
    b.setAttribute('role', 'tab');
    b.textContent = base(path);
    b.title = path;
    b.addEventListener('click', function () { showFile(path, tabs, view, files); });
    tabs.appendChild(b);
  }
  function highlight(text, path) {
    var comment = /\.(ya?ml|sh)$|Dockerfile$/.test(path) ? /^(\s*)(#.*)$/ : /\.json$/.test(path) ? /^(\s*)(\/\/.*)$/ : null;
    return lines(text).map(function (l) {
      var m = comment && comment.exec(l);
      if (m) return esc(m[1]) + '<span class="c">' + esc(m[2]) + '</span>';
      return esc(l);
    }).join('\n');
  }
  function targetsOnLine(l) {
    var m = /^(wrote|unchanged) +(\S+)$/.exec(l);
    if (!m) return;
    written[m[2]] = true;
    addTab(m[2], $('file-tabs'), $('file-view'), D.build.files);
    showFile(m[2], $('file-tabs'), $('file-view'), D.build.files);
    TARGETS.forEach(function (t) {
      if (t.files.every(function (f) { return written[f]; })) {
        var o = T[t.key];
        if (o.g.getAttribute('class') !== 't on') {
          o.g.setAttribute('class', 't on');
          var bytes = t.files.reduce(function (a, f) { return a + (D.build.files[f] || '').length; }, 0);
          var nlines = t.files.reduce(function (a, f) { return a + lines(D.build.files[f]).length; }, 0);
          o.size.textContent = nlines + ' lines';
          o.size.setAttribute('title', bytes + ' bytes');
        }
      }
    });
  }

  /* ---- step 3: findings --------------------------------------------------------------- */
  var SO = {
    C002: 'Copilot looks only for a job named copilot-setup-steps: without one, the agent session stops with an error.',
    C004: 'The variable never reaches the steps Copilot runs.',
    C005: 'Copilot doesn’t accept the file with a longer timeout.',
    C007: 'Nobody sees the setup fail until an agent session does.',
    K002: 'Cursor’s schema doesn’t allow the key, so the file fails validation.',
    X001: 'The file is missing, so the platform doesn’t get the setup the spec describes.',
    X003: 'The agent works on a different version than the team.',
    X004: 'The tests fail on this platform: nothing starts the database.'
  };
  var fcards = {};
  var findingIndex = 0;
  function findingsReset(report) {
    $('findings').textContent = '';
    fcards = {};
    findingIndex = 0;
    ['n-files', 'n-errors', 'n-warnings', 'n-missing'].forEach(function (id) { $(id).textContent = '0'; });
    $('n-files').textContent = report ? report.checked.length : 0;
  }
  function addFinding(d) {
    var card = fcards[d.file];
    if (!card) {
      card = document.createElement('div');
      card.className = 'fcard' + (d.code === 'X001' ? ' missing' : '');
      var h = document.createElement('h4');
      h.innerHTML = esc(d.file) + (d.code === 'X001' ? ' <small>missing</small>' : '');
      card.appendChild(h);
      $('findings').appendChild(card);
      fcards[d.file] = card;
    }
    var row = document.createElement('div');
    row.className = 'frow';
    var where = d.line ? 'line ' + d.line : '';
    row.innerHTML = '<span class="sev ' + d.severity + '">' + d.severity + '</span>' +
      '<span class="msg">' + esc(d.message) + ' <code>' + esc(d.code + (where ? ', ' + where : '')) + '</code></span>' +
      (SO[d.code] ? '<span class="so">' + esc(SO[d.code]) + '</span>' : '');
    card.appendChild(row);
    var box = $('findings');
    box.scrollTop = box.scrollHeight;
    if (d.severity === 'error') $('n-errors').textContent = +$('n-errors').textContent + 1;
    if (d.severity === 'warning') $('n-warnings').textContent = +$('n-warnings').textContent + 1;
    if (d.code === 'X001') $('n-missing').textContent = +$('n-missing').textContent + 1;
  }
  function findingsOnLine(l) {
    if (!/: (error|warning|note) [A-Z]\d{3}: /.test(l)) return;
    var d = D.check.report.findings[findingIndex++];
    if (d) addFinding(d);
  }

  /* ---- steps 4 and 5: the verify timeline --------------------------------------------- */
  var V = null;
  function timelineReset(run) {
    V = { run: run, rows: {}, total: run.events[run.events.length - 1].t, log: [] };
    $('timeline').textContent = '';
    $('machine').textContent = '';
    $('machine-step').textContent = '';
    $('v-clock').textContent = '0.0 s';
    $('v-have').textContent = 'nothing yet';
    $('v-result').textContent = 'starting';
    $('v-result-box').className = 'counter';
    run.events.forEach(function (e) {
      if (e.type !== 'step.start') return;
      var row = document.createElement('div');
      row.className = 'trow wait';
      row.innerHTML = '<span class="tn" title="' + esc(e.step) + '">' + esc(e.step) + '</span><span class="tb"><i></i></span><span class="ts"></span>';
      $('timeline').appendChild(row);
      V.rows[e.step] = { el: row, start: e.t };
    });
  }
  function timelineEvent(e) {
    var r = V.rows[e.step];
    $('v-clock').textContent = e.t.toFixed(1) + ' s';
    if (e.type === 'step.start' && r) {
      r.el.className = 'trow run';
      place(r, e.t, 0.2);
      $('machine-step').textContent = e.step;
      V.current = r;
    } else if (e.type === 'step.end' && r) {
      var ok = e.fields && e.fields.ok;
      r.el.className = 'trow ' + (ok ? 'ok' : 'fail');
      place(r, r.start, Math.max(e.fields.seconds, 0.1));
      r.el.querySelector('.ts').textContent = e.fields.seconds.toFixed(1) + ' s';
      V.current = null;
    } else if (e.type === 'machine.ready') {
      $('v-have').textContent = e.fields.versions;
    } else if (e.type === 'verify.end') {
      var ready = e.fields.code === 0;
      $('v-result').textContent = ready ? 'READY' : 'NOT READY, exit ' + e.fields.code;
      $('v-result-box').className = 'counter ' + (ready ? 'good' : 'bad');
    }
  }
  function place(r, start, secs) {
    var i = r.el.querySelector('i');
    i.style.left = (start / V.total * 100).toFixed(2) + '%';
    i.style.width = Math.max(secs / V.total * 100, 0.8).toFixed(2) + '%';
  }
  /* While a step runs, its bar grows with the clock. */
  function timelineClock(t) {
    if (!V || !V.current) return;
    $('v-clock').textContent = t.toFixed(1) + ' s';
    place(V.current, V.current.start, Math.max(t - V.current.start, 0.1));
  }
  function machineLine(l) {
    var box = $('machine');
    var d = document.createElement('div');
    if (/^E |Error|FAILED|failed/.test(l)) d.className = 'er';
    else if (/^>>> preconfig: /.test(l)) d.className = 'mk';
    d.textContent = l;
    box.appendChild(d);
    while (box.childNodes.length > 14) box.removeChild(box.firstChild);
  }

  /* A unified diff as block lines, coloured. */
  function diffHTML(text) {
    return lines(text).map(function (l) {
      var cls = /^(---|\+\+\+) /.test(l) ? 'fh' : /^@@/.test(l) ? 'hunk' : /^\+/.test(l) ? 'add' : /^-/.test(l) ? 'rm' : '';
      return '<span class="ln ' + cls + '">' + (l === '' ? '\u200b' : esc(l)) + '</span>';
    }).join('');
  }

  /* ---- step 6: one change ------------------------------------------------------------- */
  var changeLi = {};
  function changeReset() {
    var before = lines(D.change.before), after = lines(D.change.after);
    var out = [];
    for (var i = 0; i < before.length; i++) {
      if (before[i] !== after[i]) {
        out.push('@@ line ' + (i + 1) + ' @@');
        for (var k = Math.max(0, i - 3); k < i; k++) out.push(' ' + before[k]);
        out.push('-' + before[i]);
        out.push('+' + after[i]);
        for (k = i + 1; k < Math.min(before.length, i + 4); k++) out.push(' ' + before[k]);
      }
    }
    $('spec-diff').innerHTML = diffHTML(out.join('\n'));
    var ul = $('change-files');
    ul.textContent = '';
    changeLi = {};
    Object.keys(D.change.files).forEach(function (p) {
      var li = document.createElement('li');
      li.innerHTML = '<span class="fn">' + esc(p) + '</span><span class="fd">up to date with the old spec</span>';
      ul.appendChild(li);
      changeLi[p] = li;
    });
    $('change-diffs').innerHTML = D.change.report.diffs.map(function (d) { return diffHTML(d.diff); }).join('<span class="ln">\u200b</span>');
    $('change-diffs-box').open = false;
  }
  function changeOnLine(l, phase) {
    var m;
    if (phase === 'check' && (m = /^(\S+?)(?::\d+)?: error (X00\d): (.*)$/.exec(l))) {
      var li = changeLi[m[1]];
      if (!li) return;
      li.className = 'changed';
      var fd = li.querySelector('.fd');
      if (m[2] === 'X003') fd.textContent = m[3].replace(/^\S+ installs /, 'installs ').replace(/; preconfig\.yaml asks for .*$/, '; the spec now says 17');
      else if (fd.textContent === 'up to date with the old spec') fd.textContent = 'differs from a fresh build';
    } else if (phase === 'build' && (m = /^(wrote|unchanged) +(\S+)$/.exec(l))) {
      var li2 = changeLi[m[2]];
      if (!li2) return;
      li2.className = m[1] === 'wrote' ? 'fixed' : 'same';
      li2.querySelector('.fd').textContent = m[1] === 'wrote' ? 'rewritten from the new spec' : 'unchanged: it runs .preconfig/setup.sh, which carries the version';
    }
  }

  /* ---- the scenes --------------------------------------------------------------------- */
  function cmdItems(run, onLine, cls) {
    var items = [{ t: 0, k: 'cmd', text: '$ ' + run.cmd }];
    var t = 380;
    lines(run.err).concat(lines(run.out)).forEach(function (l) {
      items.push({ t: t, k: 'line', text: l, cls: cls || lineClass(l), fn: onLine });
      t += LINE_MS;
    });
    if (run.code) items.push({ t: t + 120, k: 'line', text: '(exit ' + run.code + ')', cls: 'exit' });
    return { items: items, ms: t + 700 };
  }

  function verifyItems(run) {
    var items = [{ t: 0, k: 'cmd', text: '$ ' + run.cmd }];
    var t0 = 380;
    lines(run.err).forEach(function (l, i) { items.push({ t: t0 + i * LINE_MS, k: 'line', text: l, cls: lineClass(l) }); });
    var off = t0 + lines(run.err).length * LINE_MS + 250;
    var at = function (secs) { return off + secs * 1000 / SPEED; };
    run.events.forEach(function (e) {
      items.push({ t: at(e.t), k: 'ev', e: e });
      if (e.line) items.push({ t: at(e.t), k: 'line', text: e.line, cls: lineClass(e.line) });
    });
    /* The machine's own output, spread over each step's recorded time. */
    var starts = run.events.filter(function (e) { return e.type === 'step.start'; });
    var ends = {};
    run.events.forEach(function (e) { if (e.type === 'step.end') ends[e.step] = e.t; });
    var groups = [], cur = null;
    run.log.forEach(function (l) {
      var m = /^>>> preconfig: (?:FAILED: )?((?:machine|services|project|ready) \d+\/\d+: .*?)(?: \(exit code \d+\))?$/.exec(l);
      if (m && !/^>>> preconfig: FAILED/.test(l)) {
        cur = { step: m[1], lines: [l] };
        groups.push(cur);
      } else if (cur) cur.lines.push(l);
    });
    groups.forEach(function (g) {
      var s = starts.filter(function (e) { return e.step === g.step; })[0];
      if (!s) return;
      var end = ends[g.step] !== undefined ? ends[g.step] : s.t + 0.5;
      var n = g.lines.length;
      g.lines.forEach(function (l, i) {
        items.push({ t: at(s.t + (end - s.t) * (i / Math.max(n, 1))), k: 'log', text: l });
      });
    });
    var last = at(run.events[run.events.length - 1].t);
    var t = last + 250;
    lines(run.tail).forEach(function (l) { items.push({ t: t, k: 'line', text: l, cls: lineClass(l) }); t += LINE_MS; });
    if (run.code) { items.push({ t: t + 100, k: 'line', text: '(exit ' + run.code + ')', cls: 'exit' }); t += 100; }
    items.sort(function (a, b) { return a.t - b.t; });
    return { items: items, ms: t + 900, tick: function (elapsed) { timelineClock(Math.max(0, (elapsed - off) * SPEED / 1000)); } };
  }

  var totalSteps = D.verify.pass.events.filter(function (e) { return e.type === 'step.start'; }).length;
  var passEnd = D.verify.pass.events[D.verify.pass.events.length - 1];
  var failEnd = D.verify.fail.events[D.verify.fail.events.length - 1];
  var changed = D.change.report.diffs.length;
  var unchangedCount = Object.keys(D.change.files).length - changed;
  var brokenReport = D.check.report;
  var testsLine = (/(\d+) passed/.exec(passEnd.fields.message) || [])[0] || 'the tests passed';

  var SCENES = [
    {
      title: 'Draft the spec from the repository',
      intro: 'orders-api is a small Python service: orders in PostgreSQL, counts cached in Redis, and tests that need both. It has no setup files for any agent yet. preconfig detect reads the files it already has and drafts preconfig.yaml. It runs nothing.',
      outro: 'detect read ' + plural(D.detect.json.found.length, 'file') + ' and ran nothing. Comments in the draft say which file each part came from, so a person can check it in a minute before building from it.',
      panel: 'repo',
      enter: repoReset,
      commands: [cmdItems(D.detect.run, repoOnLine, 'yaml'), cmdItems(D.detect.write)],
      finish: repoFinish
    },
    {
      title: 'Build every platform’s files from one spec',
      intro: 'preconfig build compiles the spec into the files each platform reads: a dev container, GitHub Copilot’s setup workflow, Cursor’s environment, cloud-init for a fresh VM, and a plain setup script for the agents that take one.',
      outro: Object.keys(D.build.files).length + ' files from one spec, with a note for each secret the files can’t carry. Build twice and you get the same bytes, which is how check can tell when a file has drifted.',
      panel: 'targets',
      enter: targetsReset,
      commands: [cmdItems(D.build.run, targetsOnLine), cmdItems(D.build.check)]
    },
    {
      title: 'Check setup files written by hand',
      intro: 'The same service as a team might set it up by hand: a Copilot workflow, a Cursor environment and a dev container, each written on its own. preconfig check reads them against each platform’s format and against preconfig.yaml.',
      outro: plural(brokenReport.errors, 'error') + ' and ' + plural(brokenReport.warnings, 'warning') + ', and nothing flagged any of them when the files were written. The misnamed job stops Copilot’s next session with an error, Cursor’s schema doesn’t allow the unknown key, and the dev container starts without the databases, so the tests can’t pass there.',
      panel: 'findings',
      enter: function () { findingsReset(brokenReport); },
      commands: [cmdItems(D.check.run, findingsOnLine)]
    },
    {
      title: 'Verify it on a clean machine',
      intro: 'preconfig verify starts a fresh Ubuntu 24.04 container, runs the generated setup script and then the ready check, which is the project’s own tests. This run was recorded and plays five times faster here.',
      outro: 'READY in ' + passEnd.t.toFixed(1) + ' seconds: ' + (D.verify.pass.events.filter(function (e) { return e.type === 'machine.ready'; })[0] || { fields: { versions: '' } }).fields.versions + ' installed and running, ' + totalSteps + ' steps, and ' + testsLine + '. Any agent that runs these steps starts from the same machine.',
      panel: 'timeline',
      enter: function () { timelineReset(D.verify.pass); },
      commands: [verifyItems(D.verify.pass)]
    },
    {
      title: 'Catch a missing service before an agent does',
      intro: 'The same repository with Redis left out of preconfig.yaml. preconfig warns at once that REDIS_URL points at a Redis nothing starts. verify shows what that would cost.',
      outro: 'The setup worked and the tests failed. verify names the step, exits with 1 and shows pytest’s own error lines, ' + failEnd.t.toFixed(1) + ' seconds after it started. Without it, an agent would find this out in the middle of its task.',
      panel: 'timeline',
      enter: function () { timelineReset(D.verify.fail); },
      commands: [verifyItems(D.verify.fail)]
    },
    {
      title: 'Change one line, and every platform follows',
      intro: 'The team moves to PostgreSQL 17. One line changes in preconfig.yaml. check lists every file that is now out of date, and build rewrites them.',
      outro: 'One edit: ' + plural(changed, 'file') + ' rewritten, ' + unchangedCount + ' unchanged, and check is clean again. Without a spec, the same upgrade means editing each platform’s file by hand and hoping none is missed.',
      panel: 'change',
      enter: changeReset,
      commands: [
        cmdItems(D.change.edit),
        cmdItems(D.change.check, function (l) { changeOnLine(l, 'check'); }),
        cmdItems(D.change.build, function (l) { changeOnLine(l, 'build'); }),
        cmdItems(D.change.checkAfter)
      ]
    }
  ];

  /* ---- playback ------------------------------------------------------------------------ */
  var S = { scene: 0, cmd: 0, item: 0, t0: 0, playing: false, pausedAt: 0, phase: 'idle', outroT: 0, started: false };
  var done = {};
  var totalMs = 0;
  SCENES.forEach(function (s, i) { s.no = i + 1; s.ms = s.commands.reduce(function (a, c) { return a + c.ms; }, 0); totalMs += s.ms; });
  function sceneOffset(i) { var t = 0; for (var k = 0; k < i; k++) t += SCENES[k].ms; return t; }

  function buildSteps() {
    var nav = $('steps');
    SCENES.forEach(function (s, i) {
      var b = document.createElement('button');
      b.type = 'button';
      b.innerHTML = '<b>Step ' + s.no + '</b><span>' + esc(s.title) + '</span>';
      b.addEventListener('click', function () { goScene(i, true); });
      nav.appendChild(b);
    });
  }
  function markSteps() {
    var bs = $('steps').children;
    for (var i = 0; i < bs.length; i++) bs[i].className = (i === S.scene ? 'on' : '') + (done[i] ? ' done' : '');
  }

  function showScene(i) {
    var s = SCENES[i];
    $('scene-no').textContent = 'Step ' + s.no + ' of ' + SCENES.length;
    $('scene-title').textContent = s.title;
    $('scene-intro').textContent = s.intro;
    $('scene-outro').hidden = true;
    $('scene-outro').textContent = s.outro;
    ['repo', 'targets', 'findings', 'timeline', 'change'].forEach(function (p) { $('panel-' + p).hidden = s.panel !== p; });
    $('term-title').textContent = s.panel === 'timeline'
      ? 'Terminal, replayed from the recorded run at five times the speed'
      : 'Terminal, replayed from the recorded run';
    termClear();
    s.enter();
  }

  function enterScene(i) {
    S.scene = i; S.cmd = 0; S.item = 0; S.phase = 'cmd';
    showScene(i);
    startCmd();
    markSteps();
  }
  function startCmd() { S.item = 0; S.t0 = performance.now(); }

  function finishCmd() {
    var s = SCENES[S.scene];
    if (S.cmd + 1 < s.commands.length) {
      S.cmd++;
      startCmd();
      return;
    }
    if (s.finish) s.finish();
    S.phase = 'outro';
    S.outroT = performance.now();
    $('scene-outro').hidden = false;
    done[S.scene] = true;
    markSteps();
  }

  function applyItem(it) {
    if (it.k === 'cmd') termLine(it.text, 'cmd');
    else if (it.k === 'line') { termLine(it.text, it.cls); if (it.fn) it.fn(it.text); }
    else if (it.k === 'ev') timelineEvent(it.e);
    else if (it.k === 'log') machineLine(it.text);
  }

  function renderSceneInstantly(i) {
    enterScene(i);
    var s = SCENES[i];
    for (var k = 0; k < s.commands.length; k++) {
      var c = s.commands[k];
      for (; S.item < c.items.length; S.item++) applyItem(c.items[S.item]);
      finishCmd();
    }
  }

  function elapsed() { return (S.playing ? performance.now() : S.pausedAt) - S.t0; }

  function tick() {
    if (!S.playing) return;
    var s = SCENES[S.scene];
    if (S.phase === 'cmd') {
      var c = s.commands[S.cmd], t = elapsed();
      while (S.item < c.items.length && c.items[S.item].t <= t) { applyItem(c.items[S.item]); S.item++; }
      if (c.tick) c.tick(t);
      if (t >= c.ms) finishCmd();
    } else if (S.phase === 'outro') {
      if (performance.now() - S.outroT >= OUTRO_MS) {
        if (S.scene + 1 < SCENES.length) enterScene(S.scene + 1);
        else { stop(true); return; }
      }
    }
    progress();
    requestAnimationFrame(tick);
  }

  function progress() {
    var s = SCENES[S.scene];
    var inScene = 0;
    for (var k = 0; k < S.cmd; k++) inScene += s.commands[k].ms;
    if (S.phase === 'cmd') inScene += Math.min(elapsed(), s.commands[S.cmd].ms);
    else inScene = s.ms;
    var t = sceneOffset(S.scene) + inScene;
    $('progress-bar').style.width = Math.min(100, t / totalMs * 100).toFixed(1) + '%';
    $('progress-text').textContent = 'Step ' + s.no + ' of ' + SCENES.length + ', ' + mmss(t) + ' / ' + mmss(totalMs);
  }

  function setPlayButton() {
    $('play-icon').setAttribute('d', S.playing ? 'M7 5h3.5v14H7zM13.5 5H17v14h-3.5z' : 'M7 4.5v15l13-7.5z');
    $('play-label').textContent = S.playing ? 'Pause' : (S.started ? (S.phase === 'end' ? 'Replay' : 'Resume') : 'Play');
  }
  function play() {
    if (S.phase === 'end') { done = {}; goScene(0, true); return; }
    if (!S.started) { S.started = true; enterScene(S.scene); }
    else if (S.phase === 'cmd') S.t0 += performance.now() - S.pausedAt;
    else if (S.phase === 'outro') S.outroT += performance.now() - S.pausedAt;
    S.playing = true;
    setPlayButton();
    requestAnimationFrame(tick);
  }
  function pause() {
    S.playing = false;
    S.pausedAt = performance.now();
    setPlayButton();
  }
  function stop(end) {
    S.playing = false;
    S.pausedAt = performance.now();
    if (end) {
      S.phase = 'end';
      $('progress-bar').style.width = '100%';
      $('progress-text').textContent = 'Done: all ' + SCENES.length + ' steps';
    }
    setPlayButton();
  }
  function goScene(i, autoplay) {
    S.started = true;
    if (autoplay) {
      enterScene(i);
      S.playing = true;
      setPlayButton();
      requestAnimationFrame(tick);
    } else {
      S.playing = false;
      renderSceneInstantly(i);
      S.pausedAt = performance.now();
      setPlayButton();
      progress();
    }
  }

  $('btn-play').addEventListener('click', function () { if (S.playing) pause(); else play(); });
  $('btn-next').addEventListener('click', function () { goScene(Math.min(S.scene + 1, SCENES.length - 1), S.playing || !S.started); });
  $('btn-prev').addEventListener('click', function () { goScene(Math.max(S.scene - 1, 0), S.playing); });
  $('btn-restart').addEventListener('click', function () { done = {}; goScene(0, true); });

  /* ---- step 2: build it again in this page -------------------------------------------- */
  $('btn-build-here').addEventListener('click', function () {
    var n = $('build-here-note');
    var t = performance.now();
    var r = E.build(D.build.spec);
    var ms = performance.now() - t;
    var same = 0;
    r.files.forEach(function (f) { if (D.build.files[f.path] === f.content) same++; });
    var all = Object.keys(D.build.files).length;
    n.className = same === all && r.files.length === all ? 'ok' : 'bad';
    n.textContent = same + ' of ' + all + ' files identical to the command line’s, byte for byte. Built in this page in ' + ms.toFixed(1) + ' ms.';
  });

  /* ---- the panel: build your own spec ------------------------------------------------- */
  var SPEC_PRESETS = [
    ['orders-api', 'Python, PostgreSQL, Redis', D.detect.teamSpec],
    ['web-shop', 'Node.js, pnpm, PostgreSQL 17', D.samples['web-shop']],
    ['ingest-worker', 'Go, Redis 8', D.samples['ingest-worker']],
    ['Node.js and Python', 'with pnpm and uv', D.samples['mixed-node-python']],
    ['A spec with mistakes', 'typos and a token', 'version: 1\nname: my app\nruntimes:\n  node: "21"\ntools: [pnmp]\nenv:\n  NPM_TOKEN: abc123\nservices:\n  mysql: "8"\nsetpu:\n  - npm ci\n']
  ];
  var labFiles = {};
  function presetButtons(box, presets, pick) {
    presets.forEach(function (p, i) {
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'preset';
      b.innerHTML = esc(p[0]) + (p[1] ? ' <small>' + esc(p[1]) + '</small>' : '');
      b.addEventListener('click', function () {
        var bs = box.children;
        for (var k = 0; k < bs.length; k++) bs[k].className = 'preset' + (k === i ? ' on' : '');
        pick(p);
      });
      box.appendChild(b);
    });
  }
  function diagItem(d, withFile) {
    var li = document.createElement('li');
    var where = (withFile ? d.file : '') + (d.line ? (withFile ? ':' : 'line ') + d.line + (d.col ? ':' + d.col : '') : '');
    li.innerHTML = '<span class="sev ' + d.severity + '">' + d.severity + '</span>' +
      '<span>' + esc(d.message) + ' <span class="where">' + esc(d.code + (where ? ', ' + where : '')) + '</span></span>' +
      (d.hint ? '<span class="hint">' + esc(d.hint) + '</span>' : '');
    return li;
  }
  var buildTimer = null;
  function labBuild() {
    if (!E) return;
    var text = $('spec-text').value;
    var t = performance.now();
    var r = E.build(text);
    var ms = performance.now() - t;
    var ul = $('spec-diags');
    ul.textContent = '';
    var errors = r.diagnostics.filter(function (d) { return d.severity === 'error'; }).length;
    var warnings = r.diagnostics.filter(function (d) { return d.severity === 'warning'; }).length;
    r.diagnostics.concat(r.notes).forEach(function (d) { ul.appendChild(diagItem(d, d.file !== 'preconfig.yaml')); });
    var st = $('spec-status');
    if (errors) {
      st.className = 'lab-status bad';
      st.textContent = plural(errors, 'error') + (warnings ? ' and ' + plural(warnings, 'warning') : '') + ': build writes nothing until they are fixed.';
      $('lab-viewer').hidden = true;
      return;
    }
    st.className = 'lab-status good';
    st.textContent = (r.summary ? r.name + ': ' + r.summary + '. ' : '') + 'Wrote ' + plural(r.files.length, 'file') + ' in ' + ms.toFixed(1) + ' ms' + (warnings ? ', with ' + plural(warnings, 'warning') : '') + '.';
    if (!r.diagnostics.length && !r.notes.length) {
      var li = document.createElement('li');
      li.className = 'clean';
      li.textContent = 'No findings.';
      ul.appendChild(li);
    }
    labFiles = {};
    var tabs = $('lab-tabs');
    var keep = tabs.querySelector('button.on');
    var keepPath = keep ? keep.getAttribute('data-path') : null;
    tabs.textContent = '';
    r.files.forEach(function (f) { labFiles[f.path] = f.content; addTab(f.path, tabs, $('lab-view'), labFiles); });
    $('lab-viewer').hidden = r.files.length === 0;
    if (r.files.length) showFile(labFiles[keepPath] !== undefined ? keepPath : r.files[0].path, tabs, $('lab-view'), labFiles);
  }
  $('spec-text').addEventListener('input', function () {
    clearTimeout(buildTimer);
    buildTimer = setTimeout(labBuild, 150);
  });

  /* ---- the panel: check a setup file -------------------------------------------------- */
  var CHECK_PRESETS = [
    ['Copilot: a job named setup', 'from step 3', '.github/workflows/copilot-setup-steps.yml', D.check.files['.github/workflows/copilot-setup-steps.yml']],
    ['Cursor: the update key', 'from step 3', '.cursor/environment.json', D.check.files['.cursor/environment.json']],
    ['Cursor: a trailing comma', '', '.cursor/environment.json', '{\n  // Cursor allows comments here, but not trailing commas.\n  "build": {"dockerfile": "Dockerfile", "context": ".."},\n  "install": "bash .preconfig/setup.sh project",\n}\n'],
    ['Dev container without databases', 'from step 3', '.devcontainer/devcontainer.json', D.check.files['.devcontainer/devcontainer.json']],
    ['cloud-init without its first line', '', 'cloud-init.yaml', '# Machine for the orders service\n#cloud-config\npackages:\n  - git\n  - postgresql-16\n'],
    ['A workflow preconfig wrote', 'no findings', '.github/workflows/copilot-setup-steps.yml', D.build.files['.github/workflows/copilot-setup-steps.yml']]
  ];
  function labCheck() {
    if (!E) return;
    var kind = $('check-kind').value;
    var files = {};
    files[kind] = $('check-text').value;
    var withSpec = $('check-with-spec').checked;
    if (withSpec) files['preconfig.yaml'] = $('spec-text').value;
    var r = E.check(files);
    var ul = $('check-out');
    ul.textContent = '';
    var mine = r.findings.filter(function (d) { return d.file === kind || (withSpec && d.file === 'preconfig.yaml' && d.severity === 'error'); });
    mine.forEach(function (d) { ul.appendChild(diagItem(d, d.file !== kind)); });
    if (!mine.length) {
      var li = document.createElement('li');
      li.className = 'clean';
      li.textContent = withSpec ? 'No findings: the file matches the spec and its platform’s format.' : 'No findings against the platform’s format.';
      ul.appendChild(li);
    }
    if (withSpec && !r.hasSpec) ul.appendChild(Object.assign(document.createElement('li'), { className: 'info', textContent: 'The spec above has errors, so the file was checked on its own.' }));
    var diff = r.diffs.filter(function (d) { return d.path === kind; })[0];
    var pre = $('check-diff');
    pre.hidden = !diff;
    if (diff) pre.innerHTML = diffHTML(diff.diff);
  }
  $('btn-check').addEventListener('click', labCheck);

  /* ---- start ---------------------------------------------------------------------------- */
  buildSteps();
  showScene(0);
  markSteps();
  $('progress-text').textContent = 'Press Play, ' + mmss(totalMs);
  Array.prototype.forEach.call(document.querySelectorAll('[data-recorded]'), function (e) { e.textContent = dateText(D.recordedAt); });
  Array.prototype.forEach.call(document.querySelectorAll('[data-verified]'), function (e) { e.textContent = dateText(D.verifyRecordedAt); });
  presetButtons($('spec-presets'), SPEC_PRESETS, function (p) { $('spec-text').value = p[2]; labBuild(); });
  presetButtons($('check-presets'), CHECK_PRESETS, function (p) { $('check-kind').value = p[2]; $('check-text').value = p[3]; labCheck(); });
  $('spec-text').value = SPEC_PRESETS[0][2];
  $('spec-presets').children[0].className = 'preset on';
  $('check-text').value = CHECK_PRESETS[0][3];
  $('check-presets').children[0].className = 'preset on';

  function fatal(msg) {
    ['lab-build', 'lab-check'].forEach(function (id) {
      var p = document.createElement('p');
      p.className = 'fatal';
      p.textContent = msg;
      $(id).insertBefore(p, $(id).children[1]);
    });
  }
  if (!window.WebAssembly || !window.PreconfigEngine) {
    fatal('This browser can’t run WebAssembly, so the two panels are off. The replay above still works.');
    return;
  }
  window.PreconfigEngine.load().then(function (engine) {
    E = engine;
    $('engine-version').textContent = E.version;
    $('engine-knowledge').textContent = E.knowledge;
    $('btn-build-here').disabled = false;
    $('btn-check').disabled = false;
    labBuild();
    labCheck();
    if (S.phase === 'outro' && S.scene === 0) repoFinish();
  }).catch(function (err) {
    fatal('The engine didn’t start in this browser (' + err + '). The replay above still works.');
  });
})();
