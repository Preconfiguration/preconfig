package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealDocker runs the orders-api sample on a real clean machine. It needs
// Docker with the ubuntu:24.04 image and network access to Ubuntu's archive and
// PyPI, so it runs only when asked:
//
//	PRECONFIG_REAL_DOCKER=1 go test ./internal/verify -run TestRealDocker -v
//
// PRECONFIG_NETWORK and PRECONFIG_CA_FILE pass --network and --ca-file.
func TestRealDocker(t *testing.T) {
	if os.Getenv("PRECONFIG_REAL_DOCKER") == "" {
		t.Skip("set PRECONFIG_REAL_DOCKER=1 to run on a real machine")
	}
	opts := Options{
		Dir:     filepath.Join("..", "..", "testdata", "repos", "orders-api"),
		Network: os.Getenv("PRECONFIG_NETWORK"),
		CAFile:  os.Getenv("PRECONFIG_CA_FILE"),
		Timeout: 20 * time.Minute,
		Events: func(e Event) {
			if e.Type == "step.end" || e.Type == "verify.end" || e.Type == "step.fail" {
				t.Logf("%6.1fs %s %s %v", e.T, e.Type, e.Step, e.Fields)
			}
		},
	}
	res := Run(context.Background(), mustSpec(t, ordersSpec), opts)
	if res.Code != Ready {
		t.Fatalf("not ready: %+v", res)
	}
	t.Logf("ready in %.1f s: %s", res.Seconds, res.Versions)
}
