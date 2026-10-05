package doctor

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Kinds of log Doctor reads.
const (
	KindSetup     = "setup"      // the setup script's own output: verify --log, Cursor's install step, a platform's setup log
	KindVerify    = "verify"     // preconfig verify's terminal output
	KindEvents    = "events"     // preconfig verify --json
	KindDocker    = "docker"     // docker build, BuildKit's plain output or the classic builder's
	KindActions   = "actions"    // a GitHub Actions job log, such as Copilot's setup steps
	KindCloudInit = "cloud-init" // cloud-init's output log
	KindText      = "text"       // anything else: a pasted error, say
)

// Line is one line of a log as Doctor reads it: the text with any docker or
// Actions prefix removed. Lines that Doctor shows are masked first.
type Line struct {
	N    int    `json:"n"`    // its line number in the log as given, from 1
	Text string `json:"text"` // what the program printed
}

// Step is one step of the setup, as the log shows it.
type Step struct {
	Name   string `json:"name"`             // "machine 1/5: system packages"
	Phase  string `json:"phase,omitempty"`  // machine, services, project or ready; "" for a platform's own step
	From   int    `json:"from"`             // the index of its first line in the log's lines
	To     int    `json:"to"`               // the index after its last line
	Failed bool   `json:"failed,omitempty"` // the log says it failed
	Code   int    `json:"exitCode,omitempty"`
	Reason string `json:"reason,omitempty"` // the script's own words, such as "redis 7 is not answering"
}

// Log is a log after reading: its lines and the steps they belong to.
type Log struct {
	Kind   string
	Lines  []Line
	Steps  []Step
	Passed bool // the log says the setup, or the build, finished
	Failed bool // the log says something failed
	// For verify's terminal output and events: what verify concluded.
	VerifyCode int
	Total      int // lines in the log as given
}

