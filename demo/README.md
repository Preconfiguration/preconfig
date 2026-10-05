# The live demo

`app/` is the live demo that preconfiguration.com/demo embeds. Open
`app/index.html` in a browser, from a web server or straight from this folder:
it needs no network.

- `index.html`, `demo.v1.css`, `demo.v1.js`: the page, its style and its code.
- `recordings.v1.js`: what the command line printed on the sample repositories,
  written by `python3 tools/demo/record.py`, and the two verify runs recorded
  with `tools/demo/record-verify.sh` (testdata/verify).
- `engine.v1.js`: the engine (spec, build, check and detect) compiled to
  WebAssembly with TinyGo, as text, with TinyGo's loader. `tools/build.sh`
  writes it.
- `THIRD-PARTY-LICENSES.txt`: the licenses of the Go and TinyGo code inside
  `engine.v1.js`.
