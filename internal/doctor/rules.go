package doctor

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
)

// Categories of cause. Only "spec" is fixed by changing preconfig.yaml.
const (
	CatSpec       = "spec"       // preconfig.yaml is missing something, or has it wrong
	CatNetwork    = "network"    // the machine couldn't reach a download source
	CatRepository = "repository" // a file in the repository is wrong: a pin, a lockfile, an include
	CatSecret     = "secret"     // a secret isn't set where the setup runs
	CatCode       = "code"       // the setup worked; the tests failed on the code
	CatPlatform   = "platform"   // the machine itself: a port taken, a full disk, apt busy
	CatUnknown    = "unknown"    // no rule knows this error
)

// finding is what a rule concludes.
type finding struct {
	rule     string
	category string
	cause    string
	advice   string
	changes  []Change
	evidence []int // indexes into ctx.lines
}

// ctx is what a rule looks at: the lines of the failed step, and the spec
// when Doctor has it.
type ctx struct {
	lines []Line
	step  *Step
	spec  *spec.Spec
	log   *Log
	noise []bool // noise[i]: lines[i] is noise
}

func newCtx(lines []Line, st *Step, s *spec.Spec, lg *Log) *ctx {
	c := &ctx{lines: lines, step: st, spec: s, log: lg, noise: make([]bool, len(lines))}
	for i, l := range lines {
		c.noise[i] = isNoise(l.Text)
	}
	return c
}

// find returns the index and submatches of the first line that matches re,
// skipping noise; -1 when none does.
func (c *ctx) find(re *regexp.Regexp) (int, []string) {
	for i, l := range c.lines {
		if c.noise[i] {
			continue
		}
		if m := re.FindStringSubmatch(l.Text); m != nil {
			return i, m
		}
	}
	return -1, nil
}

// findLast is find from the end.
func (c *ctx) findLast(re *regexp.Regexp) (int, []string) {
	for i := len(c.lines) - 1; i >= 0; i-- {
		if c.noise[i] {
			continue
		}
		if m := re.FindStringSubmatch(c.lines[i].Text); m != nil {
			return i, m
		}
	}
	return -1, nil
}

func (c *ctx) phase() string {
	if c.step == nil {
		return ""
	}
	return c.step.Phase
}

// noise is output that looks alarming and never is the cause.
var noiseRes = []*regexp.Regexp{
	regexp.MustCompile(`^debconf: `),
	regexp.MustCompile(`invoke-rc\.d: `),
	regexp.MustCompile(`^WARNING: apt does not have a stable CLI interface`),
	regexp.MustCompile(`\[notice\] A new release of pip`),
	regexp.MustCompile(`\[notice\] To update, run:`),
	regexp.MustCompile(`^npm WARN deprecated`),
	regexp.MustCompile(`^update-alternatives: warning`),
	regexp.MustCompile(`^dpkg: warning: `),
	regexp.MustCompile(`^perl: warning: `),
	regexp.MustCompile(`^\s+(LANGUAGE|LC_ALL|LANG) = `),
	regexp.MustCompile(`^\s*are supported and installed on your system\.`),
	regexp.MustCompile(`^No schema files found: doing nothing\.`),
	regexp.MustCompile(`^Processing triggers for `),
	regexp.MustCompile(`note: This error originates from a subprocess, and is likely not a problem with pip`),
	regexp.MustCompile(`^hint: See above for details\.`),
	regexp.MustCompile(`WARNING: Running pip as the 'root' user`),
}

