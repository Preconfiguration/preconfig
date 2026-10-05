#!/usr/bin/env python3
"""Checks the Doctor demo headless, at a desktop and a phone width: every
recorded case gives the same diagnosis in the page as on the command line,
the fix applies and rebuilds the files, the proof shows where there is one,
nothing scrolls sideways and nothing goes to the console as an error.

    python3 tools/doctor/demo-check.py DEMO_DIR PRECONFIG_BIN [SHOTS_DIR]

DEMO_DIR holds index.html, engine.v2.js and doctor-cases.v1.js.
"""
import asyncio
import json
import os
import re
import subprocess
import sys
import tempfile

from playwright.async_api import async_playwright

demo, binary = os.path.abspath(sys.argv[1]), os.path.abspath(sys.argv[2])
shots = sys.argv[3] if len(sys.argv) > 3 else ""


def load_cases():
    src = open(os.path.join(demo, "doctor-cases.v1.js"), encoding="utf-8").read()
    return json.loads(src[src.index("{"):src.rindex("}") + 1])


def cli(case):
    with tempfile.TemporaryDirectory() as t:
        log, spec = os.path.join(t, "log.txt"), os.path.join(t, "preconfig.yaml")
        open(log, "w").write(case["log"])
        open(spec, "w").write(case["spec"])
        p = subprocess.run([binary, "doctor", "--json", "--spec", spec, log], capture_output=True, text=True)
        return json.loads(p.stdout)["diagnosis"]


async def run(width, height, data, failures):
    async with async_playwright() as p:
        b = await p.chromium.launch()
        page = await b.new_page(viewport={"width": width, "height": height})
        errors = []
        page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
        page.on("pageerror", lambda e: errors.append(str(e)))
        await page.goto("file://" + os.path.join(demo, "index.html"))
        await page.wait_for_function("!document.getElementById('btn-diagnose').disabled", timeout=30000)
        presets = await page.query_selector_all("#presets .preset")
        for i, case in enumerate(data["cases"]):
            want = cli(case)
            await presets[i].click()
            await page.wait_for_selector("#dx-card:not([hidden])")
            cause = await page.inner_text("#dx-cause")
            rule = await page.inner_text("#dx-rule")
            changes = await page.eval_on_selector_all("#dx-changes li", "els => els.map(e => e.textContent)")
            ok = True
            if want.get("outcome") == "passed":
                ok = "Nothing to fix" in cause
            else:
                if cause.strip() != want.get("cause", "").strip():
                    ok = False
                if want.get("rule") and want["rule"] not in rule:
                    ok = False
                if len(changes) != len(want.get("changes") or []):
                    ok = False
            fixed = proof = ""
            if want.get("changes"):
                await page.click("#btn-fix")
                await page.wait_for_selector("#fix-out:not([hidden])")
                diff = await page.inner_text("#spec-diff")
                tabs = await page.eval_on_selector_all("#file-tabs button", "els => els.length")
                added = bool(re.search(r"^\+[^+]", diff, re.M))
                fixed = ("diff with an added line" if added else "no added line") + f", {tabs} files"
                if not added or tabs == 0:
                    ok = False
                if case.get("proof"):
                    vis = await page.is_visible("#proof")
                    proof = (await page.inner_text("#proof")).split("\n")[0] if vis else "no proof shown"
                    if not vis:
                        ok = False
            wide = await page.evaluate("document.documentElement.scrollWidth - window.innerWidth")
            if wide > 0:
                ok = False
            if shots:
                os.makedirs(shots, exist_ok=True)
                await page.screenshot(path=os.path.join(shots, f"doctor-{width}-{i + 1}-{case['id']}.png"), full_page=True)
            print(f"  {width:4}px  {case['id']:22} {'ok  ' if ok else 'FAIL'} {want.get('rule') or want.get('outcome'):7} "
                  f"{fixed:22} {proof[:60]}" + (f"  scrolls sideways by {wide}px" if wide > 0 else ""))
            if not ok:
                failures.append(f"{width}px {case['id']}")
        if errors:
            failures.append(f"{width}px console: {errors[:3]}")
            print("  console errors:", errors[:3])
        await b.close()


async def main():
    data = load_cases()
    failures = []
    print(f"{len(data['cases'])} recorded cases")
    for w, h in ((1280, 900), (390, 844)):
        await run(w, h, data, failures)
    print("\n" + ("all checks passed" if not failures else f"{len(failures)} failed: {failures}"))
    sys.exit(1 if failures else 0)


asyncio.run(main())
