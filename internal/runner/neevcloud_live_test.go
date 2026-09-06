package runner

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestNeevCloudLive runs a real program in a real sandbox. It needs
// NEEV_API_KEY, NEEV_ORG_ID and NEEV_PROJECT_ID and is skipped otherwise.
func TestNeevCloudLive(t *testing.T) {
	if os.Getenv("NEEV_API_KEY") == "" || os.Getenv("NEEV_ORG_ID") == "" || os.Getenv("NEEV_PROJECT_ID") == "" {
		t.Skip("NEEV_API_KEY, NEEV_ORG_ID and NEEV_PROJECT_ID not all set")
	}
	base := os.Getenv("NEEV_API_BASE")
	if base == "" {
		base = "https://api.ai.neevcloud.com/agent"
	}
	n, err := NewNeevCloud(NeevCloudConfig{
		APIBase: base, APIKey: os.Getenv("NEEV_API_KEY"),
		OrgID: os.Getenv("NEEV_ORG_ID"), ProjectID: os.Getenv("NEEV_PROJECT_ID"),
		Region: os.Getenv("NEEV_REGION"), TemplateID: os.Getenv("NEEV_TEMPLATE_ID"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	res, err := n.Run(ctx, RunSpec{
		Code:        "n = int(input())\nprint(n * 2)\nimport sys\nprint(sys.version_info[:2], file=sys.stderr)\n",
		TimeLimitMS: 2000,
		Cases:       []Case{{ID: "a", Stdin: "21\n"}, {ID: "b", Stdin: "x\n"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("wall %s, harness %s, flags %v", time.Since(start), res.Duration, res.Flags)
	if len(res.Cases) != 2 || res.Cases[0].Stdout != "42\n" {
		t.Fatalf("cases = %+v", res.Cases)
	}
	t.Logf("python in sandbox: %s", res.Cases[0].Stderr)
	if e := res.Cases[1].Error; e == nil || e.Type != "ValueError" {
		t.Errorf("case b error = %+v", e)
	}
}
