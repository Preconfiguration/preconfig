// Package kb is the knowledge base: what each platform expects, as of the date
// below. Every name, version and rule the engine writes into a file, or checks
// a file against, lives here, so that when a platform changes its format there
// is one place to change and one date to move.
package kb

import (
	"fmt"
	"strconv"
	"strings"
)

// Date is the day this knowledge was last checked against the platforms.
const Date = "2026-09-29"

// Version of the engine.
const Version = "0.1.0"

// Base describes an operating system image that every target starts from.
type Base struct {
	Name              string // the name used in preconfig.yaml
	Codename          string
	DockerImage       string // for verify and the Cursor Dockerfile
	DevcontainerImage string
	ActionsRunner     string // runs-on for GitHub Actions
	DistroPython      string // the python3 that the distribution ships
	DistroPostgres    string // the PostgreSQL major the distribution ships
	DistroRedis       string // the Redis major the distribution ships
}

// Bases known to this version.
var Bases = map[string]Base{
	"ubuntu-24.04": {
		Name:              "ubuntu-24.04",
		Codename:          "noble",
		DockerImage:       "ubuntu:24.04",
		DevcontainerImage: "mcr.microsoft.com/devcontainers/base:ubuntu24.04",
		ActionsRunner:     "ubuntu-24.04",
		DistroPython:      "3.12",
		DistroPostgres:    "16",
		DistroRedis:       "7",
	},
}

// DefaultBase is used when preconfig.yaml names none.
const DefaultBase = "ubuntu-24.04"

// GitHub Actions used by the Copilot target. Latest major versions as of Date:
// checkout v7 (July 17, 2026), setup-node v7 (July 13), setup-go v7 (July 15),
// setup-python v7 (July 19), pnpm/action-setup v6 (August 3). setup-uv stopped
// publishing major-version tags, so it is pinned to a full release.
const (
	ActionCheckout    = "actions/checkout@v7"
	ActionSetupNode   = "actions/setup-node@v7"
	ActionSetupPython = "actions/setup-python@v7"
	ActionSetupGo     = "actions/setup-go@v7"
	ActionSetupUV     = "astral-sh/setup-uv@v10.2.0"
	ActionSetupPnpm   = "pnpm/action-setup@v6"
)

// Dev container features (github.com/devcontainers/features). The node feature
// moved to major version 2.
const (
	FeatureNode   = "ghcr.io/devcontainers/features/node:2"
	FeaturePython = "ghcr.io/devcontainers/features/python:1"
	FeatureGo     = "ghcr.io/devcontainers/features/go:1"
)

// Copilot cloud agent rules, from GitHub's "Configure the development
// environment" page.
const (
	CopilotWorkflowPath = ".github/workflows/copilot-setup-steps.yml"
	CopilotJobName      = "copilot-setup-steps"
	CopilotMaxTimeout   = 59
)

// CopilotJobKeys are the only job settings Copilot honors; it ignores the rest.
var CopilotJobKeys = []string{"steps", "permissions", "runs-on", "services", "snapshot", "timeout-minutes"}

// CopilotWrongPaths are places people put the file where Copilot never looks.
var CopilotWrongPaths = []string{
	".github/copilot-setup-steps.yml",
	".github/copilot-setup-steps.yaml",
	".github/workflows/copilot-setup-steps.yaml",
}

// Cursor cloud agents: the top-level keys that cursor.com/schemas/environment.schema.json
// allows. Unknown keys are rejected.
const CursorEnvPath = ".cursor/environment.json"

var CursorKeys = []string{
	"build", "image", "snapshot", "agentCanUpdateSnapshot",
	"name", "user", "install", "start", "repositoryDependencies",
	"disableAllMcpServers", "mcpServerAllowlist", "egressAllowlist", "egressMode",
	"chromeExecutablePath", "enable_testing", "ports", "terminals",
}

// CursorBuildKeys are the keys allowed inside "build".
var CursorBuildKeys = []string{"dockerfile", "dockerfileContents", "context"}

// Paths of the generated files.
const (
	DevcontainerPath = ".devcontainer/devcontainer.json"
	ComposePath      = ".devcontainer/compose.yaml"
	CursorDocker     = ".cursor/Dockerfile"
	CloudInitPath    = "cloud-init.yaml"
	ScriptPath       = ".preconfig/setup.sh"
	SpecPath         = "preconfig.yaml"
)

// Targets in the order they are written.
var Targets = []string{"devcontainer", "copilot", "cursor", "cloud-init", "script"}

// ---- runtimes ----

// NodeMajors that the shell targets can install from NodeSource's apt repository.
var NodeMajors = []int{20, 22, 24}

// PythonMinors that the engine accepts (3.x).
var PythonMinors = []int{10, 11, 12, 13, 14}

// GoMinors that the engine accepts (1.x).
var GoMinors = []int{24, 25, 26, 27}

// Service versions.
var PostgresMajors = []int{13, 14, 15, 16, 17, 18}

// RedisMajors packaged for the base: 7 from Ubuntu, 8 from packages.redis.io.
var RedisMajors = []int{7, 8}

// PostgresImageData is where the official postgres image keeps its data. From
// version 18 the image moved it up a level.
func PostgresImageData(major int) string {
	if major >= 18 {
		return "/var/lib/postgresql"
	}
	return "/var/lib/postgresql/data"
}

// ---- tools ----

// Tool is a package manager or helper that the setup commands need.
type Tool struct {
	Name    string
	Runtime string // the runtime it needs
}

// Tools known to this version.
var Tools = map[string]Tool{
	"pnpm":   {"pnpm", "node"},
	"yarn":   {"yarn", "node"},
	"uv":     {"uv", "python"},
	"poetry": {"poetry", "python"},
}

// ToolNames in a stable order.
var ToolNames = []string{"pnpm", "yarn", "uv", "poetry"}

// ---- helpers ----

// ParseVersion splits "3.12" or "22" or "1.24.7" into numbers.
func ParseVersion(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("a version is empty")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return nil, fmt.Errorf("%q has too many parts", s)
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return nil, fmt.Errorf("%q is not a version number", s)
		}
		out[i] = n
	}
	return out, nil
}

// Contains reports whether v is in list.
func Contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Join formats a list of ints as "20, 22 or 24".
func Join(list []int, prefix string) string {
	s := make([]string, len(list))
	for i, v := range list {
		s[i] = prefix + strconv.Itoa(v)
	}
	if len(s) == 1 {
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
}
