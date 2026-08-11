package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultTaskPermissionMode = "developer"

type permissionRule struct {
	Tool   string `json:"tool"`
	Action string `json:"action"`
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
			permissionRule{Tool: "write", Action: "ask"},
			permissionRule{Tool: "edit", Action: "ask"},
			permissionRule{Tool: "bash", Action: "ask"},
			permissionRule{Tool: "quality_gate", Action: "ask"},
			permissionRule{Tool: "memory_save", Action: "ask"},
			permissionRule{Tool: "goal_complete", Action: "ask"},
			permissionRule{Tool: "mcp:*", Action: "ask"},
		)
	case "developer":
		policy.Rules = append(policy.Rules,
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
	return filepath.Join(current.dir, "permissions.json")
}

func (current *task) writePermissionPolicy(mode string) error {
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
	_, err := privateRegularFileInfo(current.permissionPolicyPath(), maxRequestBytes)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return current.writePermissionPolicy(mode)
}
