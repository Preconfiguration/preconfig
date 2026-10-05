// Package verify proves a spec on a clean machine: it runs the generated setup
// script and the ready check in a fresh container, and reports each step with
// its time, and the step that broke when one does.
package verify

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"preconfiguration.com/preconfig/internal/gen"
	"preconfiguration.com/preconfig/internal/spec"
)

// Exit codes of a verify run.
const (
	Ready        = 0 // the setup worked and the ready check passed
	NotReady     = 1 // the setup worked, the ready check failed
	SetupFailed  = 2 // a setup step failed
	CouldNotRun  = 3 // verify couldn't start a clean machine
	SpecProblems = 4 // preconfig.yaml has errors
)

// Options for a run.
type Options struct {
	Dir     string        // the repository on this machine
	Image   string        // the clean image; the spec's base when empty
	Network string        // docker --network, when set
	CAFile  string        // a CA bundle to trust inside the machine (TLS-inspecting proxies)
	PassEnv []string      // variables passed through from this environment, such as secrets
	Timeout time.Duration // 0 means 30 minutes
	Docker  string        // the docker command; "docker" when empty
	Log     io.Writer     // everything the machine printed, when set
	Events  func(Event)   // called for every event, in order
	Now     func() time.Time
}

// Event is one line of verify's record, printed as text or JSON Lines.
type Event struct {
	V      int            `json:"v"`
	TS     string         `json:"ts"`
	T      float64        `json:"t"`
	Type   string         `json:"type"`
	Step   string         `json:"step,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// Step is a finished step.
type Step struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	OK      bool    `json:"ok"`
}

// Result of a run.
type Result struct {
	Code       int      `json:"code"`
	Ready      bool     `json:"ready"`
	FailedStep string   `json:"failedStep,omitempty"`
	FailedCode int      `json:"failedCode,omitempty"`
	Message    string   `json:"message,omitempty"`
	Versions   string   `json:"versions,omitempty"`
	Steps      []Step   `json:"steps"`
	Seconds    float64  `json:"seconds"`
	Tail       []string `json:"tail,omitempty"`
}

var (
	marker     = regexp.MustCompile(`^>>> preconfig: (.*)$`)
	failedExit = regexp.MustCompile(`^FAILED: (.*) \(exit code ([0-9]+)\)$`)
	stepStart  = regexp.MustCompile(`^((machine|services|project|ready) [0-9]+/[0-9]+): (.*)$`)
	doneRe     = regexp.MustCompile(`^machine: done \((.*)\)$`)
)

// wrapper copies the repository into the machine, as a checkout would put it
// there, and runs the setup script's steps followed by the ready check.
const wrapper = `set -e
if [ -f /etc/preconfig/ca.crt ]; then
  mkdir -p /etc/apt/apt.conf.d
  echo 'Acquire::https::CAInfo "/etc/preconfig/ca.crt";' > /etc/apt/apt.conf.d/99preconfig-ca
fi
mkdir -p /work
cd /src
tar --exclude=./.git --exclude=./node_modules --exclude=./.venv --exclude=./venv -cf - . | tar -C /work -xf -
cd /work
exec bash /preconfig/setup.sh all ready 2>&1
`

type runner struct {
	opts   Options
	start  time.Time
	mu     sync.Mutex
	res    Result
	cur    string
	curAt  float64
	tail   []string
	passed bool
	last   string // the last line the ready check printed
}

func (r *runner) now() time.Time {
	if r.opts.Now != nil {
		return r.opts.Now()
	}
	return time.Now()
}

func (r *runner) emit(typ, step string, fields map[string]any) {
	if r.opts.Events == nil {
		return
	}
	t := r.now()
	r.opts.Events(Event{V: 1, TS: t.UTC().Format(time.RFC3339Nano), T: round(t.Sub(r.start).Seconds()), Type: typ, Step: step, Fields: fields})
}

func round(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// Run verifies s in a clean container.
func Run(ctx context.Context, s *spec.Spec, opts Options) Result {
	r := &runner{opts: opts}
	r.start = r.now()
	if opts.Docker == "" {
		opts.Docker = "docker"
		r.opts.Docker = "docker"
	}
	image := opts.Image
	if image == "" {
		image = s.Base.DockerImage
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	r.emit("verify.start", "", map[string]any{"image": image, "name": s.Name, "spec": gen.Summary(s)})

	fail := func(code int, msg string) Result {
		r.res.Code, r.res.Message = code, msg
		r.res.Seconds = round(r.now().Sub(r.start).Seconds())
		r.emit("verify.end", "", map[string]any{"code": code, "message": msg})
		return r.res
	}
	if _, err := exec.LookPath(opts.Docker); err != nil {
		return fail(CouldNotRun, "docker isn't installed, and verify needs it to start a clean machine")
	}
	if out, err := exec.CommandContext(ctx, opts.Docker, "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		return fail(CouldNotRun, "docker is installed but its daemon isn't answering: "+firstLine(string(out)))
	}
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return fail(CouldNotRun, err.Error())
	}
	tmp, err := os.MkdirTemp("", "preconfig-verify-")
	if err != nil {
		return fail(CouldNotRun, err.Error())
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "setup.sh"), []byte(gen.ScriptText(s)), 0o755); err != nil {
		return fail(CouldNotRun, err.Error())
	}
	name := "preconfig-verify-" + randomHex(4)
	args := []string{"run", "--rm", "-i", "--name", name}
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}
	args = append(args, "-v", dir+":/src:ro", "-v", tmp+":/preconfig:ro")
	if opts.CAFile != "" {
		ca, err := filepath.Abs(opts.CAFile)
		if err != nil {
			return fail(CouldNotRun, err.Error())
		}
		if _, err := os.Stat(ca); err != nil {
			return fail(CouldNotRun, "the CA file can't be read: "+err.Error())
		}
		args = append(args, "-v", ca+":/etc/preconfig/ca.crt:ro")
		for _, v := range []string{"SSL_CERT_FILE", "PIP_CERT", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
			args = append(args, "-e", v+"=/etc/preconfig/ca.crt")
		}
	}
	for _, v := range opts.PassEnv {
		if _, ok := os.LookupEnv(v); ok {
			args = append(args, "-e", v)
		}
	}
	args = append(args, image, "bash", "-c", wrapper)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(opts.Docker, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fail(CouldNotRun, err.Error())
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fail(CouldNotRun, err.Error())
	}
	done := make(chan struct{})
	timedOut := false
	go func() {
		select {
		case <-runCtx.Done():
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) || ctx.Err() != nil {
				r.mu.Lock()
				timedOut = true
				r.mu.Unlock()
				exec.Command(opts.Docker, "kill", name).Run()
			}
		case <-done:
		}
	}()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		r.line(sc.Text())
	}
	waitErr := cmd.Wait()
	close(done)
	code := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	elapsed := round(r.now().Sub(r.start).Seconds())
	switch {
	case timedOut:
		r.closeStep(false)
		r.res.FailedStep = r.cur
		r.res.Code = SetupFailed
		if strings.HasPrefix(r.cur, "ready") {
			r.res.Code = NotReady
		}
		r.res.Message = fmt.Sprintf("stopped after %s without finishing", timeout)
	case code == 0 && r.passed:
		r.closeStep(true)
		r.res.Ready = true
		r.res.Code = Ready
		msg := "ready"
		if r.last != "" {
			msg = r.last
		}
		r.res.Message = msg
	case (code == 125 || code == 126 || code == 127) && r.cur == "":
		r.res.Code = CouldNotRun
		r.res.Message = "docker couldn't start the machine: " + firstLine(strings.Join(r.tail, "\n"))
	default:
		if r.res.FailedStep == "" {
			r.closeStep(false)
			r.res.FailedStep = r.cur
			r.res.FailedCode = code
		}
		if strings.HasPrefix(r.res.FailedStep, "ready") {
			r.res.Code = NotReady
			r.res.Message = "the setup worked, but the ready check failed"
		} else {
			r.res.Code = SetupFailed
			r.res.Message = "a setup step failed"
		}
	}
	if !r.res.Ready {
		r.res.Tail = tail(r.tail)
	}
	r.res.Seconds = elapsed
	fields := map[string]any{"code": r.res.Code, "message": r.res.Message, "steps": len(r.res.Steps)}
	if r.res.Versions != "" {
		fields["versions"] = r.res.Versions
	}
	if r.res.FailedStep != "" {
		fields["failed_step"] = r.res.FailedStep
	}
	r.emitLocked("verify.end", "", fields)
	return r.res
}

func (r *runner) emitLocked(typ, step string, fields map[string]any) {
	r.emit(typ, step, fields)
}

// line handles one line of the machine's output.
func (r *runner) line(l string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.opts.Log != nil {
		fmt.Fprintln(r.opts.Log, l)
	}
	m := marker.FindStringSubmatch(l)
	if m == nil {
		r.tail = append(r.tail, l)
		if len(r.tail) > 200 {
			r.tail = r.tail[len(r.tail)-200:]
		}
		if strings.HasPrefix(r.cur, "ready") && strings.TrimSpace(l) != "" {
			r.last = strings.TrimSpace(l)
		}
		return
	}
	msg := m[1]
	switch {
	case failedExit.MatchString(msg):
		fm := failedExit.FindStringSubmatch(msg)
		r.closeStep(false)
		r.res.FailedStep = fm[1]
		r.res.FailedCode, _ = strconv.Atoi(fm[2])
		r.emit("step.fail", fm[1], map[string]any{"exit_code": r.res.FailedCode})
	case strings.HasPrefix(msg, "FAILED: "):
		r.closeStep(false)
		r.res.FailedStep = r.cur
		r.res.FailedCode = 1
		r.tail = append(r.tail, strings.TrimPrefix(msg, "FAILED: "))
		r.emit("step.fail", r.cur, map[string]any{"reason": strings.TrimPrefix(msg, "FAILED: ")})
	case strings.HasPrefix(msg, "warning: "):
		r.emit("warning", r.cur, map[string]any{"message": strings.TrimPrefix(msg, "warning: ")})
	case stepStart.MatchString(msg):
		sm := stepStart.FindStringSubmatch(msg)
		r.closeStep(true)
		r.cur = sm[1] + ": " + sm[3]
		r.curAt = r.now().Sub(r.start).Seconds()
		r.tail = nil
		r.emit("step.start", r.cur, nil)
	case doneRe.MatchString(msg):
		r.closeStep(true)
		r.res.Versions = doneRe.FindStringSubmatch(msg)[1]
		r.emit("machine.ready", "", map[string]any{"versions": r.res.Versions})
	case msg == "services: done":
		r.closeStep(true)
	case msg == "ready: passed":
		r.closeStep(true)
		r.passed = true
	default:
		r.emit("note", r.cur, map[string]any{"message": msg})
	}
}

// closeStep ends the current step, if one is open.
func (r *runner) closeStep(ok bool) {
	if r.cur == "" {
		return
	}
	secs := round(r.now().Sub(r.start).Seconds() - r.curAt)
	r.res.Steps = append(r.res.Steps, Step{Name: r.cur, Seconds: secs, OK: ok})
	typ := "step.end"
	r.emit(typ, r.cur, map[string]any{"ok": ok, "seconds": secs})
	if ok {
		r.cur = ""
	}
}

// tail picks the lines worth showing from a failed step: the error lines
// that test runners mark with "E ", then the last lines it printed.
func tail(lines []string) []string {
	var errs []string
	seen := map[string]bool{}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "E ") && !seen[t] {
			seen[t] = true
			errs = append(errs, l)
		}
	}
	if len(errs) > 3 {
		errs = errs[len(errs)-3:]
	}
	last := lastN(lines, 6)
	out := append([]string{}, errs...)
	for _, l := range last {
		if !seen[strings.TrimSpace(l)] {
			out = append(out, l)
		}
	}
	return out
}

func lastN(lines []string, n int) []string {
	// Drop trailing blank lines first.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
