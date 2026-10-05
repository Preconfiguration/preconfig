package gen

import (
	"fmt"
	"strings"

	"preconfiguration.com/preconfig/internal/kb"
	"preconfiguration.com/preconfig/internal/spec"
	"preconfiguration.com/preconfig/internal/tree"
)

// devcontainer writes .devcontainer/devcontainer.json, and compose.yaml when
// the spec has services: the services run next to the dev container and share
// its network, so localhost reaches them as it does everywhere else.
func devcontainer(s *spec.Spec) []File {
	dc := tree.NewMap()
	dc.Set("name", tree.NewStr(s.Name))
	if len(s.Services) > 0 {
		dc.Set("dockerComposeFile", tree.NewStr("compose.yaml"))
		dc.Set("service", tree.NewStr("app"))
		dc.Set("workspaceFolder", tree.NewStr("/workspaces/${localWorkspaceFolderBasename}"))
		dc.Set("shutdownAction", tree.NewStr("stopCompose"))
	} else {
		dc.Set("image", tree.NewStr(s.Base.DevcontainerImage))
	}

	features := tree.NewMap()
	if s.Node != "" {
		f := tree.NewMap().Set("version", tree.NewStr(s.Node))
		if t, ok := s.Tool("pnpm"); ok {
			v := t.Version
			if v == "" {
				v = "latest"
			}
			f.Set("pnpmVersion", tree.NewStr(v))
		} else {
			f.Set("pnpmVersion", tree.NewStr("none"))
		}
		features.Set(kb.FeatureNode, f)
	}
	if s.Python != "" {
		features.Set(kb.FeaturePython, tree.NewMap().
			Set("version", tree.NewStr(s.Python)).
			Set("installTools", tree.NewBool(false)))
	}
	if s.Go != "" {
		features.Set(kb.FeatureGo, tree.NewMap().
			Set("version", tree.NewStr(s.Go)).
			Set("installGoTools", tree.NewBool(false)).
			Set("golangciLintVersion", tree.NewStr("none")))
	}
	if len(features.Keys) > 0 {
		dc.Set("features", features)
	}

	if len(s.Env) > 0 {
		env := tree.NewMap()
		for _, e := range s.Env {
			env.Set(e.Key, tree.NewStr(e.Value))
		}
		dc.Set("containerEnv", env)
	}
	if len(s.Secrets) > 0 {
		secrets := tree.NewMap()
		for _, name := range s.Secrets {
			secrets.Set(name, tree.NewMap().Set("description", tree.NewStr("Listed in preconfig.yaml: the setup needs it.")))
		}
		dc.Set("secrets", secrets)
	}

	var onCreate []string
	if len(s.Packages) > 0 {
		onCreate = append(onCreate, "sudo apt-get update", "sudo apt-get install -y --no-install-recommends "+strings.Join(s.Packages, " "))
	}
	for _, t := range s.Tools {
		switch t.Name {
		case "yarn":
			onCreate = append(onCreate, `sudo env "PATH=$PATH" corepack enable`)
		case "uv", "poetry":
			want := t.Name
			if t.Version != "" {
				want += "==" + t.Version
			}
			onCreate = append(onCreate, "pipx install "+want)
		}
	}
	if len(onCreate) > 0 {
		dc.Set("onCreateCommand", tree.NewStr(joinCommands(onCreate)))
	}
	if len(s.Setup) > 0 {
		dc.Set("postCreateCommand", tree.NewStr(joinCommands(s.Setup)))
	}

	var b strings.Builder
	b.WriteString(Header("//"))
	b.WriteString(fmt.Sprintf("// A dev container for %s: %s.\n", s.Name, Summary(s)))
	if len(s.Ready) > 0 {
		b.WriteString("// Ready check (preconfig verify runs it; the container doesn't): " + strings.Join(s.Ready, " && ") + "\n")
	}
	b.WriteString(tree.JSON(dc, "  "))
	files := []File{{Path: kb.DevcontainerPath, Target: "devcontainer", Content: b.String()}}
	if len(s.Services) > 0 {
		files = append(files, File{Path: kb.ComposePath, Target: "devcontainer", Content: compose(s)})
	}
	return files
}

func compose(s *spec.Spec) string {
	var b sb
	b.WriteString(Header("#"))
	b.line("# The dev container (app) and its services. The services share app's network,")
	b.line("# so they answer on localhost inside the dev container.")
	b.line("services:")
	b.line("  app:")
	b.line("    image: %s", yq(s.Base.DevcontainerImage))
	b.line("    command: sleep infinity")
	b.line("    volumes:")
	b.line("      - ../..:/workspaces:cached")
	var volumes []string
	for _, sv := range s.Services {
		switch sv.Name {
		case "postgres":
			b.line("  postgres:")
			b.line("    image: %s", yq(fmt.Sprintf("postgres:%d", sv.Major)))
			b.line("    restart: unless-stopped")
			b.line("    network_mode: service:app")
			b.line("    environment:")
			b.line("      POSTGRES_USER: %s", yq(sv.User))
			b.line("      POSTGRES_PASSWORD: %s", yq(sv.Password))
			b.line("      POSTGRES_DB: %s", yq(sv.Database))
			b.line("    volumes:")
			b.line("      - postgres-data:%s", kb.PostgresImageData(sv.Major))
			volumes = append(volumes, "postgres-data")
		case "redis":
			b.line("  redis:")
			b.line("    image: %s", yq(fmt.Sprintf("redis:%d", sv.Major)))
			b.line("    restart: unless-stopped")
			b.line("    network_mode: service:app")
		}
	}
	if len(volumes) > 0 {
		b.line("volumes:")
		for _, v := range volumes {
			b.line("  %s:", v)
		}
	}
	return b.String()
}
