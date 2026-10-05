package gen

import (
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
)

// cloudInit writes cloud-init user data for a fresh Ubuntu 24.04 server. It
// carries the setup script and runs its steps, then clones the repository and
// runs the project and ready steps when preconfig.yaml names one.
func cloudInit(s *spec.Spec) File {
	var b sb
	b.line("#cloud-config")
	b.WriteString(Header("#"))
	b.line("# User data for a fresh Ubuntu 24.04 server: %s.", Summary(s))
	b.line("# cloud-init writes the setup script to /opt/preconfig and runs it once, at first boot.")
	if len(s.Secrets) > 0 {
		b.line("# Secrets (%s) are not in this file. Pass them to the server another way", strings.Join(s.Secrets, ", "))
		b.line("# before the project step runs.")
	}
	b.line("write_files:")
	b.line("  - path: /opt/preconfig/setup.sh")
	b.line(`    permissions: "0755"`)
	b.line("    owner: root:root")
	b.WriteString("    content: ")
	b.WriteString(block(strings.Split(strings.TrimRight(ScriptText(s), "\n"), "\n"), "      "))
	b.line("runcmd:")
	b.line("  - [bash, /opt/preconfig/setup.sh, machine]")
	if len(s.Services) > 0 {
		b.line("  - [bash, /opt/preconfig/setup.sh, services]")
	}
	if s.Repo != "" {
		dir := "/srv/" + s.Name
		b.line("  - [git, clone, --depth, %s, %s, %s]", yq("1"), yq(s.Repo), yq(dir))
		b.line("  - [bash, -c, %s]", yq("cd "+sh(dir)+" && bash /opt/preconfig/setup.sh project ready"))
	} else {
		b.line("# preconfig.yaml has no repo. Clone the project, then run in its folder:")
		b.line("#   bash /opt/preconfig/setup.sh project ready")
	}
	return File{Path: kb.CloudInitPath, Target: "cloud-init", Content: b.String()}
}
