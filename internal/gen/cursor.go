package gen

import (
	"fmt"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
)

// cursor writes .cursor/environment.json and the Dockerfile it builds. The
// Dockerfile runs the setup script's machine step, so the image carries the
// runtimes and services; Cursor then runs the project step as its install
// command and starts the services with its start command.
func cursor(s *spec.Spec) []File {
	env := tree.NewMap()
	env.Set("name", tree.NewStr(s.Name))
	build := tree.NewMap()
	build.Set("dockerfile", tree.NewStr("Dockerfile"))
	build.Set("context", tree.NewStr(".."))
	env.Set("build", build)
	env.Set("install", tree.NewStr("bash "+kb.ScriptPath+" project"))
	if len(s.Services) > 0 {
		env.Set("start", tree.NewStr("bash "+kb.ScriptPath+" services"))
	}
	envFile := File{Path: kb.CursorEnvPath, Target: "cursor", Content: tree.JSON(env, "  ")}

	var b sb
	b.WriteString(Header("#"))
	b.line("# The machine for Cursor's cloud agents. environment.json builds this file")
	b.line("# with the repository as its context, runs the setup script's project step as")
	b.line("# its install command, and starts the services with its start command.")
	if len(s.Secrets) > 0 {
		b.line("# Secrets (%s) come from the Secrets tab of Cursor's cloud agent settings.", strings.Join(s.Secrets, ", "))
	}
	b.line("FROM %s", s.Base.DockerImage)
	b.line("ENV DEBIAN_FRONTEND=noninteractive")
	b.line("COPY %s /opt/preconfig/setup.sh", kb.ScriptPath)
	b.line("RUN bash /opt/preconfig/setup.sh machine")
	var envs []string
	if s.Go != "" {
		envs = append(envs, "GOTOOLCHAIN="+dockerQuote(goToolchain(s)+"+auto"))
	}
	for _, e := range s.Env {
		envs = append(envs, e.Key+"="+dockerQuote(e.Value))
	}
	if len(envs) > 0 {
		b.line("ENV %s", strings.Join(envs, " \\\n    "))
	}
	docker := File{Path: kb.CursorDocker, Target: "cursor", Content: b.String()}
	return []File{envFile, docker}
}

// dockerQuote quotes a value for a Dockerfile ENV instruction.
func dockerQuote(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\"'\\$`#=") {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`)
	return fmt.Sprintf(`"%s"`, r.Replace(v))
}
