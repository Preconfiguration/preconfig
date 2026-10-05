//go:build js && wasm

// Command wasm is the browser build of the engine. It exposes build, check,
// detect and doctor on globalThis.preconfig. Every call takes and returns JSON
// text, and nothing leaves the page.
package main

import (
	"encoding/json"
	"syscall/js"

	"preconfiguration.com/preconfig/internal/check"
	"preconfiguration.com/preconfig/internal/detect"
	"preconfiguration.com/preconfig/internal/doctor"
	"preconfiguration.com/preconfig/internal/gen"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/textdiff"
)

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":` + jsonString(err.Error()) + `}`
	}
	return string(b)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func arg(args []js.Value, i int) string {
	if i < len(args) && args[i].Type() == js.TypeString {
		return args[i].String()
	}
	return ""
}

// build(specText) -> {"files":[...], "notes":[...], "diagnostics":[...]}
func build(this js.Value, args []js.Value) any {
	s, ds := spec.Load(kb.SpecPath, arg(args, 0))
	if ds == nil {
		ds = []spec.Diagnostic{}
	}
	if s == nil {
		return toJSON(map[string]any{"files": []gen.File{}, "notes": []spec.Diagnostic{}, "diagnostics": ds})
	}
	res := gen.Build(s)
	if res.Notes == nil {
		res.Notes = []spec.Diagnostic{}
	}
	return toJSON(map[string]any{"files": res.Files, "notes": res.Notes, "diagnostics": ds, "summary": gen.Summary(s), "name": s.Name})
}

// check(filesJSON) -> report; filesJSON maps paths to contents.
func checkFiles(this js.Value, args []js.Value) any {
	files := map[string]string{}
	if err := json.Unmarshal([]byte(arg(args, 0)), &files); err != nil {
		return toJSON(map[string]string{"error": "the files could not be read: " + err.Error()})
	}
	rep := check.Check(check.Input{Files: files})
	if rep.Findings == nil {
		rep.Findings = []spec.Diagnostic{}
	}
	if rep.Diffs == nil {
		rep.Diffs = []check.FileDiff{}
	}
	return toJSON(rep)
}

// detect(filesJSON, folderName) -> {"spec": "...", "notes": [...], "found": [...]}
func detectFiles(this js.Value, args []js.Value) any {
	files := map[string]string{}
	if err := json.Unmarshal([]byte(arg(args, 0)), &files); err != nil {
		return toJSON(map[string]string{"error": "the files could not be read: " + err.Error()})
	}
	res := detect.Detect(files, arg(args, 1))
	if res.Notes == nil {
		res.Notes = []string{}
	}
	return toJSON(res)
}

// doctor(logText, specText) -> {"diagnosis": {...}, "text": "..."}; specText may be "".
func doctorLog(this js.Value, args []js.Value) any {
	var s *spec.Spec
	if src := arg(args, 1); src != "" {
		s, _ = spec.Load(kb.SpecPath, src)
	}
	d := doctor.Diagnose(arg(args, 0), s)
	if d.Evidence == nil {
		d.Evidence = []doctor.Line{}
	}
	if d.Changes == nil {
		d.Changes = []doctor.Change{}
	}
	return toJSON(map[string]any{"diagnosis": d, "text": doctor.Text(d)})
}

// doctorFix(specText, changesJSON) -> {"spec": "...", "diff": "...", "files": [...]}
// or {"error": "..."}: the changes applied to the spec, and every file rebuilt.
func doctorFix(this js.Value, args []js.Value) any {
	var changes []doctor.Change
	if err := json.Unmarshal([]byte(arg(args, 1)), &changes); err != nil {
		return toJSON(map[string]string{"error": "the changes could not be read: " + err.Error()})
	}
	src := arg(args, 0)
	out, err := doctor.Apply(src, changes)
	if err != nil {
		return toJSON(map[string]string{"error": err.Error()})
	}
	s, _ := spec.Load(kb.SpecPath, out)
	res := gen.Build(s)
	if res.Notes == nil {
		res.Notes = []spec.Diagnostic{}
	}
	diff := textdiff.Unified("a/"+kb.SpecPath, "b/"+kb.SpecPath, src, out, 2)
	return toJSON(map[string]any{"spec": out, "diff": diff, "files": res.Files, "notes": res.Notes})
}

func version(this js.Value, args []js.Value) any {
	return toJSON(map[string]string{"version": kb.Version, "knowledge": kb.Date, "doctor": doctor.Version})
}

func main() {
	api := js.Global().Get("Object").New()
	api.Set("build", js.FuncOf(build))
	api.Set("check", js.FuncOf(checkFiles))
	api.Set("detect", js.FuncOf(detectFiles))
	api.Set("doctor", js.FuncOf(doctorLog))
	api.Set("doctorFix", js.FuncOf(doctorFix))
	api.Set("version", js.FuncOf(version))
	js.Global().Set("preconfig", api)
	if cb := js.Global().Get("onPreconfigReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {}
}
