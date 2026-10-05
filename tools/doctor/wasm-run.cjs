// Runs one browser build of the engine under Node.js on every log that
// tools/doctor/wasm-native.py collected, compares each answer with the command
// line's, and times Doctor:
//   node wasm-run.cjs engine.wasm wasm_exec.js corpus.json out.json
const fs = require("fs");
const { performance } = require("perf_hooks");
const [wasmPath, execPath, corpusPath, outPath] = process.argv.slice(2);

globalThis.require = require;
globalThis.fs = globalThis.fs || fs;
globalThis.path = require("path");
globalThis.TextEncoder = globalThis.TextEncoder || require("util").TextEncoder;
globalThis.TextDecoder = globalThis.TextDecoder || require("util").TextDecoder;
globalThis.performance = globalThis.performance || performance;
globalThis.crypto = globalThis.crypto || require("crypto").webcrypto;
require(require("path").resolve(execPath));

const median = (xs) => { const s = [...xs].sort((a, b) => a - b); return s[Math.floor(s.length / 2)]; };

// Go writes a nil list as null, and the browser build as []; both mean none.
function canon(v, key) {
  if (v === null && (key === "evidence" || key === "changes")) return [];
  if (Array.isArray(v)) return v.map((x) => canon(x));
  if (v && typeof v === "object") {
    const o = {};
    for (const k of Object.keys(v).sort()) o[k] = canon(v[k], k);
    return o;
  }
  return v;
}
const same = (a, b) => JSON.stringify(canon(a)) === JSON.stringify(canon(b));

(async () => {
  const bytes = fs.readFileSync(wasmPath);
  const t0 = performance.now();
  const go = new Go();
  const ready = new Promise((resolve) => { globalThis.onPreconfigReady = resolve; });
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(instance);
  await ready;
  const startMs = performance.now() - t0;
  const pc = globalThis.preconfig;
  const corpus = JSON.parse(fs.readFileSync(corpusPath, "utf8"));
  const out = { version: JSON.parse(pc.version()), logs: corpus.length, diagnoses: 0, fixes: 0, files: 0, differ: [], answers: {} };
  const perLog = [];
  for (const c of corpus) {
    const s = performance.now();
    const r = JSON.parse(pc.doctor(c.log, c.spec));
    perLog.push(performance.now() - s);
    out.diagnoses++;
    const ans = { diagnosis: r.diagnosis, text: r.text };
    if (!same(r.diagnosis, c.native.diagnosis)) out.differ.push(`${c.key}: diagnosis`);
    const nf = c.native.fix;
    const changes = r.diagnosis.changes || [];
    if (changes.length) {
      const f = JSON.parse(pc.doctorFix(c.spec, JSON.stringify(changes)));
      ans.fix = { spec: f.spec, diff: f.diff, error: f.error, files: (f.files || []).map((x) => x.path) };
      if (!nf) {
        out.differ.push(`${c.key}: the browser build wrote a change, the command line didn't`);
      } else if (f.error) {
        out.differ.push(`${c.key}: the browser build couldn't apply the change: ${f.error}`);
      } else {
        out.fixes++;
        if (f.spec !== nf.spec) out.differ.push(`${c.key}: fixed spec`);
        if (f.diff !== nf.diff) out.differ.push(`${c.key}: diff`);
        const disk = Object.keys(nf.files).sort();
        const mine = f.files.map((x) => x.path).sort();
        if (JSON.stringify(disk) !== JSON.stringify(mine)) out.differ.push(`${c.key}: the files written differ: ${mine} vs ${disk}`);
        for (const x of f.files) {
          out.files++;
          if (nf.files[x.path] !== x.content) out.differ.push(`${c.key}: ${x.path}`);
        }
      }
    } else if (nf) {
      out.differ.push(`${c.key}: the command line wrote a change, the browser build didn't`);
    }
    out.answers[c.key] = ans;
  }
  // Timings: the Alpha's 1,096-line log without Redis, and every log once.
  const alpha = corpus.find((c) => c.key === "alpha/fail.log");
  const alphaChanges = JSON.stringify(JSON.parse(pc.doctor(alpha.log, alpha.spec)).diagnosis.changes || []);
  const time = (fn) => { const xs = []; for (let i = 0; i < 5; i++) fn(); for (let i = 0; i < 50; i++) { const s = performance.now(); fn(); xs.push(performance.now() - s); } return median(xs); };
  out.timings = {
    startMs,
    alphaLines: alpha.log.split("\n").length - (alpha.log.endsWith("\n") ? 1 : 0),
    doctorAlphaMs: time(() => pc.doctor(alpha.log, alpha.spec)),
    fixAlphaMs: time(() => pc.doctorFix(alpha.spec, alphaChanges)),
    firstPassMedianMs: median(perLog),
    firstPassMaxMs: Math.max(...perLog),
  };
  fs.writeFileSync(outPath, JSON.stringify(out));
  const t = out.timings;
  console.log(`${out.logs} logs: ${out.diagnoses} diagnoses and ${out.fixes} fixes compared with the command line's, ` +
    `${out.files} rebuilt files; ${out.differ.length} differ`);
  for (const d of out.differ) console.log("  differs: " + d);
  console.log(`start ${t.startMs.toFixed(1)} ms; the Alpha's ${t.alphaLines}-line log: doctor ${t.doctorAlphaMs.toFixed(2)} ms, ` +
    `fix and rebuild ${t.fixAlphaMs.toFixed(2)} ms (medians of 50); every log once: median ${t.firstPassMedianMs.toFixed(2)} ms, ` +
    `slowest ${t.firstPassMaxMs.toFixed(2)} ms`);
  process.exit(out.differ.length ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
