#!/usr/bin/env python3
"""Writes the engine file for the demo pages (engine.v2.js): TinyGo's loader
(wasm_exec.js), the engine compiled to WebAssembly as base64 text, and a small
wrapper, so that the pages also work when opened straight from a folder on disk.

    python3 tools/demo/engine.py engine.wasm wasm_exec.js OUT.js VERSION
"""
import base64
import sys

wasm, loader, out, version = sys.argv[1:5]
b64 = base64.b64encode(open(wasm, "rb").read()).decode()
head = f"""/* Preconfiguration.com engine for the demo pages, version {version}: the engine's own Go
 * code (the spec reader, build, check, detect and Doctor), compiled to WebAssembly with TinyGo.
 * Loaded from the text below, so the page also works when opened straight from a folder
 * on disk. Nothing here talks to a server.
 *
 * The loader that follows is TinyGo's wasm_exec.js, under the Go authors' BSD license. */
"""
wrapper = """
(function (global) {
  'use strict';
  var WASM = '%s';

  function bytes(b64) {
    var bin = atob(b64), n = bin.length, out = new Uint8Array(n);
    for (var i = 0; i < n; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  function load() {
    var go = new global.Go();
    var ready = new Promise(function (resolve) { global.onPreconfigReady = resolve; });
    return WebAssembly.instantiate(bytes(WASM), go.importObject).then(function (res) {
      go.run(res.instance);
      return ready;
    }).then(function () {
      var e = global.preconfig;
      if (!e) throw new Error('the engine did not start');
      var v = JSON.parse(e.version());
      return {
        version: v.version,
        knowledge: v.knowledge,
        /* preconfig build: {files, notes, diagnostics, summary, name}. */
        build: function (specText) { return JSON.parse(e.build(String(specText))); },
        /* preconfig check: files maps repository paths to their text. */
        check: function (files) { return JSON.parse(e.check(JSON.stringify(files || {}))); },
        /* preconfig detect: {spec, notes, found}. */
        detect: function (files, folder) { return JSON.parse(e.detect(JSON.stringify(files || {}), String(folder || ''))); },
        /* preconfig doctor: {diagnosis, text}; specText may be empty. */
        doctor: e.doctor ? function (logText, specText) { return JSON.parse(e.doctor(String(logText), String(specText || ''))); } : null,
        /* the changes applied to the spec, and every file rebuilt: {spec, diff, files, notes} or {error}. */
        doctorFix: e.doctorFix ? function (specText, changes) { return JSON.parse(e.doctorFix(String(specText), JSON.stringify(changes || []))); } : null,
        doctorVersion: v.doctor || ''
      };
    });
  }

  global.PreconfigEngine = { load: load };
})(typeof window !== 'undefined' ? window : globalThis);
""" % b64
with open(out, "w") as f:
    f.write(head)
    f.write(open(loader).read())
    f.write(wrapper)
print(f"wrote {out}")
