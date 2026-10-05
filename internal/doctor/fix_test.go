package doctor

import (
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		change Change
		want   string // a line the result must contain
		keep   string // a line the result must still contain
	}{
		{"add a package to a new block", ordersSpec, Change{Op: "add-package", Value: "build-essential"},
			"packages:\n  - build-essential\n\nservices:", "# none"},
		{"add a package to a block list", strings.Replace(ordersSpec, "secrets:\n", "packages:\n  - libpq-dev   # for psycopg2\n\nsecrets:\n", 1),
			Change{Op: "add-package", Value: "build-essential"}, "  - libpq-dev   # for psycopg2\n  - build-essential\n", "# for psycopg2"},
		{"add a package to a flow list", strings.Replace(ordersSpec, "secrets:\n", "packages: [libpq-dev]\n\nsecrets:\n", 1),
			Change{Op: "add-package", Value: "python3-dev"}, "packages:\n  - libpq-dev\n  - python3-dev\n", "secrets:"},
		{"replace a package", strings.Replace(ordersSpec, "secrets:\n", "packages:\n  - libpq-devv\n\nsecrets:\n", 1),
			Change{Op: "replace-package", Old: "libpq-devv", Value: "libpq-dev"}, "  - libpq-dev\n", "secrets:"},
		{"add a tool", ordersSpec, Change{Op: "add-tool", Value: "uv"}, "tools:\n  - uv\n\nservices:", "runtimes:"},
		{"add a service", strings.Replace(ordersSpec, "  redis: \"7\"\n", "", 1), Change{Op: "add-service", Key: "redis", Value: "7"},
			"    database: orders\n  redis: \"7\"\n", "env:"},
		{"add postgres with its user", strings.Replace(ordersSpec, "  postgres:\n    version: \"16\"\n    user: orders\n    password: orders\n    database: orders\n", "", 1),
			Change{Op: "add-service", Key: "postgres", Value: "16", User: "orders", Password: "orders", Database: "orders"},
			"  redis: \"7\"\n  postgres:\n    version: \"16\"\n    user: orders\n    password: orders\n    database: orders\n", "env:"},
		{"set a runtime", ordersSpec, Change{Op: "set-runtime", Key: "python", Value: "3.13"}, "  python: \"3.13\"\n", "name: orders-api"},
		{"add a runtime block", strings.Replace(ordersSpec, "runtimes:\n  python: \"3.12\"\n\n", "", 1),
			Change{Op: "set-runtime", Key: "python", Value: "3.12"}, "name: orders-api\n\nruntimes:\n  python: \"3.12\"\n\nservices:", "setup:"},
		{"add a variable", strings.Replace(ordersSpec, "  DATABASE_URL: postgres://orders:orders@localhost:5432/orders\n", "", 1),
			Change{Op: "add-env", Key: "DATABASE_URL", Value: "postgres://orders:orders@localhost:5432/orders"},
			"  REDIS_URL: redis://localhost:6379/0\n  DATABASE_URL: postgres://orders:orders@localhost:5432/orders\n", "secrets:"},
		{"set a variable", strings.Replace(ordersSpec, "orders:orders@", "orders:secret@", 1),
			Change{Op: "set-env", Key: "DATABASE_URL", Value: "postgres://orders:orders@localhost:5432/orders"},
			"  DATABASE_URL: postgres://orders:orders@localhost:5432/orders\n", "  REDIS_URL: redis://localhost:6379/0\n"},
		{"set a quoted variable", strings.Replace(ordersSpec, "  REDIS_URL: redis://localhost:6379/0", "  REDIS_URL: \"redis://localhost:6380/0\"  # the cache", 1),
			Change{Op: "set-env", Key: "REDIS_URL", Value: "redis://localhost:6379/0"}, "  REDIS_URL: redis://localhost:6379/0  # the cache\n", "DATABASE_URL"},
		{"add a secret", ordersSpec, Change{Op: "add-secret", Key: "STRIPE_API_KEY"}, "  - PAYMENTS_API_KEY\n  - STRIPE_API_KEY\n", "setup:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			if tc.keep == "# none" {
				tc.keep = ""
			}
			out, err := Apply(src, []Change{tc.change})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("result lacks %q:\n%s", tc.want, out)
			}
			if tc.keep != "" && !strings.Contains(out, tc.keep) {
				t.Errorf("result lost %q:\n%s", tc.keep, out)
			}
			// Applying the same change again does nothing more, or refuses.
			if again, err := Apply(out, []Change{tc.change}); err == nil && again != out && tc.change.Op != "set-runtime" && tc.change.Op != "set-env" {
				t.Errorf("applied twice:\n%s", again)
			}
		})
	}
}

func TestApplyRefuses(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		change Change
	}{
		{"replace a package that isn't there", ordersSpec, Change{Op: "replace-package", Old: "nope", Value: "libpq-dev"}},
		{"an unknown operation", ordersSpec, Change{Op: "delete-everything"}},
		{"a version the spec doesn't accept", ordersSpec, Change{Op: "set-runtime", Key: "python", Value: "2.7"}},
		{"a service that is there", ordersSpec, Change{Op: "add-service", Key: "redis", Value: "7"}},
		{"a broken spec", "version: [1\n", Change{Op: "add-package", Value: "make"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Apply(tc.src, []Change{tc.change})
			if err == nil {
				t.Fatalf("Apply accepted it:\n%s", out)
			}
			if out != tc.src {
				t.Errorf("a refused change still changed the text")
			}
		})
	}
}

func TestApplyKeepsEverythingElse(t *testing.T) {
	src := "# orders-api's machine\n" + strings.Replace(ordersSpec, "services:\n", "# the databases\nservices:\n", 1)
	out, err := Apply(src, []Change{{Op: "add-package", Value: "sqlite3"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(src, "\n") {
		if !strings.Contains(out, l) {
			t.Errorf("lost line %q", l)
		}
	}
	if strings.Count(out, "\n")-strings.Count(src, "\n") != 3 {
		t.Errorf("expected 3 new lines (blank, key, item):\n%s", out)
	}
}
