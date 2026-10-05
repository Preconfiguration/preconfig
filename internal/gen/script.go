package gen

import (
	"fmt"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

// Script writes .preconfig/setup.sh: the shell steps that the script,
// cloud-init and Cursor targets share, and that preconfig verify runs.
func Script(s *spec.Spec) File {
	return File{Path: kb.ScriptPath, Target: "script", Content: ScriptText(s), Mode: "0755"}
}

type sb struct{ strings.Builder }

// raw writes s and a line break, with no formatting.
func (b *sb) raw(s string) {
	b.WriteString(s)
	b.WriteByte('\n')
}

func (b *sb) line(format string, args ...any) {
	if len(args) == 0 {
		b.WriteString(format)
	} else {
		fmt.Fprintf(b, format, args...)
	}
	b.WriteByte('\n')
}

// ScriptText is the text of the setup script.
func ScriptText(s *spec.Spec) string {
	var b sb
	b.line("#!/usr/bin/env bash")
	b.WriteString(Header("#"))
	b.line("# Sets up %s on Ubuntu 24.04: %s.", s.Name, Summary(s))
	b.line("#")
	b.line("# Usage: bash %s [all|machine|services|project|ready] ...", kb.ScriptPath)
	b.line("#   all       machine, services and project, in that order (the default)")
	b.line("#   machine   system packages, runtimes, tools and services; needs root or sudo")
	b.line("#   services  starts the services and creates the database user")
	b.line("#   project   the setup commands, run in the repository's root folder")
	b.line("#   ready     the ready check, run in the repository's root folder")
	b.line("#")
	b.line("# cloud-init and the Cursor Dockerfile run these same steps, and so does")
	b.line("# preconfig verify on a clean machine. Agents that take a setup script, such as")
	b.line("# Codex, Jules or Claude Code's cloud environments, can run this file too.")
	b.line("set -Eeuo pipefail")
	b.line("")
	b.line("export DEBIAN_FRONTEND=noninteractive")
	b.line("")
	b.line(`if [ "$(id -u)" -eq 0 ]; then`)
	b.line(`  SUDO=""`)
	b.line(`elif command -v sudo >/dev/null 2>&1; then`)
	b.line(`  SUDO="sudo"`)
	b.line(`else`)
	b.line(`  echo "preconfig: run this script as root, or install sudo" >&2`)
	b.line(`  exit 1`)
	b.line(`fi`)
	b.line("")
	b.line(`CURRENT="start"`)
	b.raw(`step() { CURRENT="$*"; printf '>>> preconfig: %s\n' "$*"; }`)
	b.raw(`fail() { printf '>>> preconfig: FAILED: %s\n' "$*" >&2; exit 1; }`)
	b.line(`as_root() { if [ -n "$SUDO" ]; then "$SUDO" "$@"; else "$@"; fi; }`)
	b.line(`apt_install() { as_root apt-get install -y --no-install-recommends "$@"; }`)
	b.line(`systemd_running() { [ -d /run/systemd/system ]; }`)
	b.raw(`on_error() { local rc=$?; trap - ERR; printf '>>> preconfig: FAILED: %s (exit code %s)\n' "$CURRENT" "$rc" >&2; exit "$rc"; }`)
	b.line(`trap on_error ERR`)
	b.line("")
	writeEnv(&b, s)
	writeMachine(&b, s)
	writeServices(&b, s)
	writeProject(&b, s)
	writeReady(&b, s)
	b.line(`usage() {`)
	b.line(`  echo "usage: bash %s [all|machine|services|project|ready] ..."`, kb.ScriptPath)
	b.line(`}`)
	b.line("")
	if len(s.Services) > 0 {
		b.line(`STARTED=""`)
	}
	b.line(`[ $# -gt 0 ] || set -- all`)
	b.line(`for phase in "$@"; do`)
	b.line(`  case "$phase" in`)
	b.line(`    all) machine; services; project ;;`)
	b.line(`    machine|services|project|ready) "$phase" ;;`)
	b.line(`    -h|--help|help) usage; exit 0 ;;`)
	b.line(`    *) echo "preconfig: unknown step \"$phase\"" >&2; usage >&2; exit 2 ;;`)
	b.line(`  esac`)
	b.line(`done`)
	return b.String()
}

// envLines are the export lines for the project's environment.
func envLines(s *spec.Spec) []string {
	var out []string
	if s.Go != "" {
		out = append(out, "export GOTOOLCHAIN="+goToolchain(s)+"+auto")
	}
	for _, e := range s.Env {
		out = append(out, "export "+e.Key+"="+sh(e.Value))
	}
	return out
}

// goToolchain is the minimum Go toolchain for the spec: go1.24.0 for "1.24",
// or the exact release when the spec gives one.
func goToolchain(s *spec.Spec) string {
	if strings.Count(s.Go, ".") == 2 {
		return "go" + s.Go
	}
	return "go" + s.Go + ".0"
}

func writeEnv(b *sb, s *spec.Spec) {
	b.line(`# The project's environment. Secrets are not here: set them in the environment`)
	b.line(`# that runs this script.`)
	b.line(`project_env() {`)
	lines := envLines(s)
	if len(lines) == 0 {
		b.line(`  :`)
	}
	for _, l := range lines {
		b.raw("  " + l)
	}
	b.line(`}`)
	b.line("")
}

type step struct {
	title string
	body  []string
}

func machineSteps(s *spec.Spec) []step {
	var steps []step
	pkgs := []string{"ca-certificates", "curl", "git", "gnupg"}
	pkgs = append(pkgs, s.Packages...)
	steps = append(steps, step{"system packages", []string{
		"as_root apt-get update",
		"apt_install " + strings.Join(pkgs, " "),
	}})
	if s.Node != "" {
		v := s.Node
		steps = append(steps, step{"node " + v, []string{
			fmt.Sprintf(`if ! node --version 2>/dev/null | grep -q '^v%s\.'; then`, v),
			`  as_root install -d -m 0755 /etc/apt/keyrings`,
			`  curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | as_root gpg --dearmor --yes -o /etc/apt/keyrings/nodesource.gpg`,
			fmt.Sprintf(`  echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_%s.x nodistro main" | as_root tee /etc/apt/sources.list.d/nodesource.list >/dev/null`, v),
			`  as_root apt-get update`,
			`  apt_install nodejs`,
			`fi`,
			fmt.Sprintf(`node --version | grep -q '^v%s\.' || fail "node is $(node --version), not %s"`, v, v),
		}})
	}
	if s.Python != "" {
		steps = append(steps, pythonStep(s))
	}
	if s.Go != "" {
		minor := s.GoMinor()
		tc := goToolchain(s)
		steps = append(steps, step{"go " + s.Go, []string{
			`apt_install golang-go`,
			fmt.Sprintf(`export GOTOOLCHAIN=%s+auto`, tc),
			fmt.Sprintf(`go version | grep -q ' go%s[. ]' || fail "go is $(go version), not %s"`, strings.ReplaceAll(minor, ".", `\.`), s.Go),
		}})
	}
	for _, t := range s.Tools {
		steps = append(steps, toolStep(s, t))
	}
	for _, sv := range s.Services {
		steps = append(steps, serviceInstallStep(s, sv))
	}
	envs := envLines(s)
	if len(envs) > 0 {
		// Lines that start with a tab are written at the left margin: the
		// here-document's end marker has to be.
		body := []string{`as_root tee /etc/profile.d/preconfig.sh >/dev/null <<'PRECONFIG_ENV'`}
		for _, e := range envs {
			body = append(body, "\t"+e)
		}
		body = append(body, "\tPRECONFIG_ENV")
		steps = append(steps, step{"environment for login shells", body})
	}
	return steps
}

func pythonStep(s *spec.Spec) step {
	v := s.Python
	parts := strings.Split(v, ".")
	check := fmt.Sprintf(`python3 -c 'import sys; sys.exit(sys.version_info[:2] != (%s, %s))' || fail "python3 is $(python3 --version 2>&1), not %s"`, parts[0], parts[1], v)
	if len(parts) == 3 {
		check = fmt.Sprintf(`python3 -c 'import platform, sys; sys.exit(platform.python_version() != "%s")' || fail "python3 is $(python3 --version 2>&1), not %s"`, v, v)
	}
	if v == s.Base.DistroPython {
		return step{"python " + v, []string{
			`apt_install python3 python3-venv python3-pip`,
			check,
		}}
	}
	uvSpec := "uv"
	if t, ok := s.Tool("uv"); ok && t.Version != "" {
		uvSpec = "uv==" + t.Version
	}
	return step{"python " + v, []string{
		`apt_install python3 python3-venv pipx`,
		`if ! command -v uv >/dev/null 2>&1; then`,
		fmt.Sprintf(`  as_root env PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin pipx install %s`, sh(uvSpec)),
		`fi`,
		fmt.Sprintf(`as_root env UV_PYTHON_INSTALL_DIR=/opt/uv/python UV_PYTHON_BIN_DIR=/usr/local/bin uv python install --default %s`, v),
		`hash -r`,
		check,
	}}
}

func toolStep(s *spec.Spec, t spec.ToolUse) step {
	pipx := func(pkg string) []string {
		want := pkg
		if t.Version != "" {
			want = pkg + "==" + t.Version
		}
		return []string{
			`command -v pipx >/dev/null 2>&1 || apt_install pipx`,
			fmt.Sprintf(`if ! command -v %s >/dev/null 2>&1; then`, pkg),
			fmt.Sprintf(`  as_root env PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin pipx install %s`, sh(want)),
			`fi`,
			fmt.Sprintf(`%s --version`, pkg),
		}
	}
	switch t.Name {
	case "pnpm":
		want := "pnpm"
		if t.Version != "" {
			want = "pnpm@" + t.Version
		}
		return step{t.String(), []string{
			fmt.Sprintf(`as_root npm install -g %s`, want),
			`pnpm --version`,
		}}
	case "yarn":
		return step{"yarn", []string{
			`as_root corepack enable`,
			`yarn --version`,
		}}
	case "uv":
		if s.Python != "" && s.Python != s.Base.DistroPython {
			// Installed with python already.
			return step{t.String(), []string{`uv --version`}}
		}
		return step{t.String(), pipx("uv")}
	case "poetry":
		return step{t.String(), pipx("poetry")}
	}
	return step{t.String(), nil}
}

func serviceInstallStep(s *spec.Spec, sv spec.Service) step {
	switch sv.Name {
	case "postgres":
		pkg := fmt.Sprintf("postgresql-%d postgresql-client-%d", sv.Major, sv.Major)
		body := []string{}
		if sv.Version != s.Base.DistroPostgres {
			body = append(body,
				`as_root install -d /usr/share/postgresql-common/pgdg`,
				`curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | as_root tee /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc >/dev/null`,
				fmt.Sprintf(`echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt %s-pgdg main" | as_root tee /etc/apt/sources.list.d/pgdg.list >/dev/null`, s.Base.Codename),
				`as_root apt-get update`,
			)
		}
		body = append(body, "apt_install "+pkg)
		body = append(body, fmt.Sprintf(`/usr/lib/postgresql/%d/bin/postgres --version | grep -q ' %d\.' || fail "postgres %d did not install"`, sv.Major, sv.Major, sv.Major))
		return step{"postgres " + sv.Version, body}
	case "redis":
		body := []string{}
		if sv.Version != s.Base.DistroRedis {
			body = append(body,
				`as_root install -d -m 0755 /etc/apt/keyrings`,
				`curl -fsSL https://packages.redis.io/gpg | as_root gpg --dearmor --yes -o /etc/apt/keyrings/redis.gpg`,
				fmt.Sprintf(`echo "deb [signed-by=/etc/apt/keyrings/redis.gpg] https://packages.redis.io/deb %s main" | as_root tee /etc/apt/sources.list.d/redis.list >/dev/null`, s.Base.Codename),
				`as_root apt-get update`,
			)
		}
		body = append(body,
			`apt_install redis-server redis-tools`,
			fmt.Sprintf(`redis-server --version | grep -q ' v=%d\.' || fail "redis-server is not version %d: $(redis-server --version)"`, sv.Major, sv.Major),
		)
		return step{"redis " + sv.Version, body}
	}
	return step{sv.Name, nil}
}

func writeSteps(b *sb, phase string, steps []step) {
	for i, st := range steps {
		b.line(`  step "%s %d/%d: %s"`, phase, i+1, len(steps), shellEscapeDouble(st.title))
		for _, l := range st.body {
			if strings.HasPrefix(l, "\t") {
				b.raw(l[1:])
				continue
			}
			b.raw("  " + l)
		}
	}
}

func writeMachine(b *sb, s *spec.Spec) {
	b.line(`machine() {`)
	writeSteps(b, "machine", machineSteps(s))
	b.line(`  step "machine: done (%s)"`, installedSummary(s))
	b.line(`}`)
	b.line("")
}

// installedSummary is shell text that prints the versions actually installed.
func installedSummary(s *spec.Spec) string {
	var parts []string
	if s.Node != "" {
		parts = append(parts, `node $(node --version | tr -d v)`)
	}
	if s.Python != "" {
		parts = append(parts, `python $(python3 -c 'import platform; print(platform.python_version())')`)
	}
	if s.Go != "" {
		parts = append(parts, `go $(go env GOVERSION | sed 's/^go//')`)
	}
	for _, sv := range s.Services {
		switch sv.Name {
		case "postgres":
			parts = append(parts, fmt.Sprintf(`postgres $(/usr/lib/postgresql/%d/bin/postgres --version | awk '{print $3}')`, sv.Major))
		case "redis":
			parts = append(parts, `redis $(redis-server --version | sed 's/.* v=\([^ ]*\).*/\1/')`)
		}
	}
	if len(parts) == 0 {
		return "system packages only"
	}
	return strings.Join(parts, ", ")
}

func shellEscapeDouble(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return r.Replace(s)
}

func writeServices(b *sb, s *spec.Spec) {
	b.line(`services() {`)
	if len(s.Services) == 0 {
		b.line(`  : # No services in preconfig.yaml.`)
		b.line(`}`)
		b.line("")
		return
	}
	var steps []step
	for _, sv := range s.Services {
		switch sv.Name {
		case "postgres":
			m := sv.Major
			sql := fmt.Sprintf(`DO \$\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s LOGIN PASSWORD '%s'; ELSE ALTER ROLE %s WITH LOGIN PASSWORD '%s'; END IF; END \$\$;`,
				sv.User, sv.User, sv.Password, sv.User, sv.Password)
			steps = append(steps, step{"postgres " + sv.Version, []string{
				`if systemd_running; then`,
				`  as_root systemctl enable --now postgresql`,
				fmt.Sprintf(`elif ! as_root pg_ctlcluster %d main status >/dev/null 2>&1; then`, m),
				fmt.Sprintf(`  as_root pg_ctlcluster %d main start`, m),
				`fi`,
				`for _ in $(seq 1 30); do pg_isready -q -h localhost -p 5432 && break; sleep 1; done`,
				fmt.Sprintf(`pg_isready -q -h localhost -p 5432 || fail "postgres %s is not accepting connections on localhost:5432"`, sv.Version),
				fmt.Sprintf(`as_root runuser -u postgres -- psql -q -v ON_ERROR_STOP=1 -c "%s"`, sql),
				fmt.Sprintf(`if [ "$(as_root runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname = '%s'")" != "1" ]; then`, sv.Database),
				fmt.Sprintf(`  as_root runuser -u postgres -- createdb -O %s %s`, sv.User, sv.Database),
				`fi`,
			}})
		case "redis":
			steps = append(steps, step{"redis " + sv.Version, []string{
				`if systemd_running; then`,
				`  as_root systemctl enable --now redis-server`,
				`elif ! redis-cli -h 127.0.0.1 ping >/dev/null 2>&1; then`,
				`  as_root install -d -o redis -g redis /run/redis`,
				`  as_root redis-server /etc/redis/redis.conf --daemonize yes`,
				`fi`,
				`for _ in $(seq 1 30); do [ "$(redis-cli -h 127.0.0.1 ping 2>/dev/null)" = "PONG" ] && break; sleep 1; done`,
				fmt.Sprintf(`[ "$(redis-cli -h 127.0.0.1 ping 2>/dev/null)" = "PONG" ] || fail "redis %s is not answering on localhost:6379"`, sv.Version),
			}})
		}
	}
	writeSteps(b, "services", steps)
	b.line(`  step "services: done"`)
	b.line(`  STARTED=1`)
	b.line(`}`)
	b.line("")
}

func writeProject(b *sb, s *spec.Spec) {
	b.line(`project() {`)
	b.line(`  project_env`)
	if len(s.Services) > 0 {
		b.line(`  [ -n "$STARTED" ] || services`)
	}
	for _, sec := range s.Secrets {
		b.line(`  [ -n "${%s:-}" ] || printf '>>> preconfig: warning: %s is not set\n' >&2`, sec, sec)
	}
	if len(s.Setup) == 0 {
		b.line(`  step "project: no setup commands in preconfig.yaml"`)
	}
	for i, c := range s.Setup {
		b.line(`  step "project %d/%d: %s"`, i+1, len(s.Setup), shellEscapeDouble(c))
		b.raw("  " + c)
	}
	b.line(`}`)
	b.line("")
}

func writeReady(b *sb, s *spec.Spec) {
	b.line(`ready() {`)
	b.line(`  project_env`)
	if len(s.Services) > 0 {
		b.line(`  [ -n "$STARTED" ] || services`)
	}
	if len(s.Ready) == 0 {
		b.line(`  step "ready: no ready check in preconfig.yaml"`)
	}
	for i, c := range s.Ready {
		b.line(`  step "ready %d/%d: %s"`, i+1, len(s.Ready), shellEscapeDouble(c))
		b.raw("  " + c)
	}
	if len(s.Ready) > 0 {
		b.line(`  step "ready: passed"`)
	}
	b.line(`}`)
	b.line("")
}