var (
	markerRe   = regexp.MustCompile(`^>>> preconfig: (.*)$`)
	failExitRe = regexp.MustCompile(`^FAILED: (.*) \(exit code ([0-9]+)\)$`)
	stepRe     = regexp.MustCompile(`^((machine|services|project|ready) [0-9]+/[0-9]+): (.*)$`)
	buildkitRe = regexp.MustCompile(`^#([0-9]+) ([0-9]+\.[0-9]+) (.*)$`)
	bkOtherRe  = regexp.MustCompile(`^#([0-9]+) (.*)$`)
	classicRe  = regexp.MustCompile(`^Step [0-9]+/[0-9]+ : (.*)$`)
	actionsTS  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z ?`)
	verifyRe   = regexp.MustCompile(`^verify +[0-9]+\.[0-9]s  (\S+) +(.*)$`)
	tailRe     = regexp.MustCompile(`^ {15}\| ?(.*)$`)
	cloudRe    = regexp.MustCompile(`Cloud-init v\. [0-9]`)
)

// Read splits a log into lines and steps. It never fails: a log it doesn't
// recognize is read as plain text.
func Read(text string) *Log {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	raw := strings.Split(text, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	lg := &Log{Total: len(raw)}
	switch kind(raw) {
	case KindEvents:
		readEvents(lg, raw)
	case KindVerify:
		readVerify(lg, raw)
	case KindDocker:
		readDocker(lg, raw)
	case KindActions:
		readActions(lg, raw)
	default:
		lg.Kind = kind(raw)
		for i, l := range raw {
			lg.Lines = append(lg.Lines, Line{N: i + 1, Text: strings.TrimRight(l, " \t")})
		}
		splitSteps(lg)
	}
	for i := range lg.Lines {
		lg.Lines[i].Text = clipLine(lg.Lines[i].Text)
	}
	return lg
}

// maxLine is the longest line Doctor reads; the rest of a longer line, such as
// a progress bar redrawn in place, is dropped.
const maxLine = 4096

func clipLine(s string) string {
	if len(s) <= maxLine {
		return s
	}
	cut := maxLine
	for cut > 0 && s[cut]&0xC0 == 0x80 { // don't split a UTF-8 sequence
		cut--
	}
	return s[:cut]
}

// kind looks at the first lines that say something to tell what wrote the log.
func kind(raw []string) string {
	seen, markers, bk, classic, ts, verify, cloud := 0, 0, 0, 0, 0, 0, 0
	for _, l := range raw {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if seen == 0 && strings.HasPrefix(t, "{") && strings.Contains(t, `"type"`) {
			var e map[string]any
			if json.Unmarshal([]byte(t), &e) == nil {
				if _, ok := e["type"]; ok {
					return KindEvents
				}
			}
		}
		seen++
		switch {
		case markerRe.MatchString(l):
			markers++
		case buildkitRe.MatchString(l) || strings.HasPrefix(l, "#0 building with"):
			bk++
		case classicRe.MatchString(l) || strings.HasPrefix(l, "Sending build context to Docker daemon"):
			classic++
		case actionsTS.MatchString(l):
			ts++
		case verifyRe.MatchString(l):
			verify++
		}
		if cloudRe.MatchString(l) {
			cloud++
		}
		if seen >= 400 {
			break
		}
	}
	switch {
	case verify > 0 && verify >= markers:
		return KindVerify
	case bk > 0 || classic > 0:
		return KindDocker
	case ts > 0 && ts*2 >= seen:
		return KindActions
	case cloud > 0:
		return KindCloudInit
	case markers > 0:
		return KindSetup
	}
	return KindText
}

// splitSteps finds the setup script's markers in lg.Lines and groups the lines
// into steps. The marker lines themselves stay in the lines.
func splitSteps(lg *Log) {
	cur := -1
	byName := map[string]int{} // a step's name to its last index
	lastDone := false          // the last marker said a phase finished
	closeAt := func(i int) {
		if cur >= 0 && lg.Steps[cur].To == 0 {
			lg.Steps[cur].To = i
		}
	}
	for i, l := range lg.Lines {
		m := markerRe.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		msg := m[1]
		lastDone = msg == "ready: passed" || strings.HasPrefix(msg, "machine: done") || msg == "services: done"
		switch {
		case failExitRe.MatchString(msg):
			fm := failExitRe.FindStringSubmatch(msg)
			idx, ok := byName[fm[1]]
			if !ok {
				idx = cur
			}
			if idx < 0 {
				lg.Steps = append(lg.Steps, Step{Name: fm[1], Phase: phaseOf(fm[1]), From: i})
				idx = len(lg.Steps) - 1
				byName[fm[1]] = idx
				cur = idx
			}
			lg.Steps[idx].Failed = true
			lg.Steps[idx].Code, _ = strconv.Atoi(fm[2])
			lg.Steps[idx].To = i + 1
			lg.Failed = true
		case strings.HasPrefix(msg, "FAILED: "):
			if cur < 0 {
				lg.Steps = append(lg.Steps, Step{Name: "start", From: 0})
				cur = len(lg.Steps) - 1
			}
			lg.Steps[cur].Failed = true
			lg.Steps[cur].Reason = strings.TrimPrefix(msg, "FAILED: ")
			if lg.Steps[cur].Code == 0 {
				lg.Steps[cur].Code = 1
			}
			lg.Failed = true
		case stepRe.MatchString(msg):
			sm := stepRe.FindStringSubmatch(msg)
			closeAt(i)
			lg.Steps = append(lg.Steps, Step{Name: sm[1] + ": " + sm[3], Phase: sm[2], From: i})
			cur = len(lg.Steps) - 1
			byName[lg.Steps[cur].Name] = cur
		case msg == "ready: passed":
			closeAt(i)
			lg.Passed = true
		case strings.HasPrefix(msg, "machine: done"), msg == "services: done":
			closeAt(i)
		}
	}
	if cur >= 0 && lg.Steps[cur].To == 0 {
		lg.Steps[cur].To = len(lg.Lines)
	}
	for i := range lg.Steps {
		if lg.Steps[i].To <= lg.Steps[i].From {
			lg.Steps[i].To = lg.Steps[i].From + 1
		}
	}
	// Without a failure, the log passed only if its last marker says a
	// phase finished: a log that stops in the middle of a step was cut off.
	if !lg.Failed && lastDone {
		lg.Passed = true
	}
}

func phaseOf(step string) string {
	if m := stepRe.FindStringSubmatch(step); m != nil {
		return m[2]
	}
	for _, p := range []string{"machine", "services", "project", "ready"} {
		if strings.HasPrefix(step, p) {
			return p
		}
	}
	return ""
}

// readDocker unwraps docker build's output. BuildKit prefixes each line of a
// step with "#N T.TT"; the classic builder prints the output as it is.
func readDocker(lg *Log, raw []string) {
	lg.Kind = KindDocker
	var buildErr []Line
	builtOK := false
	for i, l := range raw {
		n := i + 1
		if m := buildkitRe.FindStringSubmatch(l); m != nil {
			lg.Lines = append(lg.Lines, Line{N: n, Text: strings.TrimRight(m[3], " \t")})
			continue
		}
		if m := bkOtherRe.FindStringSubmatch(l); m != nil {
			rest := m[2]
			if strings.HasPrefix(rest, "ERROR: ") {
				buildErr = append(buildErr, Line{N: n, Text: rest})
				lg.Failed = true
			}
			if strings.HasPrefix(rest, "naming to ") || strings.HasPrefix(rest, "writing image ") {
				builtOK = true
			}
			continue
		}
		t := strings.TrimRight(l, " \t")
		switch {
		case strings.HasPrefix(t, "ERROR: failed to solve:") || strings.HasPrefix(t, "ERROR: failed to build:"):
			buildErr = append(buildErr, Line{N: n, Text: t})
			lg.Failed = true
		case strings.HasPrefix(t, "The command '") && strings.Contains(t, "returned a non-zero code"):
			buildErr = append(buildErr, Line{N: n, Text: t})
			lg.Failed = true
		case strings.HasPrefix(t, "Successfully built ") || strings.HasPrefix(t, "Successfully tagged "):
			builtOK = true
		case classicRe.MatchString(t), strings.HasPrefix(t, " ---> "), strings.HasPrefix(t, "Sending build context"),
			strings.HasPrefix(t, "Removing intermediate container"), strings.HasPrefix(t, "DEPRECATED:"):
		default:
			if t != "" {
				lg.Lines = append(lg.Lines, Line{N: n, Text: t})
			}
		}
	}
	lg.Lines = append(lg.Lines, buildErr...)
	splitSteps(lg)
	if builtOK && len(buildErr) == 0 {
		lg.Passed = true
	}
	if len(buildErr) > 0 {
		lg.Failed = true
		lg.Passed = false
		// A build that failed outside the setup script has no failed step
		// yet: give it one, so the error lines have a home.
		if !anyFailed(lg.Steps) {
			from := len(lg.Lines) - len(buildErr)
			if len(lg.Steps) > 0 && !stepDone(lg, len(lg.Steps)-1) {
				last := &lg.Steps[len(lg.Steps)-1]
				last.Failed, last.To = true, len(lg.Lines)
				last.Code = dockerExit(buildErr)
			} else {
				lg.Steps = append(lg.Steps, Step{Name: "docker build", From: from, To: len(lg.Lines), Failed: true, Code: dockerExit(buildErr)})
			}
		}
	} else if lg.Passed {
		lg.Failed = false
	}
	// splitSteps judged the script's own markers; the build's result decides.
	if len(buildErr) == 0 && !builtOK {
		lg.Passed = false
	}
}

var dockerExitRe = regexp.MustCompile(`(?:exit code: |non-zero code: )([0-9]+)`)

func dockerExit(ls []Line) int {
	for _, l := range ls {
		if m := dockerExitRe.FindStringSubmatch(l.Text); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 1
}

func anyFailed(steps []Step) bool {
	for _, s := range steps {
		if s.Failed {
			return true
		}
	}
	return false
}

// stepDone reports whether the step at i was followed by a marker that closes
// it, such as the next step or "machine: done".
func stepDone(lg *Log, i int) bool {
	st := lg.Steps[i]
	for j := st.From + 1; j < len(lg.Lines); j++ {
		if m := markerRe.FindStringSubmatch(lg.Lines[j].Text); m != nil {
			return true
		}
	}
	return false
}

var (
	groupRe     = regexp.MustCompile(`^##\[group\](.*)$`)
	actionErrRe = regexp.MustCompile(`^##\[error\](.*)$`)
	exitCodeRe  = regexp.MustCompile(`exit code ([0-9]+)`)
)

