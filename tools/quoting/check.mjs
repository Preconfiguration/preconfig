// The same check with the yaml package (1.2 and 1.1 modes) and js-yaml.
import fs from "fs";
import YAML from "yaml";
import jsyaml from "js-yaml";

const m = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const first = (d) => { while (d && typeof d === "object") d = Array.isArray(d) ? d[0] : Object.values(d)[0]; return d; };
const readers = [
  ["yaml 1.2", (d) => YAML.parse(d)],
  ["yaml 1.1", (d) => YAML.parse(d, { version: "1.1" })],
  ["js-yaml", (d) => jsyaml.load(d)],
];
const bad = Object.fromEntries(readers.map(([n]) => [n, []]));
for (const [s, out] of Object.entries(m)) {
  for (const ctx of ["k: X\n", "- X\n", "k:\n  - X\n", "a:\n  b: X\n"]) {
    const doc = ctx.replace("X", () => out);
    for (const [name, load] of readers) {
      try { const v = first(load(doc)); if (v !== s) bad[name].push([s, out, String(v)]); }
      catch (e) { bad[name].push([s, out, "error: " + e.message.split("\n")[0]]); }
    }
  }
}
let failed = false;
for (const [k, v] of Object.entries(bad)) { console.log(`${k}: ${v.length} failures`, JSON.stringify(v.slice(0, 5))); failed ||= v.length > 0; }
process.exit(failed ? 1 : 0);
