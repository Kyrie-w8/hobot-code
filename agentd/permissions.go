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
}

func normalizePermissionMode(value string) (string, error) {
	if value == "" {
		return defaultTaskPermissionMode, nil
	}
	switch value {
	case "review", "ask", "developer":
		return value, nil
	default:
		return "", fmt.Errorf("permission mode must be review, ask, or developer")
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
	case "ask":
		policy.RootMode = "confirm"
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
		// calls to the policy while the extension still confirms high-risk work.
		policy.RootMode = "policy"
		policy.Rules = append(policy.Rules,
			permissionRule{Tool: "network", Action: "ask"},
			permissionRule{Tool: "write", Action: "allow"},
			permissionRule{Tool: "edit", Action: "allow"},
			permissionRule{Tool: "bash", Action: "allow"},
			permissionRule{Tool: "quality_gate", Action: "ask"},
			permissionRule{Tool: "memory_save", Action: "ask"},
			permissionRule{Tool: "goal_complete", Action: "ask"},
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
		if isLegacyDeveloperTaskPolicy(policy) {
			policy.RootMode = "policy"
			if !hasPermissionRule(policy, "network") {
				policy.Rules = insertPermissionRuleAfter(policy.Rules, "bash", permissionRule{Tool: "network", Action: "ask"})
			}
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

func isLegacyDeveloperTaskPolicy(policy taskPermissionPolicy) bool {
	if policy.SchemaVersion != 2 || policy.RootMode != "confirm" || policy.Default != "ask" {
		return false
	}
	expected, err := permissionPolicyForMode("developer")
	if err != nil {
		return false
	}
	broad := make([]permissionRule, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.TargetHash == "" {
			broad = append(broad, rule)
		}
	}
	candidates := [][]permissionRule{expected.Rules}
	withoutNetwork := make([]permissionRule, 0, len(expected.Rules)-1)
	for _, rule := range expected.Rules {
		if rule.Tool != "network" {
			withoutNetwork = append(withoutNetwork, rule)
		}
	}
	candidates = append(candidates, withoutNetwork)
	for _, candidate := range candidates {
		if len(broad) != len(candidate) {
			continue
		}
		matches := true
		for index := range broad {
			if broad[index].Tool != candidate[index].Tool || broad[index].Action != candidate[index].Action {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func hasPermissionRule(policy taskPermissionPolicy, tool string) bool {
	for _, rule := range policy.Rules {
		if rule.Tool == tool && rule.TargetHash == "" {
			return true
		}
	}
	return false
}

func insertPermissionRuleAfter(rules []permissionRule, tool string, inserted permissionRule) []permissionRule {
	result := make([]permissionRule, 0, len(rules)+1)
	for _, rule := range rules {
		result = append(result, rule)
		if rule.TargetHash == "" && rule.Tool == tool {
			result = append(result, inserted)
		}
	}
	return result
}