// readActions reads a GitHub Actions job log. Each "Run ..." group is a step;
// the step with an ##[error] line failed.
func readActions(lg *Log, raw []string) {
	lg.Kind = KindActions
	cur := -1
	for i, l := range raw {
		t := strings.TrimRight(actionsTS.ReplaceAllString(l, ""), " \t")
		idx := len(lg.Lines)
		if m := groupRe.FindStringSubmatch(t); m != nil {
			if cur >= 0 && lg.Steps[cur].To == 0 {
				lg.Steps[cur].To = idx
			}
			lg.Steps = append(lg.Steps, Step{Name: strings.TrimSpace(m[1]), From: idx})
			cur = len(lg.Steps) - 1
			lg.Lines = append(lg.Lines, Line{N: i + 1, Text: t})
			continue
		}
		if t == "##[endgroup]" {
			continue
		}
		if m := actionErrRe.FindStringSubmatch(t); m != nil {
			t = "Error: " + m[1]
			if cur < 0 {
				lg.Steps = append(lg.Steps, Step{Name: "job", From: idx})
				cur = len(lg.Steps) - 1
			}
			lg.Steps[cur].Failed = true
			if c := exitCodeRe.FindStringSubmatch(m[1]); c != nil {
				lg.Steps[cur].Code, _ = strconv.Atoi(c[1])
			} else if lg.Steps[cur].Code == 0 {
				lg.Steps[cur].Code = 1
			}
			lg.Failed = true
		}
		lg.Lines = append(lg.Lines, Line{N: i + 1, Text: t})
	}
	if cur >= 0 && lg.Steps[cur].To == 0 {
		lg.Steps[cur].To = len(lg.Lines)
	}
	// "Process completed with exit code 1." is reported in the group that
	// failed; a later cleanup group isn't the one to blame.
	for i := range lg.Steps {
		if lg.Steps[i].To <= lg.Steps[i].From {
			lg.Steps[i].To = lg.Steps[i].From + 1
		}
	}
	if !lg.Failed && len(lg.Steps) > 0 {
		lg.Passed = true
	}
}

