package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"preconfiguration.com/preconfig/internal/doctor"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/textdiff"
)

// Exit codes of preconfig doctor.
const (
	doctorNothing   = 0 // the log shows no failure, or --fix applied the change
	doctorExplained = 1 // Doctor found the cause
	doctorUnknown   = 2 // the log shows a failure, and no rule explains it
)

func cmdDoctor(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlags("doctor", stderr)
	dir := fs.String("dir", ".", "the repository's root folder, for its preconfig.yaml")
	file := fs.String("spec", "", "the spec file (default DIR/preconfig.yaml)")
	fix := fs.Bool("fix", false, "apply the change to preconfig.yaml and rebuild the files")
	asJSON := fs.Bool("json", false, "print the diagnosis as JSON")
	if fs.Parse(reorder(args)) != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "preconfig doctor: give one log file, or - for standard input")
		return exitUsage
	}
	var text []byte
	var err error
	if fs.NArg() == 0 || fs.Arg(0) == "-" {
		text, err = io.ReadAll(stdin)
	} else {
		text, err = os.ReadFile(fs.Arg(0))
	}
	if err != nil {
		fmt.Fprintf(stderr, "preconfig doctor: %v\n", err)
		return exitUsage
	}

	specPath := *file
	if specPath == "" {
		specPath = filepath.Join(*dir, kb.SpecPath)
	}
	var s *spec.Spec
	src, specErr := os.ReadFile(specPath)
	if specErr == nil {
		s, _ = spec.Load(kb.SpecPath, string(src))
	}

	d := doctor.Diagnose(string(text), s)
	code := doctorExplained
	switch {
	case d.Outcome == doctor.Passed:
		code = doctorNothing
	case d.Category == doctor.CatUnknown:
		code = doctorUnknown
	}

	var fixed, diff string
	if *fix && d.Fixable() {
		switch {
		case errors.Is(specErr, os.ErrNotExist):
			fmt.Fprintf(stderr, "preconfig doctor: %s doesn't exist, so there is nothing to change\n", specPath)
			return exitSpec
		case specErr != nil:
			fmt.Fprintf(stderr, "preconfig doctor: %v\n", specErr)
			return exitSpec
		}
		out, err := doctor.Apply(string(src), d.Changes)
		if err != nil {
			fmt.Fprintf(stderr, "preconfig doctor: the change wasn't applied: %v\n", err)
			return exitSpec
		}
		if err := os.WriteFile(specPath, []byte(out), 0o644); err != nil {
			fmt.Fprintf(stderr, "preconfig doctor: %v\n", err)
			return exitSpec
		}
		fixed = specPath
		diff = textdiff.Unified("a/"+kb.SpecPath, "b/"+kb.SpecPath, string(src), out, 2)
	}

	if *asJSON {
		v := map[string]any{"tool": "preconfig doctor " + doctor.Version, "diagnosis": d}
		if fixed != "" {
			v["fixed"] = fixed
			v["diff"] = diff
		}
		printJSON(stdout, v)
	} else {
		fmt.Fprint(stdout, doctor.Text(d))
		if d.Fixable() && fixed == "" {
			fmt.Fprintln(stdout, "  run      preconfig doctor --fix with the same log to apply it, then preconfig verify")
		}
		if fixed != "" {
			fmt.Fprintf(stdout, "\nchanged %s:\n%s", fixed, diff)
		}
	}
	if fixed != "" {
		// The spec changed: every platform's file follows.
		build := []string{"--dir", *dir}
		if *file != "" {
			build = append(build, "--spec", *file)
		}
		out := stdout
		if *asJSON {
			out = io.Discard
		} else {
			fmt.Fprintln(stdout)
		}
		if c := cmdBuild(build, out, stderr); c != exitOK {
			return c
		}
		return doctorNothing
	}
	return code
}

// reorder lets flags come after the log file too: "doctor LOG --fix".
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' && a != "-" {
			flags = append(flags, a)
			if (a == "--dir" || a == "-dir" || a == "--spec" || a == "-spec") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}
