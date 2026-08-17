package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultTaskPermissionMode = "ask"

type permissionRule struct {
	Tool       string `json:"tool"`
	Action     string `json:"action"`
	TargetHash string `json:"targetHash,omitempty"`
}

type taskPermissionPolicy struct {
	SchemaVersion int              `json:"schemaVersion"`
	RootMode      string           `json:"rootMode"`
	Default       string           `json:"default"`
	Rules         []permissionRule `json:"rules"`
	Reviewer      string           `json:"reviewer,omitempty"`
}

func normalizePermissionMode(value string) (string, error) {
	if value == "" {
		return defaultTaskPermissionMode, nil
	}
	switch value {
	case "review", "ask", "auto-review", "developer":
		return value, nil
	default:
		return "", fmt.Errorf("permission mode must be review, ask, auto-review, or developer")
	}
}

func permissionPolicyForMode(mode string) (taskPermissionPolicy, error) {
	normalized, err := normalizePermissionMode(mode)
	if err != nil {
		return taskPermissionPolicy{}, err
	}
	base := []permissionRule{
		{Tool: "read", Action: "allow"},
		{Tool: "ls", Action: "allow"},
		{Tool: "find", Action: "allow"},
		{Tool: "grep", Action: "allow"},
		{Tool: "system_snapshot", Action: "allow"},
		{Tool: "rdk_docs_search", Action: "allow"},
		{Tool: "memory_search", Action: "allow"},
		{Tool: "goal_status", Action: "allow"},
		{Tool: "goal_progress", Action: "allow"},
		{Tool: "lsp", Action: "allow"},
	}
	policy := taskPermissionPolicy{SchemaVersion: 2, RootMode: "policy", Default: "ask", Rules: base}
	switch normalized {
	case "review":
		policy.Rules = append(policy.Rules,
			permissionRule{Tool: "network", Action: "deny"},
			permissionRule{Tool: "write", Action: "deny"},
			permissionRule{Tool: "edit", Action: "deny"},
			permissionRule{Tool: "bash", Action: "deny"},
			permissionRule{Tool: "quality_gate", Action: "deny"},
			permissionRule{Tool: "memory_save", Action: "deny"},
			permissionRule{Tool: "goal_complete", Action: "deny"},
			permissionRule{Tool: "mcp:*", Action: "ask"},
		)
	case "ask", "auto-review":
		// Auto-review has Ask's board policy. Its extension reviewer can only
		// decide one qualifying action and cannot persist a broader grant.
		policy.RootMode = "confirm"
		if normalized == "auto-review" {
			policy.Reviewer = "auto-review"
		}
		policy.Rules = append(policy.Rules,
			permissionRule{Tool: "network", Action: "ask"},
			permissionRule{Tool: "write", Action: "ask"},
			permissionRule{Tool: "edit", Action: "ask"},
			permissionRule{Tool: "bash", Action: "ask"},
			permissionRule{Tool: "quality_gate", Action: "ask"},
			permissionRule{Tool: "memory_save", Action: "ask"},
			permissionRule{Tool: "goal_complete", Action: "ask"},
			permissionRule{Tool: "mcp:*", Action: "ask"},
		)
	case "developer":
		// RDK sessions commonly run as root. Developer mode delegates routine
		// calls to the policy while the extension still confirms destructive work.
		policy.RootMode = "policy"
		policy.Rules = append(policy.Rules,
			permissionRule{Tool: "network", Action: "allow"},
			permissionRule{Tool: "write", Action: "allow"},
			permissionRule{Tool: "edit", Action: "allow"},
			permissionRule{Tool: "bash", Action: "allow"},
			permissionRule{Tool: "openexplorer_build_host", Action: "allow"},
			permissionRule{Tool: "openexplorer_remote_run", Action: "allow"},
			permissionRule{Tool: "quality_gate", Action: "allow"},
			permissionRule{Tool: "memory_save", Action: "allow"},
			permissionRule{Tool: "goal_complete", Action: "allow"},
			permissionRule{Tool: "mcp:*", Action: "ask"},
		)
	}
	return policy, nil
}

func (current *task) permissionPolicyPath() string {
	return filepath.Join(current.permissionPolicyDirectory(), "permissions.json")
}

func (current *task) permissionPolicyDirectory() string {
	return filepath.Join(current.taskSessionDirectory(), "policy")
}

func (current *task) writePermissionPolicy(mode string) error {
	if err := ensurePrivateDir(current.permissionPolicyDirectory()); err != nil {
		return err
	}
	policy, err := permissionPolicyForMode(mode)
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(current.permissionPolicyPath(), append(content, '\n'))
}

func (current *task) ensurePermissionPolicy(mode string) error {
	path := current.permissionPolicyPath()
	_, err := privateRegularFileInfo(path, maxRequestBytes)
	if err == nil {
		if mode != "developer" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var policy taskPermissionPolicy
		if json.Unmarshal(content, &policy) != nil || policy.SchemaVersion != 2 {
			return nil
		}
		if migrated, ok := migrateDeveloperTaskPolicy(policy); ok {
			policy = migrated
			updated, marshalErr := json.MarshalIndent(policy, "", "  ")
			if marshalErr != nil {
				return marshalErr
			}
			return writePrivateFile(path, append(updated, '\n'))
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return current.writePermissionPolicy(mode)
}

func migrateDeveloperTaskPolicy(policy taskPermissionPolicy) (taskPermissionPolicy, bool) {
	if policy.SchemaVersion != 2 || policy.Default != "ask" {
		return taskPermissionPolicy{}, false
	}
	exact := make([]permissionRule, 0)
	broad := make([]permissionRule, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.TargetHash == "" {
			broad = append(broad, rule)
		} else {
			exact = append(exact, rule)
		}
	}
	legacy, _ := permissionPolicyForMode("developer")
	for index := range legacy.Rules {
		switch legacy.Rules[index].Tool {
		case "network", "quality_gate", "memory_save", "goal_complete":
			legacy.Rules[index].Action = "ask"
		}
	}
	legacy.Rules = removePermissionRules(legacy.Rules, "openexplorer_build_host", "openexplorer_remote_run")
	candidates := [][]permissionRule{legacy.Rules}
	withoutNetwork := removePermissionRules(legacy.Rules, "network")
	candidates = append(candidates, withoutNetwork)
	for _, candidate := range candidates {
		if permissionRulesEqual(broad, candidate) {
			updated, _ := permissionPolicyForMode("developer")
			updated.Rules = append(exact, updated.Rules...)
			return updated, true
		}
	}
	return taskPermissionPolicy{}, false
}

func removePermissionRules(rules []permissionRule, tools ...string) []permissionRule {
	removed := make(map[string]bool, len(tools))
	for _, tool := range tools {
		removed[tool] = true
	}
	result := make([]permissionRule, 0, len(rules))
	for _, rule := range rules {
		if !removed[rule.Tool] {
			result = append(result, rule)
		}
	}
	return result
}

func permissionRulesEqual(left, right []permissionRule) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Tool != right[index].Tool || left[index].Action != right[index].Action {
			return false
		}
	}
	return true
}

func hasPermissionRule(policy taskPermissionPolicy, tool string) bool {
	for _, rule := range policy.Rules {
		if rule.Tool == tool && rule.TargetHash == "" {
			return true
		}
	}
	return false
}
