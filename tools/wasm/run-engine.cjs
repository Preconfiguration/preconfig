// Runs one browser build of the engine under Node.js on the corpus and writes
// every answer, with timings:
//   node run-engine.cjs engine.wasm wasm_exec.js corpus.json out.json
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

function median(xs) {
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.floor(s.length / 2)];
}

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
  const out = { version: pc.version(), build: {}, check: {}, detect: {} };
  for (const [name, src] of Object.entries(corpus.build)) out.build[name] = pc.build(src);
  for (const [name, files] of Object.entries(corpus.check)) out.check[name] = pc.check(JSON.stringify(files));
  for (const [name, files] of Object.entries(corpus.detect)) out.detect[name] = pc.detect(JSON.stringify(files), name);
  // Timings on the orders-api sample.
  const time = (fn) => { const xs = []; for (let i = 0; i < 5; i++) fn(); for (let i = 0; i < 50; i++) { const s = performance.now(); fn(); xs.push(performance.now() - s); } return median(xs); };
  const spec = corpus.build["orders-api"];
  const files = JSON.stringify(corpus.check["orders-api"]);
  const dfiles = JSON.stringify(corpus.detect["orders-api"]);
  out.timings = {
    startMs,
    buildMs: time(() => pc.build(spec)),
    checkMs: time(() => pc.check(files)),
    detectMs: time(() => pc.detect(dfiles, "orders-api")),
  };
  fs.writeFileSync(outPath, JSON.stringify(out));
  process.exit(0);
})().catch((e) => { console.error(e); process.exit(1); });
