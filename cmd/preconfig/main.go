// Command preconfig compiles one preconfig.yaml into the setup files that
// coding agents and machines read, checks the setup files a repository
// already has, drafts a spec from a repository, and verifies a spec on a clean
// machine.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"preconfiguration.com/preconfig/internal/check"
	"preconfiguration.com/preconfig/internal/detect"
	"preconfiguration.com/preconfig/internal/gen"
	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/verify"
)

// Exit codes.
const (
	exitOK       = 0
	exitFindings = 1
	exitSpec     = 4
	exitUsage    = 64
)

const usage = `preconfig: everything a machine needs, settled before the agent starts.

Usage:
  preconfig detect [--dir DIR] [--write]      draft preconfig.yaml from the repository's files
  preconfig build  [--dir DIR] [--dry-run]    write every target from preconfig.yaml
  preconfig check  [--dir DIR] [--diff]       check the setup files against their formats and preconfig.yaml
  preconfig verify [--dir DIR] [flags]        run the setup and the ready check on a clean machine
  preconfig doctor [--dir DIR] [--fix] LOG    explain a failed setup's log, and fix the spec
  preconfig targets                            list the targets and where they are written
  preconfig version

detect, build, check, verify and doctor take --json for machine-readable output.
Exit codes: 0 ok, 1 problems found (check) or not ready (verify), 2 a setup step
failed (verify), 3 verify couldn't start a machine, 4 preconfig.yaml has errors,
64 wrong usage.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "build":
		return cmdBuild(rest, stdout, stderr)
	case "check":
		return cmdCheck(rest, stdout, stderr)
	case "detect":
		return cmdDetect(rest, stdout, stderr)
	case "verify":
		return cmdVerify(rest, stdout, stderr)
	case "doctor":
		return cmdDoctor(rest, os.Stdin, stdout, stderr)
	case "targets":
		return cmdTargets(stdout)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "preconfig %s (knowledge as of %s)\n", kb.Version, kb.Date)
		return exitOK
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	fmt.Fprintf(stderr, "preconfig: unknown command %q\n\n%s", cmd, usage)
	return exitUsage
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("preconfig "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func printJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// loadSpec reads and checks preconfig.yaml in dir.
func loadSpec(dir, file string, stderr io.Writer, quiet bool) (*spec.Spec, []spec.Diagnostic, int) {
	p := file
	if p == "" {
		p = filepath.Join(dir, kb.SpecPath)
	}
	src, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "preconfig: %s doesn't exist. Draft one with: preconfig detect --write\n", p)
		} else {
			fmt.Fprintf(stderr, "preconfig: %v\n", err)
		}
		return nil, nil, exitSpec
	}
	s, ds := spec.Load(kb.SpecPath, string(src))
	if !quiet {
		for _, d := range ds {
			fmt.Fprintln(stderr, d.String())
		}
	}
	if s == nil {
		return nil, ds, exitSpec
	}
	return s, ds, exitOK
}

func cmdBuild(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("build", stderr)
	dir := fs.String("dir", ".", "the repository's root folder")
	file := fs.String("spec", "", "the spec file (default DIR/preconfig.yaml)")
	dry := fs.Bool("dry-run", false, "print what would be written, write nothing")
	asJSON := fs.Bool("json", false, "print the files and notes as JSON")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	s, ds, code := loadSpec(*dir, *file, stderr, *asJSON)
	if s == nil {
		if *asJSON {
			printJSON(stdout, map[string]any{"diagnostics": ds})
		}
		return code
	}
	res := gen.Build(s)
	if *asJSON {
		printJSON(stdout, map[string]any{"files": res.Files, "notes": res.Notes, "diagnostics": ds})
	}
	for _, f := range res.Files {
		p := filepath.Join(*dir, filepath.FromSlash(f.Path))
		if *dry {
			if !*asJSON {
				fmt.Fprintf(stdout, "would write %s (%d bytes)\n", f.Path, len(f.Content))
			}
			continue
		}
		old, err := os.ReadFile(p)
		unchanged := err == nil && string(old) == f.Content
		if !unchanged {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				fmt.Fprintf(stderr, "preconfig: %v\n", err)
				return exitFindings
			}
			mode := os.FileMode(0o644)
			if f.Mode == "0755" {
				mode = 0o755
			}
			if err := os.WriteFile(p, []byte(f.Content), mode); err != nil {
				fmt.Fprintf(stderr, "preconfig: %v\n", err)
				return exitFindings
			}
			if f.Mode == "0755" {
				os.Chmod(p, 0o755)
			}
		}
		if !*asJSON {
			state := "wrote"
			if unchanged {
				state = "unchanged"
			}
			fmt.Fprintf(stdout, "%-9s %s\n", state, f.Path)
		}
	}
	if !*asJSON {
		for _, n := range res.Notes {
			fmt.Fprintln(stdout, n.String())
		}
	}
	return exitOK
}

// readFiles reads the given repository-relative paths that exist.
func readFiles(dir string, paths []string, into map[string]string) {
	for _, p := range paths {
		if _, done := into[p]; done {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err == nil {
			into[p] = string(b)
		}
	}
}

func cmdCheck(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("check", stderr)
	dir := fs.String("dir", ".", "the repository's root folder")
	showDiff := fs.Bool("diff", false, "show how each out-of-date file differs from a fresh build")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	files := map[string]string{}
	readFiles(*dir, check.KnownPaths, files)
	readFiles(*dir, check.Referenced(files), files)
	exists := func(p string) bool {
		_, err := os.Stat(filepath.Join(*dir, filepath.FromSlash(p)))
		return err == nil
	}
	rep := check.Check(check.Input{Files: files, Exists: exists})
	if *asJSON {
		printJSON(stdout, rep)
	} else {
		if len(rep.Checked) == 0 {
			fmt.Fprintf(stdout, "No setup files and no preconfig.yaml in %s. Draft a spec with: preconfig detect --write\n", *dir)
			return exitOK
		}
		for _, d := range rep.Findings {
			fmt.Fprintln(stdout, d.String())
		}
		if *showDiff {
			for _, d := range rep.Diffs {
				fmt.Fprintln(stdout)
				fmt.Fprint(stdout, d.Diff)
			}
		}
		fmt.Fprintf(stdout, "checked %s: %s, %s\n", plural(len(rep.Checked), "file"), plural(rep.Errors, "error"), plural(rep.Warnings, "warning"))
		if len(rep.Diffs) > 0 && !*showDiff {
			fmt.Fprintln(stdout, "Run with --diff to see the changes, or preconfig build to apply them.")
		}
	}
	if rep.Errors > 0 {
		for _, d := range rep.Findings {
			if d.File == kb.SpecPath && d.Severity == spec.Error {
				return exitSpec
			}
		}
		return exitFindings
	}
	return exitOK
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func cmdDetect(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("detect", stderr)
	dir := fs.String("dir", ".", "the repository's root folder")
	write := fs.Bool("write", false, "write the draft to preconfig.yaml")
	force := fs.Bool("force", false, "with --write, replace an existing preconfig.yaml")
	asJSON := fs.Bool("json", false, "print the draft and notes as JSON")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	files := map[string]string{}
	readFiles(*dir, detect.Paths, files)
	abs, _ := filepath.Abs(*dir)
	res := detect.Detect(files, filepath.Base(abs))
	if *asJSON {
		printJSON(stdout, res)
	}
	if *write {
		p := filepath.Join(*dir, kb.SpecPath)
		if _, err := os.Stat(p); err == nil && !*force {
			fmt.Fprintf(stderr, "preconfig: %s already exists; use --force to replace it\n", p)
			return exitFindings
		}
		if err := os.WriteFile(p, []byte(res.Spec), 0o644); err != nil {
			fmt.Fprintf(stderr, "preconfig: %v\n", err)
			return exitFindings
		}
		if !*asJSON {
			fmt.Fprintf(stdout, "wrote %s from %s. Read it before you build from it.\n", kb.SpecPath, strings.Join(res.Found, ", "))
		}
		return exitOK
	}
	if !*asJSON {
		fmt.Fprint(stdout, res.Spec)
	}
	return exitOK
}

func cmdVerify(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("verify", stderr)
	dir := fs.String("dir", ".", "the repository's root folder")
	file := fs.String("spec", "", "the spec file (default DIR/preconfig.yaml)")
	image := fs.String("image", "", "the clean image to start from (default: the spec's base, ubuntu:24.04)")
	network := fs.String("network", "", "the docker network for the machine, such as host")
	ca := fs.String("ca-file", "", "a CA bundle the machine should trust, for networks that inspect TLS")
	passEnv := fs.String("pass-env", "", "comma-separated variables to pass into the machine, such as secrets")
	timeout := fs.Duration("timeout", 30*time.Minute, "give up after this long")
	logFile := fs.String("log", "", "write everything the machine printed to this file")
	asJSON := fs.Bool("json", false, "print events as JSON Lines")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	s, _, code := loadSpec(*dir, *file, stderr, false)
	if s == nil {
		return code
	}
	opts := verify.Options{Dir: *dir, Image: *image, Network: *network, CAFile: *ca, Timeout: *timeout}
	if *passEnv != "" {
		for _, v := range strings.Split(*passEnv, ",") {
			if v = strings.TrimSpace(v); v != "" {
				opts.PassEnv = append(opts.PassEnv, v)
			}
		}
	}
	// Secrets named in the spec are passed when this environment has them.
	opts.PassEnv = append(opts.PassEnv, s.Secrets...)
	if *logFile != "" {
		f, err := os.Create(*logFile)
		if err != nil {
			fmt.Fprintf(stderr, "preconfig: %v\n", err)
			return exitUsage
		}
		defer f.Close()
		opts.Log = f
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	opts.Events = func(e verify.Event) {
		if *asJSON {
			enc.Encode(e)
			return
		}
		printEvent(stdout, e)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res := verify.Run(ctx, s, opts)
	if !*asJSON && !res.Ready && len(res.Tail) > 0 {
		fmt.Fprintln(stdout, "               the last lines it printed:")
		for _, l := range res.Tail {
			fmt.Fprintf(stdout, "               | %s\n", l)
		}
	}
	return res.Code
}

func printEvent(w io.Writer, e verify.Event) {
	if line := verify.Text(e); line != "" {
		fmt.Fprintln(w, line)
	}
}

func cmdTargets(stdout io.Writer) int {
	fmt.Fprintf(stdout, "Targets (knowledge as of %s):\n", kb.Date)
	rows := [][2]string{
		{"devcontainer", kb.DevcontainerPath + ", and " + kb.ComposePath + " when there are services"},
		{"copilot", kb.CopilotWorkflowPath},
		{"cursor", kb.CursorEnvPath + " and " + kb.CursorDocker},
		{"cloud-init", kb.CloudInitPath},
		{"script", kb.ScriptPath},
	}
	for _, r := range rows {
		fmt.Fprintf(stdout, "  %-13s %s\n", r[0], r[1])
	}
	return exitOK
}