// readVerify reads preconfig verify's terminal output: its step lines and, when
// verify failed, the last lines the machine printed.
func readVerify(lg *Log, raw []string) {
	lg.Kind = KindVerify
	cur := -1
	for i, l := range raw {
		idx := len(lg.Lines)
		if m := tailRe.FindStringSubmatch(l); m != nil {
			lg.Lines = append(lg.Lines, Line{N: i + 1, Text: strings.TrimRight(m[1], " \t")})
			if cur >= 0 {
				lg.Steps[cur].To = len(lg.Lines)
			}
			continue
		}
		m := verifyRe.FindStringSubmatch(l)
		if m == nil {
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "the last lines it printed") {
				lg.Lines = append(lg.Lines, Line{N: i + 1, Text: t})
			}
			continue
		}
		word, rest := m[1], strings.TrimSpace(m[2])
		switch word {
		case "step":
			lg.Steps = append(lg.Steps, Step{Name: rest, Phase: phaseOf(rest), From: idx, To: idx + 1})
			cur = len(lg.Steps) - 1
		case "FAIL":
			name, code := rest, 1
			if j := strings.LastIndex(rest, "  (exit code "); j >= 0 {
				name = rest[:j]
				code, _ = strconv.Atoi(strings.TrimSuffix(rest[j+len("  (exit code "):], ")"))
			} else if j := strings.Index(rest, ": "); j >= 0 && cur >= 0 {
				name = lg.Steps[cur].Name
				lg.Steps[cur].Reason = rest[len(name)+2:]
			}
			for j := len(lg.Steps) - 1; j >= 0; j-- {
				if lg.Steps[j].Name == name {
					cur = j
					break
				}
			}
			if cur >= 0 {
				lg.Steps[cur].Failed, lg.Steps[cur].Code = true, code
			}
			lg.Failed = true
		case "READY":
			lg.Passed = true
		case "NOT":
			lg.Failed = true
			if c := regexp.MustCompile(`\(exit ([0-9]+)\)`).FindStringSubmatch(rest); c != nil {
				lg.VerifyCode, _ = strconv.Atoi(c[1])
			}
		case "ERROR":
			lg.Failed = true
			lg.VerifyCode = 3
		}
		lg.Lines = append(lg.Lines, Line{N: i + 1, Text: strings.TrimSpace(l)})
	}
}

// readEvents reads verify --json. The events name the steps; they carry no
// output from the machine, so Doctor can name the failed step and little more.
func readEvents(lg *Log, raw []string) {
	lg.Kind = KindEvents
	cur := -1
	for i, l := range raw {
		var e struct {
			Type   string         `json:"type"`
			Step   string         `json:"step"`
			Fields map[string]any `json:"fields"`
		}
		if json.Unmarshal([]byte(l), &e) != nil {
			continue
		}
		idx := len(lg.Lines)
		text := e.Type
		if e.Step != "" {
			text += " " + e.Step
		}
		switch e.Type {
		case "step.start":
			lg.Steps = append(lg.Steps, Step{Name: e.Step, Phase: phaseOf(e.Step), From: idx, To: idx + 1})
			cur = len(lg.Steps) - 1
		case "step.fail":
			for j := len(lg.Steps) - 1; j >= 0; j-- {
				if lg.Steps[j].Name == e.Step {
					cur = j
					break
				}
			}
			if cur >= 0 {
				lg.Steps[cur].Failed = true
				lg.Steps[cur].Code = 1
				if c, ok := e.Fields["exit_code"].(float64); ok {
					lg.Steps[cur].Code = int(c)
				}
				if r, ok := e.Fields["reason"].(string); ok {
					lg.Steps[cur].Reason = r
					text += ": " + r
				}
				lg.Steps[cur].To = idx + 1
			}
			lg.Failed = true
		case "warning", "note":
			// Written back as the script's own marker lines, so the rules
			// that read those markers read these too.
			if m, ok := e.Fields["message"].(string); ok {
				text = ">>> preconfig: " + m
				if e.Type == "warning" {
					text = ">>> preconfig: warning: " + m
				}
			}
			if cur >= 0 {
				lg.Steps[cur].To = idx + 1
			}
		case "verify.end":
			code := 0
			if c, ok := e.Fields["code"].(float64); ok {
				code = int(c)
			}
			lg.VerifyCode = code
			if code == 0 {
				lg.Passed = true
			} else {
				lg.Failed = true
			}
			if m, ok := e.Fields["message"].(string); ok {
				text += ": " + m
			}
		}
		lg.Lines = append(lg.Lines, Line{N: i + 1, Text: text})
	}
}