func isNoise(s string) bool {
	for _, re := range noiseRes {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// rules in the order they are tried: the most specific causes first, so that
// a missing package isn't blamed when the network hid it, and a test failure
// is blamed on the code only when nothing about the setup explains it.
var rules = []struct {
	id string
	fn func(*ctx) *finding
}{
	{"D002", ruleSpecErrors},
	{"D003", ruleNoDocker},
	{"D105", ruleAptBusy},
	{"D106", ruleDiskFull},
	{"D107", ruleNotRoot},
	{"D102", ruleNetwork},
	{"D101", ruleAptUnknown},
	{"D109", ruleImageMissing},
	{"D110", ruleActionsVersion},
	{"D201", rulePortTaken},
	{"D307", rulePythonVersion},
	{"D312", ruleNodeEngine},
	{"D315", ruleGoVersion},
	{"D302", ruleCompiler},
	{"D303", ruleHeader},
	{"D301", rulePgConfig},
	{"D314", ruleRegistryAuth},
	{"D309", ruleRequirementsFile},
	{"D310", ruleHashes},
	{"D311", ruleStaleLock},
	{"D308", rulePipNoVersion},
	{"D306", ruleCommandMissing},
	{"D305", ruleSharedLibrary},
	{"D405", ruleLocale},
	{"D402", rulePgAuth},
	{"D403", rulePgNoDatabase},
	{"D401", ruleServiceRefused},
	{"D404", ruleMissingEnv},
	{"D103", ruleVersionCheck},
	{"D202", ruleServiceDown},
	{"D408", ruleTestsFailed},
}

// ---- rules ----

var specErrRe = regexp.MustCompile(`^preconfig\.yaml(?::[0-9]+(?::[0-9]+)?)?: error (S[0-9]+): (.*)$`)

func ruleSpecErrors(c *ctx) *finding {
	var ev []int
	for i, l := range c.lines {
		if specErrRe.MatchString(l.Text) {
			ev = append(ev, i)
		}
	}
	if len(ev) == 0 {
		return nil
	}
	return &finding{category: CatSpec, evidence: ev,
		cause:  "preconfig.yaml has errors, so nothing ran.",
		advice: "Fix the errors listed; preconfig check shows each one with its line and a hint."}
}

var noDockerRe = regexp.MustCompile(`docker isn't installed|daemon isn't answering|docker couldn't start the machine|Cannot connect to the Docker daemon`)

func ruleNoDocker(c *ctx) *finding {
	i, _ := c.find(noDockerRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatPlatform, evidence: []int{i},
		cause:  "Docker couldn't start the clean machine, so the setup never ran.",
		advice: "Start Docker, or install it, and run verify again."}
}

var aptBusyRe = regexp.MustCompile(`Could not get lock /var/lib/(dpkg|apt)/|dpkg was interrupted|Unable to acquire the dpkg frontend lock`)

func ruleAptBusy(c *ctx) *finding {
	i, _ := c.find(aptBusyRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatPlatform, evidence: []int{i},
		cause:  "Another program was using apt when the setup ran, often a fresh server's automatic updates.",
		advice: "Run the setup again once the other apt run has finished. On a new server, cloud-init can wait for it."}
}

var diskFullRe = regexp.MustCompile(`No space left on device`)

func ruleDiskFull(c *ctx) *finding {
	i, _ := c.find(diskFullRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatPlatform, evidence: []int{i},
		cause:  "The machine ran out of disk space.",
		advice: "Give the machine more disk, or clear caches before the setup."}
}

var notRootRe = regexp.MustCompile(`run this script as root, or install sudo`)

func ruleNotRoot(c *ctx) *finding {
	i, _ := c.find(notRootRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatPlatform, evidence: []int{i},
		cause:  "The setup ran as a user without root and without sudo, so it couldn't install anything.",
		advice: "Run the machine step as root, or install sudo for that user."}
}

// ---- the network ----

var (
	netLineRes = []*regexp.Regexp{
		regexp.MustCompile(`curl: \((5|6|7|28|35|52|56|60)\) (.*)`),
		regexp.MustCompile(`curl: \(22\) The requested URL returned error: (403|407|451)`),
		regexp.MustCompile(`Could not resolve host:? '?([A-Za-z0-9.-]+)`),
		regexp.MustCompile(`Temporary failure resolving '([^']+)'`),
		regexp.MustCompile(`(?:W|E): Failed to fetch https?://([^/\s]+)\S*\s+(.*)`),
		regexp.MustCompile(`Err:[0-9]+ https?://([^/\s]+)\S*.*`),
		regexp.MustCompile(`Could not fetch URL https?://([^/\s]+)/`),
		regexp.MustCompile(`HTTPSConnectionPool\(host='([^']+)'`),
		regexp.MustCompile(`Failed to establish a new connection`),
		regexp.MustCompile(`(ENOTFOUND|ETIMEDOUT|EAI_AGAIN|ECONNRESET|UNABLE_TO_GET_ISSUER_CERT_LOCALLY|SELF_SIGNED_CERT_IN_CHAIN)\b.*?(?:https?://([^/\s]+))?`),
		regexp.MustCompile(`ERR_PNPM_META_FETCH_FAIL.*https?://([^/\s]+)`),
		regexp.MustCompile(`Get "https?://([^/"]+)[^"]*": (.*)`),
		regexp.MustCompile(`dial tcp: lookup ([A-Za-z0-9.-]+)`),
		// The go command, when a proxy or firewall refuses a module or toolchain.
		regexp.MustCompile(`reading https?://([^/\s]+)\S*: (403|407|451) `),
		regexp.MustCompile(`toolchain not available`),
		regexp.MustCompile(`error sending request for url \(https?://([^/)\s]+)`),
		regexp.MustCompile(`status client error \((40[37]) [A-Za-z ]+\) for url \(https?://([^/)\s]+)`),
		regexp.MustCompile(`Request failed after [0-9]+ retr`),
		regexp.MustCompile(`(?i)certificate verify failed|SSL certificate problem|x509: certificate signed by unknown authority`),
		regexp.MustCompile(`(?i)Network is unreachable|Name or service not known|No route to host`),
	}
	urlHostRe = regexp.MustCompile(`https?://([A-Za-z0-9.-]+\.[A-Za-z]{2,})`)
	// netWords is a quick first look: a line without any of these words can't
	// match a network pattern.
	netWords = regexp.MustCompile(`(?i)curl|resolv|fetch|Err:|connect|ENOTFOUND|ETIMEDOUT|EAI_AGAIN|ECONNRESET|CERT|Get "|dial tcp|toolchain|request|retr|certificate|SSL|x509|unreachable|service not known|route to host|reading http`)
	tlsRe     = regexp.MustCompile(`(?i)certificate verify failed|SSL certificate problem|x509: certificate|UNABLE_TO_GET_ISSUER_CERT_LOCALLY|SELF_SIGNED_CERT_IN_CHAIN|SSL_connect`)
	localRe   = regexp.MustCompile(`localhost|127\.0\.0\.1|\[?::1\]?`)
)

// sourceOfStep is where a step of the setup script downloads from, for logs
// whose error doesn't name the host.
func sourceOfStep(c *ctx) string {
	if c.step == nil {
		return ""
	}
	n := c.step.Name
	title := n
	if i := strings.Index(n, ": "); i >= 0 {
		title = n[i+2:]
	}
	f := strings.Fields(title)
	if len(f) == 0 {
		return ""
	}
	base := kb.Bases[kb.DefaultBase]
	switch f[0] {
	case "node":
		return "deb.nodesource.com"
	case "go":
		return "proxy.golang.org"
	case "python":
		if len(f) > 1 && f[1] != base.DistroPython {
			return "github.com"
		}
	case "postgres":
		if len(f) > 1 && f[1] != base.DistroPostgres {
			return "www.postgresql.org"
		}
	case "redis":
		if len(f) > 1 && f[1] != base.DistroRedis {
			return "packages.redis.io"
		}
	}
	return ""
}

func ruleNetwork(c *ctx) *finding {
	var ev []int
	host := ""
	tls := false
	for i, l := range c.lines {
		if c.noise[i] || !netWords.MatchString(l.Text) {
			continue
		}
		for _, re := range netLineRes {
			m := re.FindStringSubmatch(l.Text)
			if m == nil {
				continue
			}
			// A refused connection to this machine is a service, not the network.
			if localRe.MatchString(l.Text) && !strings.Contains(l.Text, "Failed to fetch") {
				continue
			}
			ev = append(ev, i)
			if tlsRe.MatchString(l.Text) {
				tls = true
			}
			if host == "" {
				host = hostIn(l.Text)
			}
			break
		}
	}
	if len(ev) == 0 {
		return nil
	}
	// A pip or apt line that only says "retrying" is weaker evidence than the
	// error that ended the step; keep the first three.
	if len(ev) > 3 {
		ev = append(ev[:2], ev[len(ev)-1])
	}
	if host == "" {
		host = sourceOfStep(c)
	}
	f := &finding{category: CatNetwork, evidence: ev}
	name, what := hostName(host)
	switch {
	case tls:
		f.cause = "The machine's network inspects TLS, and the machine doesn't trust the certificate it presents"
		if host != "" {
			f.cause += " for " + host
		}
		f.cause += "."
		f.advice = "Give the machine your network's CA bundle: preconfig verify --ca-file, or add it to the image. Nothing in preconfig.yaml is wrong."
	case host != "":
		f.cause = fmt.Sprintf("The machine couldn't reach %s", name)
		if name != host {
			f.cause += " (" + host + ")"
		}
		if what != "" {
			f.cause += fmt.Sprintf(", where this step downloads %s", what)
		}
		f.cause += "."
		f.advice = fmt.Sprintf("Nothing in preconfig.yaml is wrong: the network blocks the source. Allow %s from the machine, or point the setup at a mirror your network allows.", host)
		if alt := distroAlternative(c); alt != "" {
			f.advice += " " + alt
		}
	default:
		f.cause = "The machine couldn't reach a download source."
		f.advice = "Nothing in preconfig.yaml is wrong: the network blocks a source this step needs. Allow it from the machine, or use a mirror."
	}
	return f
}

// distroAlternative says which version comes from Ubuntu's own archive, when
// the step asked for another one.
func distroAlternative(c *ctx) string {
	if c.step == nil {
		return ""
	}
	base := kb.Bases[kb.DefaultBase]
	title := c.step.Name
	if i := strings.Index(title, ": "); i >= 0 {
		title = title[i+2:]
	}
	f := strings.Fields(title)
	if len(f) < 2 {
		return ""
	}
	switch f[0] {
	case "python":
		if f[1] != base.DistroPython {
			return fmt.Sprintf("If the project can use Python %s, it comes from Ubuntu's own archive.", base.DistroPython)
		}
	case "postgres":
		if f[1] != base.DistroPostgres {
			return fmt.Sprintf("If the project can use PostgreSQL %s, it comes from Ubuntu's own archive.", base.DistroPostgres)
		}
	case "redis":
		if f[1] != base.DistroRedis {
			return fmt.Sprintf("If the project can use Redis %s, it comes from Ubuntu's own archive.", base.DistroRedis)
		}
	}
	return ""
}

func hostIn(s string) string {
	for _, re := range netLineRes {
		m := re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		for _, g := range m[1:] {
			if strings.Contains(g, ".") && !strings.Contains(g, " ") && !strings.Contains(g, "/") && len(g) < 100 {
				if _, err := strconv.Atoi(strings.ReplaceAll(g, ".", "")); err == nil {
					continue // an IP address or a number
				}
				return strings.Trim(g, `'".,:`)
			}
		}
	}
	if m := urlHostRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// ---- apt ----

var aptUnknownRe = regexp.MustCompile(`^E: (?:Unable to locate package |Package '|Couldn't find any package by (?:glob|regex) ')([^\s']+)`)

func ruleAptUnknown(c *ctx) *finding {
	i, m := c.find(aptUnknownRe)
	if i < 0 {
		return nil
	}
	pkg := m[1]
	if len(pkg) > 100 {
		return nil // no package name is this long; the line is something else
	}
	f := &finding{evidence: []int{i}}
	inSpec := c.spec == nil || contains(c.spec.Packages, pkg)
	if !inSpec {
		f.category = CatPlatform
		f.cause = fmt.Sprintf("The setup asked apt for %s, and Ubuntu 24.04's archive has no package by that name.", pkg)
		f.advice = "preconfig.yaml doesn't list it, so the name came from the setup script itself; its knowledge may be out of date for this archive."
		return f
	}
	f.category = CatSpec
	f.cause = fmt.Sprintf("Ubuntu 24.04's archive has no package named %s.", pkg)
	if sug := tree.Suggest(pkg, packageNames()); sug != "" && sug != pkg {
		f.changes = []Change{{Op: "replace-package", Old: pkg, Value: sug}}
		f.cause = fmt.Sprintf("Ubuntu 24.04's archive has no package named %s; %s is the likely name.", pkg, sug)
		return f
	}
	f.advice = fmt.Sprintf("Correct %s under packages in preconfig.yaml, or remove it. apt-cache search on an Ubuntu 24.04 machine finds the right name.", pkg)
	return f
}

var imageMissingRe = regexp.MustCompile(`(?:manifest for|pull access denied for|manifest unknown.*?|failed to resolve source metadata for )\s*([A-Za-z0-9./:_-]+)`)

func ruleImageMissing(c *ctx) *finding {
	i, m := c.find(imageMissingRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatPlatform, evidence: []int{i},
		cause:  fmt.Sprintf("The container image %s couldn't be pulled: it doesn't exist, or the registry refused it.", strings.TrimSpace(m[1])),
		advice: "Check that the registry is reachable from the machine and that the image tag exists."}
}

var actionsVersionRe = regexp.MustCompile(`(?:The version '([^']+)' with architecture '[^']+' was not found|Unable to find (?:Node|Go) version '([^']+)'|Version ([0-9][0-9.]*) was not found in the local cache)`)

func ruleActionsVersion(c *ctx) *finding {
	i, m := c.find(actionsVersionRe)
	if i < 0 {
		return nil
	}
	v := firstNonEmpty(m[1:]...)
	return &finding{category: CatPlatform, evidence: []int{i},
		cause:  fmt.Sprintf("GitHub's setup action has no build of version %s for this runner.", v),
		advice: "Pick a version the action publishes for Ubuntu 24.04, then change it under runtimes in preconfig.yaml."}
}

// ---- services ----

var portTakenRe = regexp.MustCompile(`(?:could not bind .* socket: Address already in use|Address already in use|another postmaster already running on port ([0-9]+)|bind: Address already in use|Could not create server TCP listening socket .*:([0-9]+))`)
var portNumRe = regexp.MustCompile(`port ([0-9]{2,5})|:([0-9]{2,5})\b`)

func rulePortTaken(c *ctx) *finding {
	i, _ := c.find(portTakenRe)
	if i < 0 {
		return nil
	}
	ev := []int{i}
	port := ""
	for j, l := range c.lines {
		if strings.Contains(l.Text, "already running on port") || strings.Contains(l.Text, "Address already in use") {
			if m := portNumRe.FindStringSubmatch(l.Text); m != nil && port == "" {
				port = firstNonEmpty(m[1:]...)
				if j != i {
					ev = append(ev, j)
				}
			}
		}
	}
	svc := "The service"
	if c.step != nil && c.step.Phase == "services" {
		title := c.step.Name
		if k := strings.Index(title, ": "); k >= 0 {
			title = title[k+2:]
		}
		svc = serviceTitle(title)
	}
	cause := svc + " couldn't start: another program already listens on its port"
	if port != "" {
		cause += " " + port
	}
	return &finding{category: CatPlatform, evidence: uniq(ev),
		cause:  cause + ".",
		advice: "Stop the other program, or run the setup on a clean machine. On a shared host network, two setups at once share their ports."}
}

func serviceTitle(t string) string {
	f := strings.Fields(t)
	if len(f) == 0 {
		return "The service"
	}
	switch f[0] {
	case "postgres":
		return "PostgreSQL"
	case "redis":
		return "Redis"
	}
	return f[0]
}

var serviceFailRe = regexp.MustCompile(`^(postgres|redis) (\S+) is not (accepting connections|answering)`)

func ruleServiceDown(c *ctx) *finding {
	if c.step == nil || c.step.Reason == "" {
		return nil
	}
	m := serviceFailRe.FindStringSubmatch(c.step.Reason)
	if m == nil {
		return nil
	}
	ev := []int{}
	for i, l := range c.lines {
		if strings.Contains(l.Text, "FATAL") || strings.Contains(l.Text, "ERROR") || strings.Contains(l.Text, "FAILED") {
			ev = append(ev, i)
		}
	}
	return &finding{category: CatPlatform, evidence: ev,
		cause:  fmt.Sprintf("%s %s was installed but didn't start.", serviceTitle(m[1]), m[2]),
		advice: "The lines above are the service's own reasons. A machine without systemd starts it directly; check that nothing else holds its port."}
}

// ---- runtime versions ----

var (
	pyNeedsRe   = regexp.MustCompile(`requires a different Python: ([0-9.]+) not in '([^']+)'`)
	pyIgnoredRe = regexp.MustCompile(`Requires-Python ([<>=!~][^\s;,]*(?:,\s*[<>=!~][^\s;,]*)*)`)
	uvPyRe      = regexp.MustCompile(`(?:requested Python version|requires-python)[^0-9<>=]*([<>=!~][^\s` + "`" + `'"]+)`)
)

func rulePythonVersion(c *ctx) *finding {
	i, m := c.find(pyNeedsRe)
	specifier := ""
	if i >= 0 {
		specifier = m[2]
	} else if j, m2 := c.find(pyIgnoredRe); j >= 0 {
		if k, _ := c.find(regexp.MustCompile(`No matching distribution found|Could not find a version that satisfies`)); k >= 0 {
			i, specifier = j, m2[1]
		}
	}
	if i < 0 {
		return nil
	}
	want := pickVersion(specifier, kb.PythonMinors, "3.")
	f := &finding{category: CatSpec, evidence: []int{i}}
	have := ""
	if m != nil {
		have = m[1]
	}
	f.cause = fmt.Sprintf("A package needs Python %s", specifier)
	if have != "" {
		f.cause += fmt.Sprintf(", and the machine has %s", have)
	}
	f.cause += "."
	if want != "" {
		f.changes = []Change{{Op: "set-runtime", Key: "python", Value: want}}
	} else {
		f.advice = "No Python version that preconfig installs satisfies " + specifier + "."
	}
	return f
}

// pickVersion returns the lowest version in minors (as prefix+minor) that
// satisfies a specifier such as ">=3.13" or "<3.12,>=3.9".
func pickVersion(specifier string, minors []int, prefix string) string {
	ok := func(v float64) bool {
		for _, part := range strings.Split(specifier, ",") {
			part = strings.TrimSpace(part)
			op := strings.TrimRight(part, "0123456789.*")
			for len(op) > 0 && !strings.ContainsAny(op[len(op)-1:], "<>=!~") {
				op = op[:len(op)-1]
			}
			num := strings.TrimSpace(part[len(op):])
			num = strings.TrimSuffix(num, ".*")
			n := versionNum(num, prefix)
			switch strings.TrimSpace(op) {
			case ">=":
				if v < n {
					return false
				}
			case ">":
				if v <= n {
					return false
				}
			case "<":
				if v >= n {
					return false
				}
			case "<=":
				if v > n {
					return false
				}
			case "==", "~=":
				if v != n && strings.TrimSpace(op) == "==" {
					return false
				}
				if strings.TrimSpace(op) == "~=" && v < n {
					return false
				}
			case "!=":
				if v == n {
					return false
				}
			}
		}
		return true
	}
	sorted := append([]int{}, minors...)
	sort.Ints(sorted)
	for _, mn := range sorted {
		v := versionNum(prefix+strconv.Itoa(mn), prefix)
		if ok(v) {
			return prefix + strconv.Itoa(mn)
		}
	}
	return ""
}

// versionNum turns "3.12" or "3.12.1" into 3012.001 for comparing; for node,
// "20" is 20.
func versionNum(s, prefix string) float64 {
	p := strings.Split(s, ".")
	nums := []float64{}
	for _, x := range p {
		n, err := strconv.Atoi(x)
		if err != nil {
			break
		}
		nums = append(nums, float64(n))
	}
	switch len(nums) {
	case 0:
		return 0
	case 1:
		return nums[0] * 1000
	case 2:
		return nums[0]*1000 + nums[1]
	}
	return nums[0]*1000 + nums[1] + nums[2]/1000
}

var nodeEngineRe = regexp.MustCompile(`(?:ERR_PNPM_UNSUPPORTED_ENGINE|EBADENGINE|Unsupported engine|The engine "node" is incompatible)`)
var nodeWantRe = regexp.MustCompile(`(?:Expected version: |Required: \{"node":"|Expected version "|required: \{ node: ')([^"'\s}]+)`)

func ruleNodeEngine(c *ctx) *finding {
	i, _ := c.find(nodeEngineRe)
	if i < 0 {
		return nil
	}
	ev := []int{i}
	want := ""
	if j, m := c.find(nodeWantRe); j >= 0 {
		ev = append(ev, j)
		want = m[1]
	}
	f := &finding{category: CatSpec, evidence: uniq(ev), cause: "A package needs another Node.js version than the machine has."}
	if want != "" {
		f.cause = fmt.Sprintf("A package needs Node.js %s.", want)
		if v := pickVersion(want, kb.NodeMajors, ""); v != "" {
			f.changes = []Change{{Op: "set-runtime", Key: "node", Value: strings.TrimSuffix(v, ".0")}}
		}
	}
	if len(f.changes) == 0 {
		f.advice = "Set runtimes.node in preconfig.yaml to a version the packages accept."
	}
	return f
}

var goNeedsRe = regexp.MustCompile(`go\.mod requires go >= ([0-9]+\.[0-9]+)`)

func ruleGoVersion(c *ctx) *finding {
	i, m := c.find(goNeedsRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatSpec, evidence: []int{i},
		cause:   fmt.Sprintf("go.mod needs Go %s or later.", m[1]),
		changes: []Change{{Op: "set-runtime", Key: "go", Value: m[1]}}}
}

// ---- builds ----

var compilerRes = []*regexp.Regexp{
	regexp.MustCompile(`error: \[Errno 2\] No such file or directory: '((?:[a-z0-9_]+-linux-gnu-)?(?:gcc|cc|g\+\+|c\+\+|clang))'`),
	regexp.MustCompile(`error: command '(?:[^']*/)?((?:[a-z0-9_]+-linux-gnu-)?(?:gcc|cc|g\+\+|c\+\+))' failed(?: with exit (?:code|status) [0-9]+)?: No such file or directory`),
	regexp.MustCompile(`unable to execute '((?:[a-z0-9_]+-linux-gnu-)?(?:gcc|cc|g\+\+))': No such file or directory`),
	regexp.MustCompile(`(?:^|\s|/)(gcc|cc|g\+\+|c\+\+|make)(?:: command)?: not found`),
	regexp.MustCompile(`gyp ERR! stack Error: not found: (make)`),
	regexp.MustCompile(`(?:C|c) compiler cannot create executables|no acceptable C compiler found`),
	regexp.MustCompile(`linker ` + "`" + `(cc)` + "`" + ` not found`),
}

func ruleCompiler(c *ctx) *finding {
	for _, re := range compilerRes {
		if i, _ := c.find(re); i >= 0 {
			if c.spec != nil && contains(c.spec.Packages, "build-essential") {
				return nil
			}
			return &finding{category: CatSpec, evidence: []int{i},
				cause:   "A package is built from source, and the machine has no C compiler.",
				changes: []Change{{Op: "add-package", Value: "build-essential"}}}
		}
	}
	return nil
}

var headerRe = regexp.MustCompile(`fatal error: ([A-Za-z0-9_./+-]+\.h): No such file or directory`)

func ruleHeader(c *ctx) *finding {
	i, m := c.find(headerRe)
	if i < 0 {
		return nil
	}
	h := m[1]
	f := &finding{category: CatSpec, evidence: []int{i}}
	if pkg, ok := headerPackages[h]; ok {
		if c.spec != nil && contains(c.spec.Packages, pkg) {
			f.cause = fmt.Sprintf("The build can't find %s, though %s is in packages.", h, pkg)
			f.category = CatUnknown
			return f
		}
		f.cause = fmt.Sprintf("A package is built from source and needs %s, from %s.", h, pkg)
		f.changes = []Change{{Op: "add-package", Value: pkg}}
		return f
	}
	f.cause = fmt.Sprintf("A package is built from source and needs the header %s.", h)
	f.advice = "Doctor doesn't know which Ubuntu package provides it: apt-file search " + h + " finds it; add that package under packages."
	return f
}

var pgConfigRe = regexp.MustCompile(`pg_config executable not found|pg_config: (?:command )?not found|Please add the directory containing pg_config`)

func rulePgConfig(c *ctx) *finding {
	i, _ := c.find(pgConfigRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatSpec, evidence: []int{i},
		cause:   "A PostgreSQL client library is built from source and needs pg_config, from libpq-dev.",
		changes: []Change{{Op: "add-package", Value: "libpq-dev"}}}
}

// ---- package indexes and lockfiles ----

var authRe = regexp.MustCompile(`(?:401 Client Error: Unauthorized for url: https?://([^/\s]+)|code E401|ERR_PNPM_FETCH_401|401 Unauthorized.*https?://([^/\s]+)|authentication required.*https?://([^/\s]+))`)

func ruleRegistryAuth(c *ctx) *finding {
	i, m := c.find(authRe)
	if i < 0 {
		return nil
	}
	host := firstNonEmpty(m[1:]...)
	f := &finding{category: CatSecret, evidence: []int{i},
		cause:  "A package index asked for credentials, and the machine has none.",
		advice: "Keep the index's token as a secret: name it under secrets in preconfig.yaml and set it in each platform's secret settings."}
	if host != "" {
		f.cause = fmt.Sprintf("The package index at %s asked for credentials, and the machine has none.", host)
	}
	return f
}

var reqFileRe = regexp.MustCompile(`Could not open requirements file: \[Errno 2\] No such file or directory: '([^']+)'`)

func ruleRequirementsFile(c *ctx) *finding {
	i, m := c.find(reqFileRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatRepository, evidence: []int{i},
		cause:  fmt.Sprintf("The setup installs from %s, which isn't in the repository.", m[1]),
		advice: fmt.Sprintf("Add %s to the repository, or remove the line that includes it.", m[1])}
}

var hashRe = regexp.MustCompile(`THESE PACKAGES DO NOT MATCH THE HASHES|Hashes are required in --require-hashes mode`)

func ruleHashes(c *ctx) *finding {
	i, _ := c.find(hashRe)
	if i < 0 {
		return nil
	}
	return &finding{category: CatRepository, evidence: []int{i},
		cause:  "The requirements file's hashes don't match the packages; the file is out of date.",
		advice: "Regenerate the hashes in the repository, for example with pip-compile --generate-hashes, and commit the file."}
}

var staleLockRe = regexp.MustCompile("(?:The lockfile at `?([^` ]+)`? needs to be updated|ERR_PNPM_OUTDATED_LOCKFILE|can only install packages when your package\\.json and package-lock\\.json .*are in sync|Your lockfile needs to be updated, but yarn was run with|pyproject\\.toml changed significantly since poetry\\.lock was last generated|go: updates to go\\.mod needed)")

func ruleStaleLock(c *ctx) *finding {
	i, m := c.find(staleLockRe)
	if i < 0 {
		return nil
	}
	lock := "The lockfile"
	if m[1] != "" {
		lock = m[1]
	}
	return &finding{category: CatRepository, evidence: []int{i},
		cause:  fmt.Sprintf("%s is out of date with the project's dependencies, and the setup installs strictly from it.", lock),
		advice: "Update the lockfile in the repository (uv lock, pnpm install, npm install, poetry lock or go mod tidy) and commit it. The strict install is right to refuse."}
}

var (
	noDistRe    = regexp.MustCompile(`(?:No matching distribution found for|Could not find a version that satisfies the requirement) ([^\s(]+)`)
	fromVersRe  = regexp.MustCompile(`\(from versions: ([^)]*)\)`)
	npmNoVersRe = regexp.MustCompile(`(?:No matching version found for|ERR_PNPM_NO_MATCHING_VERSION.*?for) ([^\s.]+[^\s]*)`)
)

func rulePipNoVersion(c *ctx) *finding {
	i, m := c.find(noDistRe)
	if i < 0 {
		if j, m2 := c.find(npmNoVersRe); j >= 0 {
			return &finding{category: CatRepository, evidence: []int{j},
				cause:  fmt.Sprintf("The project asks for %s, which the registry doesn't have.", strings.TrimRight(m2[1], ".")),
				advice: "Fix the version in package.json or the lockfile."}
		}
		return nil
	}
	req := strings.TrimRight(m[1], ".")
	f := &finding{category: CatRepository, evidence: []int{i},
		cause:  fmt.Sprintf("The requirements ask for %s, which the package index doesn't have.", req),
		advice: "Fix the pin in the requirements file."}
	if j, fm := c.find(fromVersRe); j >= 0 {
		f.evidence = uniq(append(f.evidence, j))
		vs := strings.Split(fm[1], ", ")
		if len(vs) > 0 && strings.TrimSpace(fm[1]) != "none" {
			last := vs
			if len(last) > 3 {
				last = last[len(last)-3:]
			}
			f.advice = fmt.Sprintf("Fix the pin in the requirements file; the newest versions on the index are %s.", strings.Join(last, ", "))
		}
		if strings.TrimSpace(fm[1]) == "none" {
			f.cause = fmt.Sprintf("The package index has no version of %s at all: the name is wrong, or the package lives on another index.", req)
			f.advice = "Check the name, and the index the requirements point at."
		}
	}
	return f
}

// ---- commands, libraries, locales ----

var cmdMissingRes = []*regexp.Regexp{
	regexp.MustCompile(`^(?:bash: )?(?:line [0-9]+: )?([A-Za-z0-9_.+-]+): command not found$`),
	regexp.MustCompile(`^/bin/sh: [0-9]+: ([A-Za-z0-9_.+-]+): not found$`),
	regexp.MustCompile(`^\S+: line [0-9]+: ([A-Za-z0-9_.+-]+): command not found$`),
	regexp.MustCompile(`(?:FileNotFoundError|No such file or directory): (?:\[Errno 2\] No such file or directory: )?'([A-Za-z0-9_.+-]+)'$`),
	regexp.MustCompile(`exec: "([A-Za-z0-9_.+-]+)": executable file not found in \$PATH`),
	regexp.MustCompile(`spawn ([A-Za-z0-9_.+-]+) ENOENT`),
}

func ruleCommandMissing(c *ctx) *finding {
	for _, re := range cmdMissingRes {
		for i, l := range c.lines {
			if c.noise[i] || !cmdWords.MatchString(l.Text) {
				continue
			}
			m := re.FindStringSubmatch(strings.TrimSpace(l.Text))
			if m == nil {
				continue
			}
			if f := commandFix(c, m[1], i); f != nil {
				return f
			}
		}
	}
	return nil
}

var cmdWords = regexp.MustCompile(`not found|No such file|executable file not found|ENOENT`)

func commandFix(c *ctx, cmd string, i int) *finding {
	base := kb.Bases[kb.DefaultBase]
	f := &finding{category: CatSpec, evidence: []int{i}}
	if rt, ok := commandRuntimes[cmd]; ok {
		f.cause = fmt.Sprintf("The step runs %s, and no %s runtime is installed.", cmd, runtimeTitle(rt))
		switch rt {
		case "python":
			if c.spec != nil && c.spec.Python != "" {
				return nil
			}
			f.changes = []Change{{Op: "set-runtime", Key: "python", Value: base.DistroPython}}
		default:
			f.advice = fmt.Sprintf("Add %s under runtimes in preconfig.yaml, with the version the project needs.", rt)
		}
		return f
	}
	if tool, ok := commandTools[cmd]; ok {
		if c.spec != nil {
			if _, has := c.spec.Tool(tool); has {
				return nil
			}
		}
		f.cause = fmt.Sprintf("The step runs %s, and %s isn't installed.", cmd, tool)
		f.changes = []Change{{Op: "add-tool", Value: tool}}
		return f
	}
	if pkg, ok := commandPackages[cmd]; ok {
		if c.spec != nil && contains(c.spec.Packages, pkg) {
			return nil
		}
		f.cause = fmt.Sprintf("The step runs %s, and %s isn't installed.", cmd, cmd)
		f.changes = []Change{{Op: "add-package", Value: pkg}}
		return f
	}
	return nil
}

func runtimeTitle(rt string) string {
	switch rt {
	case "python":
		return "Python"
	case "node":
		return "Node.js"
	case "go":
		return "Go"
	}
	return rt
}

var libRes = []*regexp.Regexp{
	regexp.MustCompile(`(lib[A-Za-z0-9_+.-]*\.so(?:\.[0-9]+)*): cannot open shared object file`),
	regexp.MustCompile(`OSError: cannot load library '([^']+)'`),
	regexp.MustCompile(`ImportError: failed to find (lib[A-Za-z0-9_]+)`),
	regexp.MustCompile(`Unable to find ([A-Za-z0-9_]+) shared library`),
	regexp.MustCompile(`no library called "([^"]+)" was found`),
	regexp.MustCompile(`(lib[a-z0-9]+) library not found`),
}

func ruleSharedLibrary(c *ctx) *finding {
	for _, re := range libRes {
		i, m := c.find(re)
		if i < 0 {
			continue
		}
		lib := m[1]
		pkg := libraryPackage(lib)
		f := &finding{category: CatSpec, evidence: []int{i}}
		if pkg == "" {
			f.cause = fmt.Sprintf("A program needs the shared library %s, and it isn't installed.", lib)
			f.advice = "Doctor doesn't know which Ubuntu package provides it: apt-file search " + lib + " finds it; add that package under packages."
			return f
		}
		if c.spec != nil && contains(c.spec.Packages, pkg) {
			continue
		}
		f.cause = fmt.Sprintf("A program needs the shared library %s, from %s.", lib, pkg)
		f.changes = []Change{{Op: "add-package", Value: pkg}}
		return f
	}
	return nil
}

func libraryPackage(lib string) string {
	if p, ok := libraryPackages[lib]; ok {
		return p
	}
	// "libmagic.so.1.0.0" → "libmagic.so.1"; "libfoo.so" → the table's key
	base := lib
	if i := strings.Index(base, ".so"); i >= 0 {
		stem := base[:i]
		for k, p := range libraryPackages {
			if strings.HasPrefix(k, stem+".so") || k == stem || "lib"+k == stem {
				return p
			}
		}
	}
	return ""
}

var localeRe = regexp.MustCompile(`locale\.Error: unsupported locale setting|setlocale: LC_[A-Z]+: cannot change locale|Invalid locale|locale not supported`)

func ruleLocale(c *ctx) *finding {
	i, _ := c.find(localeRe)
	if i < 0 {
		return nil
	}
	if c.spec != nil && (contains(c.spec.Packages, "locales-all") || contains(c.spec.Packages, "locales")) {
		return nil
	}
	return &finding{category: CatSpec, evidence: []int{i},
		cause:   "The code asks for a locale that a clean Ubuntu doesn't have; it ships only C.UTF-8.",
		changes: []Change{{Op: "add-package", Value: "locales-all"}}}
}

// ---- the project's services, as its code sees them ----

var refusedRes = []*regexp.Regexp{
	regexp.MustCompile(`Error 111 connecting to (?:localhost|127\.0\.0\.1):([0-9]+)`),
	regexp.MustCompile(`connection to server at "(?:localhost|127\.0\.0\.1|::1)"(?: \([^)]*\))?,? port ([0-9]+) failed: Connection refused`),
	regexp.MustCompile(`(?:could not connect to server|Connection refused).*?(?:localhost|127\.0\.0\.1).*?port ([0-9]+)`),
	regexp.MustCompile(`ECONNREFUSED (?:127\.0\.0\.1|::1|localhost):([0-9]+)`),
	regexp.MustCompile(`dial tcp (?:127\.0\.0\.1|\[::1\]|localhost):([0-9]+): connect: connection refused`),
	regexp.MustCompile(`(?:localhost|127\.0\.0\.1):([0-9]+).*Connection refused`),
}

var servicePorts = map[string]string{"6379": "redis", "5432": "postgres", "3306": "mysql", "27017": "mongodb", "5672": "rabbitmq", "9200": "elasticsearch", "11211": "memcached"}

func ruleServiceRefused(c *ctx) *finding {
	for _, re := range refusedRes {
		i, m := c.find(re)
		if i < 0 {
			continue
		}
		port := m[1]
		return serviceFix(c, port, i)
	}
	return nil
}

func serviceFix(c *ctx, port string, i int) *finding {
	base := kb.Bases[kb.DefaultBase]
	svc := servicePorts[port]
	f := &finding{category: CatSpec, evidence: []int{i}}
	switch svc {
	case "redis":
		if c.spec != nil {
			if _, ok := c.spec.Service("redis"); ok {
				f.category = CatPlatform
				f.cause = "The code couldn't connect to Redis on port 6379, though the spec runs Redis: it had stopped, or never started."
				return f
			}
		}
		f.cause = "The code connects to Redis on localhost:6379, and nothing is listening there: the spec doesn't run Redis."
		f.changes = []Change{{Op: "add-service", Key: "redis", Value: base.DistroRedis}}
		return f
	case "postgres":
		if c.spec != nil {
			if _, ok := c.spec.Service("postgres"); ok {
				f.category = CatPlatform
				f.cause = "The code couldn't connect to PostgreSQL on port 5432, though the spec runs it: it had stopped, or never started."
				return f
			}
		}
		f.cause = "The code connects to PostgreSQL on localhost:5432, and nothing is listening there: the spec doesn't run PostgreSQL."
		ch := Change{Op: "add-service", Key: "postgres", Value: base.DistroPostgres}
		if c.spec != nil {
			if u := findURL(c.spec, "postgres", "5432"); u != nil {
				ch.User = u.User.Username()
				ch.Password, _ = u.User.Password()
				ch.Database = strings.TrimPrefix(u.Path, "/")
			}
		}
		f.changes = []Change{ch}
		return f
	case "":
		// A port that no service uses: maybe an env URL points at the wrong port.
		if c.spec != nil {
			for _, e := range c.spec.Env {
				u, err := url.Parse(e.Value)
				if err != nil || u.Port() != port || !localRe.MatchString(u.Host) {
					continue
				}
				want := ""
				switch u.Scheme {
				case "postgres", "postgresql":
					if _, ok := c.spec.Service("postgres"); ok {
						want = "5432"
					}
				case "redis", "rediss":
					if _, ok := c.spec.Service("redis"); ok {
						want = "6379"
					}
				}
				if want == "" {
					continue
				}
				nu := *u
				nu.Host = u.Hostname() + ":" + want
				f.cause = fmt.Sprintf("%s points at port %s, and the spec's %s listens on %s.", e.Key, port, serviceTitle(schemeService(u.Scheme)), want)
				f.changes = []Change{{Op: "set-env", Key: e.Key, Value: nu.String()}}
				return f
			}
		}
		f.category = CatUnknown
		f.cause = fmt.Sprintf("The code couldn't connect to port %s on this machine, and nothing in the spec runs there.", port)
		return f
	default:
		f.cause = fmt.Sprintf("The code connects to %s on port %s, which preconfig can't run yet.", svc, port)
		f.advice = "Run it in the platform's own way for now; the services preconfig runs are PostgreSQL and Redis."
		return f
	}
}

func schemeService(s string) string {
	switch s {
	case "postgres", "postgresql":
		return "postgres"
	case "redis", "rediss":
		return "redis"
	}
	return s
}

// findURL finds an env value that points at the named service on this machine.
func findURL(s *spec.Spec, scheme, port string) *url.URL {
	for _, e := range s.Env {
		u, err := url.Parse(e.Value)
		if err != nil || !strings.HasPrefix(u.Scheme, scheme) {
			continue
		}
		if !localRe.MatchString(u.Host) {
			continue
		}
		if u.Port() == "" || u.Port() == port {
			return u
		}
	}
	return nil
}

var pgAuthRe = regexp.MustCompile(`password authentication failed for user "([^"]+)"`)

func rulePgAuth(c *ctx) *finding {
	i, m := c.find(pgAuthRe)
	if i < 0 {
		return nil
	}
	f := &finding{category: CatSpec, evidence: []int{i},
		cause: fmt.Sprintf("PostgreSQL refused the password for %s: the connection settings and the spec's postgres service disagree.", m[1])}
	if c.spec == nil {
		f.advice = "Make the password in the connection settings match the postgres service's in preconfig.yaml."
		return f
	}
	sv, ok := c.spec.Service("postgres")
	if !ok {
		return f
	}
	for _, e := range c.spec.Env {
		u, err := url.Parse(e.Value)
		if err != nil || !strings.HasPrefix(u.Scheme, "postgres") || !localRe.MatchString(u.Host) {
			continue
		}
		p, _ := u.User.Password()
		if u.User.Username() == sv.User && p == sv.Password {
			continue
		}
		nu := *u
		nu.User = url.UserPassword(sv.User, sv.Password)
		f.cause = fmt.Sprintf("PostgreSQL refused the password for %s: %s doesn't use the user and password the spec's postgres service creates.", m[1], e.Key)
		f.changes = []Change{{Op: "set-env", Key: e.Key, Value: nu.String()}}
		return f
	}
	return f
}

var pgNoDBRe = regexp.MustCompile(`database "([^"]+)" does not exist`)

func rulePgNoDatabase(c *ctx) *finding {
	i, m := c.find(pgNoDBRe)
	if i < 0 {
		return nil
	}
	f := &finding{category: CatSpec, evidence: []int{i},
		cause: fmt.Sprintf("The code uses a database named %s, and the setup doesn't create it.", m[1])}
	if c.spec == nil {
		return f
	}
	sv, ok := c.spec.Service("postgres")
	if !ok {
		return f
	}
	for _, e := range c.spec.Env {
		u, err := url.Parse(e.Value)
		if err != nil || !strings.HasPrefix(u.Scheme, "postgres") || !localRe.MatchString(u.Host) {
			continue
		}
		if strings.TrimPrefix(u.Path, "/") != m[1] {
			continue
		}
		nu := *u
		nu.Path = "/" + sv.Database
		f.cause = fmt.Sprintf("%s names the database %s, and the spec's postgres service creates %s.", e.Key, m[1], sv.Database)
		f.changes = []Change{{Op: "set-env", Key: e.Key, Value: nu.String()}}
		return f
	}
	return f
}

var (
	keyErrorRe = regexp.MustCompile(`KeyError: '([A-Z][A-Z0-9_]{2,})'`)
	envMissRes = []*regexp.Regexp{
		keyErrorRe,
		regexp.MustCompile(`(?i)missing (?:required )?environment variable:? ['"]?([A-Z][A-Z0-9_]{2,})`),
		regexp.MustCompile(`(?:environment variable|env var) ['"]?([A-Z][A-Z0-9_]{2,})['"]? (?:is )?(?:not set|missing|undefined|required)`),
		regexp.MustCompile(`^>>> preconfig: warning: ([A-Z][A-Z0-9_]{2,}) is not set$`),
	}
	secretNameRe = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIALS?)`)
)

func ruleMissingEnv(c *ctx) *finding {
	for _, re := range envMissRes {
		i, m := c.find(re)
		if i < 0 {
			continue
		}
		name := m[1]
		// The script's own warning only counts when the code then failed on it.
		if strings.HasPrefix(c.lines[i].Text, ">>> preconfig: warning:") {
			if c.phase() != "ready" && c.phase() != "project" {
				continue
			}
		}
		ev := []int{i}
		for j, l := range c.lines {
			if j != i && strings.Contains(l.Text, name) && (strings.Contains(l.Text, "KeyError") || strings.Contains(l.Text, "is not set")) {
				ev = append(ev, j)
				break
			}
		}
		return envFix(c, name, uniq(ev))
	}
	return nil
}

func envFix(c *ctx, name string, ev []int) *finding {
	f := &finding{evidence: ev}
	if c.spec != nil && contains(c.spec.Secrets, name) {
		f.category = CatSecret
		f.cause = fmt.Sprintf("The code reads %s, a secret the spec names, and it isn't set where the setup runs.", name)
		f.advice = fmt.Sprintf("Set %s in the platform's secret settings. For preconfig verify, export it in the shell that runs verify; verify passes the secrets the spec names.", name)
		return f
	}
	if c.spec != nil {
		if v := serviceURL(c.spec, name); v != "" {
			f.category = CatSpec
			f.cause = fmt.Sprintf("The code reads %s, and the spec doesn't set it; the spec's service says what it should be.", name)
			f.changes = []Change{{Op: "add-env", Key: name, Value: v}}
			return f
		}
	}
	if secretNameRe.MatchString(name) {
		f.category = CatSecret
		f.cause = fmt.Sprintf("The code reads %s, which looks like a secret, and nothing sets it.", name)
		f.changes = []Change{{Op: "add-secret", Key: name}}
		f.advice = fmt.Sprintf("Set %s in each platform's secret settings; the spec only names it.", name)
		return f
	}
	f.category = CatSpec
	f.cause = fmt.Sprintf("The code reads %s, and nothing sets it.", name)
	f.advice = fmt.Sprintf("Add %s under env in preconfig.yaml, with the value the project expects.", name)
	return f
}

// serviceURL is the value a variable such as DATABASE_URL should have for a
// service the spec runs, or "".
func serviceURL(s *spec.Spec, name string) string {
	n := strings.ToUpper(name)
	if pg, ok := s.Service("postgres"); ok && (strings.Contains(n, "DATABASE") || strings.Contains(n, "POSTGRES") || strings.HasPrefix(n, "PG") || strings.Contains(n, "DB_URL") || n == "DSN") {
		return fmt.Sprintf("postgres://%s:%s@localhost:5432/%s", pg.User, pg.Password, pg.Database)
	}
	if _, ok := s.Service("redis"); ok && (strings.Contains(n, "REDIS") || strings.Contains(n, "CACHE_URL")) {
		return "redis://localhost:6379/0"
	}
	return ""
}

var versionCheckRe = regexp.MustCompile(`^(?:(\S+) is (.+), not (\S+)|postgres ([0-9]+) did not install|redis-server is not version ([0-9]+))`)

func ruleVersionCheck(c *ctx) *finding {
	if c.step == nil || c.step.Reason == "" {
		return nil
	}
	m := versionCheckRe.FindStringSubmatch(c.step.Reason)
	if m == nil {
		return nil
	}
	return &finding{category: CatPlatform, evidence: lastLines(c, 1),
		cause:  "The install finished, but the machine has another version than the spec asks for: " + c.step.Reason + ".",
		advice: "An older version already on the image comes first, or the step's package source was skipped. Check the lines above for a source that failed."}
}

var (
	testFailRes = []*regexp.Regexp{
		regexp.MustCompile(`^E\s+(?:assert |AssertionError)`),
		regexp.MustCompile(`^(?:FAILED|ERROR) \S+::\S+`),
		regexp.MustCompile(`^=+ .*\b[0-9]+ (?:failed|error)`),
		regexp.MustCompile(`^Tests:\s+[0-9]+ failed`),
		regexp.MustCompile(`^--- FAIL: `),
		regexp.MustCompile(`^FAIL\t`),
		regexp.MustCompile(`AssertionError`),
	}
	assertRe = regexp.MustCompile(`^E\s+(?:assert |AssertionError)|AssertionError|^E\s+where `)
)

func ruleTestsFailed(c *ctx) *finding {
	if c.phase() != "ready" && c.phase() != "" {
		return nil
	}
	var ev []int
	hasAssert := false
	for i, l := range c.lines {
		if assertRe.MatchString(l.Text) {
			hasAssert = true
		}
		for _, re := range testFailRes {
			if re.MatchString(l.Text) {
				ev = append(ev, i)
				break
			}
		}
	}
	if len(ev) == 0 || !hasAssert {
		return nil
	}
	if len(ev) > 4 {
		ev = append(ev[:3], ev[len(ev)-1])
	}
	return &finding{category: CatCode, evidence: ev,
		cause:  "The setup worked. The tests ran and failed on their own assertions, which is about the code, not the machine.",
		advice: "Look at the failing assertion. Nothing in preconfig.yaml needs to change."}
}

// ---- helpers ----

func lastLines(c *ctx, n int) []int {
	var out []int
	for i := len(c.lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(c.lines[i].Text) != "" && !c.noise[i] {
			out = append([]int{i}, out...)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func uniq(xs []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Ints(out)
	return out
}
