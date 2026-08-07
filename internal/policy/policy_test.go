package policy

import (
	"github.com/Kyrie-w8/aster-edge/internal/config"
	"testing"
)

func TestDecisionPrecedenceAndApproval(t *testing.T) {
	engine := New(config.SecurityConfig{AllowedTools: []string{"fs_*", "shell_exec"}, DeniedTools: []string{"fs_secret"}, ApprovalTools: []string{"shell_exec"}})
	if d := engine.Decide("fs_secret", "read"); d.Allowed {
		t.Fatal("explicit deny must win")
	}
	if d := engine.Decide("system_snapshot", "read"); d.Allowed {
		t.Fatal("unlisted tool must be denied")
	}
	if d := engine.Decide("fs_read", "read"); !d.Allowed || d.RequiresApproval {
		t.Fatalf("unexpected read decision: %+v", d)
	}
	if d := engine.Decide("fs_write", "write"); !d.Allowed || !d.RequiresApproval {
		t.Fatalf("write must require approval: %+v", d)
	}
}
