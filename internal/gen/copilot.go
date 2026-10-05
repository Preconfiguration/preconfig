package gen

import (
	"fmt"
	"regexp"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

// ValidationEvents are the events on which the Copilot workflow runs the
// ready check. When Copilot itself runs the job before a session, the check is
// skipped: it would cost time, and a failing test is often the task.
var ValidationEvents = []string{"push", "pull_request", "workflow_dispatch"}

var pipReqRe = regexp.MustCompile(`\bpip3? install\b.*\s-r\s+(\S+)`)

// copilot writes .github/workflows/copilot-setup-steps.yml.
func copilot(s *spec.Spec) File {
	var b sb
	b.WriteString(Header("#"))
	b.line("# Copilot runs the copilot-setup-steps job before it starts work. The workflow")
	b.line("# also runs when this file or preconfig.yaml changes, and then it ends with the")
	b.line("# ready check, so a pull request proves the setup before Copilot relies on it.")
	if len(s.Secrets) > 0 {
		b.line("# Secrets (%s) come from the repository's copilot environment.", strings.Join(s.Secrets, ", "))
	}
	b.line("name: Copilot Setup Steps")
	b.line("")
	b.line("on:")
	b.line("  workflow_dispatch:")
	for _, ev := range []string{"push", "pull_request"} {
		b.line("  %s:", ev)
		b.line("    paths:")
		b.line("      - %s", kb.CopilotWorkflowPath)
		b.line("      - %s", kb.SpecPath)
	}
	b.line("")
	b.line("jobs:")
	b.line("  %s:", kb.CopilotJobName)
	b.line("    runs-on: %s", s.Base.ActionsRunner)
	b.line("    permissions:")
	b.line("      contents: read")
	if len(s.Services) > 0 {
		b.line("    services:")
		for _, sv := range s.Services {
			switch sv.Name {
			case "postgres":
				b.line("      postgres:")
				b.line("        image: %s", yq(fmt.Sprintf("postgres:%d", sv.Major)))
				b.line("        env:")
				b.line("          POSTGRES_USER: %s", yq(sv.User))
				b.line("          POSTGRES_PASSWORD: %s", yq(sv.Password))
				b.line("          POSTGRES_DB: %s", yq(sv.Database))
				b.line("        ports:")
				b.line(`          - "5432:5432"`)
				b.line("        options: >-")
				b.line("          --health-cmd pg_isready")
				b.line("          --health-interval 5s")
				b.line("          --health-timeout 5s")
				b.line("          --health-retries 10")
			case "redis":
				b.line("      redis:")
				b.line("        image: %s", yq(fmt.Sprintf("redis:%d", sv.Major)))
				b.line("        ports:")
				b.line(`          - "6379:6379"`)
				b.line("        options: >-")
				b.line(`          --health-cmd "redis-cli ping"`)
				b.line("          --health-interval 5s")
				b.line("          --health-timeout 5s")
				b.line("          --health-retries 10")
			}
		}
	}
	b.line("    steps:")
	b.line("      - name: Check out the repository")
	b.line("        uses: %s", kb.ActionCheckout)
	if len(s.Env) > 0 {
		b.line("      - name: Set the project's environment")
		var lines []string
		for _, e := range s.Env {
			lines = append(lines, fmt.Sprintf(`echo %s >> "$GITHUB_ENV"`, sh(e.Key+"="+e.Value)))
		}
		b.WriteString("        run: " + block(lines, "          "))
	}
	if len(s.Packages) > 0 {
		b.line("      - name: Install system packages")
		b.WriteString("        run: " + block([]string{
			"sudo apt-get update",
			"sudo apt-get install -y --no-install-recommends " + strings.Join(s.Packages, " "),
		}, "          "))
	}
	if t, ok := s.Tool("pnpm"); ok {
		b.line("      - name: Set up pnpm")
		b.line("        uses: %s", kb.ActionSetupPnpm)
		if t.Version != "" {
			b.line("        with:")
			b.line("          version: %s", yq(t.Version))
		}
	}
	if s.Node != "" {
		b.line("      - name: Set up Node.js %s", s.Node)
		b.line("        uses: %s", kb.ActionSetupNode)
		b.line("        with:")
		b.line("          node-version: %s", yq(s.Node))
		if cache := nodeCache(s); cache != "" {
			b.line("          cache: %s", cache)
		}
	}
	if _, ok := s.Tool("yarn"); ok {
		b.line("      - name: Enable Yarn")
		b.line("        run: corepack enable")
	}
	if s.Python != "" {
		b.line("      - name: Set up Python %s", s.Python)
		b.line("        uses: %s", kb.ActionSetupPython)
		b.line("        with:")
		b.line("          python-version: %s", yq(s.Python))
		if req := pipRequirements(s); req != "" {
			b.line("          cache: pip")
			b.line("          cache-dependency-path: %s", yq(req))
		}
	}
	if t, ok := s.Tool("uv"); ok {
		b.line("      - name: Set up uv")
		b.line("        uses: %s", kb.ActionSetupUV)
		if t.Version != "" {
			b.line("        with:")
			b.line("          version: %s", yq(t.Version))
		}
	}
	if t, ok := s.Tool("poetry"); ok {
		want := "poetry"
		if t.Version != "" {
			want += "==" + t.Version
		}
		b.line("      - name: Install Poetry")
		b.line("        run: pipx install %s", yq(want))
	}
	if s.Go != "" {
		b.line("      - name: Set up Go %s", s.Go)
		b.line("        uses: %s", kb.ActionSetupGo)
		b.line("        with:")
		b.line("          go-version: %s", yq(s.Go))
	}
	secretEnv := func() {
		if len(s.Secrets) == 0 {
			return
		}
		b.line("        env:")
		for _, name := range s.Secrets {
			b.line("          %s: ${{ secrets.%s }}", name, name)
		}
	}
	if len(s.Setup) > 0 {
		b.line("      - name: Set up the project")
		secretEnv()
		b.WriteString("        run: " + block(s.Setup, "          "))
	}
	if len(s.Ready) > 0 {
		var conds []string
		for _, ev := range ValidationEvents {
			conds = append(conds, "github.event_name == '"+ev+"'")
		}
		b.line("      - name: Ready check")
		b.line("        if: %s", strings.Join(conds, " || "))
		secretEnv()
		b.WriteString("        run: " + block(s.Ready, "          "))
	}
	return File{Path: kb.CopilotWorkflowPath, Target: "copilot", Content: b.String()}
}

// nodeCache picks setup-node's cache when the setup installs from a lockfile.
func nodeCache(s *spec.Spec) string {
	for _, c := range s.Setup {
		for _, part := range splitCmds(c) {
			f := strings.Fields(part)
			if len(f) >= 2 && f[0] == "npm" && (f[1] == "ci" || f[1] == "clean-install") {
				return "npm"
			}
			if len(f) >= 2 && f[0] == "pnpm" && f[1] == "install" && strings.Contains(part, "--frozen-lockfile") {
				if _, ok := s.Tool("pnpm"); ok {
					return "pnpm"
				}
			}
		}
	}
	return ""
}

// pipRequirements returns the requirements file a setup command installs from.
func pipRequirements(s *spec.Spec) string {
	for _, c := range s.Setup {
		if m := pipReqRe.FindStringSubmatch(c); m != nil {
			return m[1]
		}
	}
	return ""
}

func splitCmds(c string) []string {
	r := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n")
	return strings.Split(r.Replace(c), "\n")
}
