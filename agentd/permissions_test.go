package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskTitlesSupportUnicodeAndDeriveFromPrompt(t *testing.T) {
	if got := deriveTaskTitle("请帮我修复 S600 上的模型部署问题，并验证结果"); got != "修复 S600 上的模型部署问题，并验证结果" {
		t.Fatalf("derived title = %q", got)
	}
	if got := deriveTaskTitle("检查 /root/yolo_bench 并修复 C\\C++ 构建"); got != "检查 root yolo_bench 并修复 C C++ 构建" {
		t.Fatalf("path-safe derived title = %q", got)
	}
	if got, err := validateTaskName("修复 S600 模型部署"); err != nil || got != "修复 S600 模型部署" {
		t.Fatalf("unicode title = %q, %v", got, err)
	}
	for _, invalid := range []string{"", "bad/name", "bad\\name", "bad\nname"} {
		if _, err := validateTaskName(invalid); err == nil {
			t.Fatalf("invalid title was accepted: %q", invalid)
		}
	}
}

func TestPermissionPoliciesKeepHighRiskToolsBounded(t *testing.T) {
	tests := []struct {
		mode     string
		rootMode string
		bash     string
		quality  string
	}{
		{mode: "review", rootMode: "policy", bash: "deny", quality: "deny"},
		{mode: "ask", rootMode: "confirm", bash: "ask", quality: "ask"},
		{mode: "developer", rootMode: "policy", bash: "allow", quality: "ask"},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			policy, err := permissionPolicyForMode(test.mode)
			if err != nil {
				t.Fatal(err)
			}
			if policy.RootMode != test.rootMode || permissionAction(policy, "bash") != test.bash || permissionAction(policy, "quality_gate") != test.quality {
				t.Fatalf("unexpected policy: %+v", policy)
			}
		})
	}
	if _, err := permissionPolicyForMode("unrestricted"); err == nil {
		t.Fatal("unsafe permission mode was accepted")
	}
}

func TestSetPermissionModePersistsPrivateTaskPolicy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "00112233445566778899aabb")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	current := &task{
		dir: dir,
		metadata: taskMetadata{
			ID: "00112233445566778899aabb", Name: "test", Status: statusIdle,
		},
	}
	manager := &taskManager{tasks: map[string]*task{current.metadata.ID: current}}
	current.manager = manager
	if _, err := manager.setPermissionMode(setTaskPermissionParams{TaskID: current.metadata.ID, Mode: "developer"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(current.permissionPolicyPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permission policy info = %+v, %v", info, err)
	}
	content, err := os.ReadFile(current.permissionPolicyPath())
	if err != nil {
		t.Fatal(err)
	}
	var policy taskPermissionPolicy
	if json.Unmarshal(content, &policy) != nil || permissionAction(policy, "bash") != "allow" {
		t.Fatalf("unexpected persisted policy: %s", content)
	}
}

func permissionAction(policy taskPermissionPolicy, tool string) string {
	for _, rule := range policy.Rules {
		if rule.Tool == tool {
			return rule.Action
		}
	}
	return policy.Default
}
