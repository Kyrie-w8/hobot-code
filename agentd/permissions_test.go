package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestImagePromptsAreBoundedAndValidated(t *testing.T) {
	valid := imageContent{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("png")), Name: "board.png"}
	if err := validateImages([]imageContent{valid}); err != nil {
		t.Fatal(err)
	}
	for _, images := range [][]imageContent{
		{{Type: "image", MimeType: "application/pdf", Data: valid.Data}},
		{{Type: "image", MimeType: "image/png", Data: "not-base64"}},
		{{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(make([]byte, 1024*1024+1))}},
	} {
		if err := validateImages(images); err == nil {
			t.Fatalf("invalid images were accepted: %+v", images[0].MimeType)
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
	if got, err := normalizePermissionMode(""); err != nil || got != "ask" {
		t.Fatalf("default permission mode = %q, %v", got, err)
	}
}

func TestDeveloperModeMigratesLegacyRootConfirmation(t *testing.T) {
	dir := t.TempDir()
	current := &task{dir: dir}
	policy, err := permissionPolicyForMode("developer")
	if err != nil {
		t.Fatal(err)
	}
	policy.RootMode = "confirm"
	policy.Rules = append([]permissionRule{{Tool: "bash", Action: "allow", TargetHash: strings.Repeat("a", 64)}}, policy.Rules...)
	legacy, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current.permissionPolicyPath(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := current.ensurePermissionPolicy("developer"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(current.permissionPolicyPath())
	if err != nil {
		t.Fatal(err)
	}
	var migrated taskPermissionPolicy
	if json.Unmarshal(content, &migrated) != nil || migrated.RootMode != "policy" ||
		permissionAction(migrated, "bash") != "allow" || migrated.Rules[0].TargetHash != strings.Repeat("a", 64) {
		t.Fatalf("legacy developer policy was not migrated safely: %s", content)
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
	remembered := []byte(`{"schemaVersion":2,"rootMode":"policy","default":"ask","rules":[{"tool":"bash","action":"allow"}]}`)
	if err := os.WriteFile(current.permissionPolicyPath(), remembered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := current.ensurePermissionPolicy("ask"); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(current.permissionPolicyPath())
	if err != nil || string(content) != string(remembered) {
		t.Fatalf("remembered task policy was replaced: %q, %v", content, err)
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
