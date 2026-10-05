// Compares the answers of the two browser builds, and the files they write
// for the sample repositories with the files on disk, which the command-line
// engine wrote:
//   node compare.cjs tiny.json std.json repo-root
const fs = require("fs");
const path = require("path");
const [aPath, bPath, root] = process.argv.slice(2);
const a = JSON.parse(fs.readFileSync(aPath, "utf8"));
const b = JSON.parse(fs.readFileSync(bPath, "utf8"));
let answers = 0, differ = [];
for (const kind of ["build", "check", "detect"]) {
  for (const name of Object.keys(a[kind])) {
    answers++;
    if (a[kind][name] !== b[kind][name]) differ.push(`${kind} ${name}`);
  }
}
console.log(`${answers} answers compared; ${differ.length} differ between the two builds`, differ);
let files = 0, mismatched = [];
for (const name of ["orders-api", "web-shop", "ingest-worker"]) {
  const res = JSON.parse(a.build[name]);
  for (const f of res.files) {
    files++;
    const disk = fs.readFileSync(path.join(root, "testdata", "repos", name, f.path), "utf8");
    if (disk !== f.content) mismatched.push(`${name}/${f.path}`);
  }
}
console.log(`${files} generated files compared with the command-line engine's; ${mismatched.length} differ`, mismatched);
for (const [label, r] of [["TinyGo", a], ["Go", b]]) {
  const t = r.timings;
  console.log(`${label}: start ${t.startMs.toFixed(1)} ms; build ${t.buildMs.toFixed(2)} ms, check ${t.checkMs.toFixed(2)} ms, detect ${t.detectMs.toFixed(2)} ms (medians of 50)`);
}
process.exit(differ.length || mismatched.length ? 1 : 0);
