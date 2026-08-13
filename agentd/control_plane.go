package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maximumWorkspaceEntries = 256

var modelProviderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+/-]{0,255}$`)
var workspaceNamePattern = regexp.MustCompile(`^[^/\x00]{1,128}$`)

type modelOption struct {
	Provider         string            `json:"provider"`
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Default          bool              `json:"default,omitempty"`
	Capabilities     modelCapabilities `json:"capabilities"`
	CapabilitySource string            `json:"capabilitySource"`
}

type modelCapabilities struct {
	Reasoning  bool `json:"reasoning"`
	ImageInput bool `json:"imageInput"`
}

type workspaceParams struct {
	Path string `json:"path,omitempty"`
}

type createWorkspaceParams struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

type workspaceEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type workspaceListing struct {
	Path        string           `json:"path"`
	Parent      string           `json:"parent,omitempty"`
	Home        string           `json:"home"`
	Directories []workspaceEntry `json:"directories"`
}

func normalizeModelSelection(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 || !modelProviderPattern.MatchString(parts[0]) || !modelIDPattern.MatchString(parts[1]) {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func joinModel(provider, id string) string {
	if !modelProviderPattern.MatchString(provider) || !modelIDPattern.MatchString(id) {
		return ""
	}
	return provider + "/" + id
}

func listModels(cfg config) ([]modelOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, cfg.AgentBinary, "--offline", "--list-models")
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("model discovery timed out")
		}
		return nil, fmt.Errorf("model discovery failed: %w", err)
	}
	models := parseModelTable(output)
	if len(models) == 0 {
		return nil, fmt.Errorf("model discovery returned no models")
	}
	markDefaultModel(models)
	return models, nil
}

func parseModelTable(output []byte) []modelOption {
	models := make([]modelOption, 0)
	seen := make(map[string]bool)
	columns := make(map[string]int)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "provider" && fields[1] == "model" {
			for index, field := range fields {
				columns[field] = index
			}
			continue
		}
		if len(fields) < 2 || !modelProviderPattern.MatchString(fields[0]) || !modelIDPattern.MatchString(fields[1]) {
			continue
		}
		key := fields[0] + "/" + fields[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		capabilities := modelCapabilities{}
		capabilitySource := "conservative-default"
		if thinkingColumn, ok := columns["thinking"]; ok && thinkingColumn < len(fields) {
			capabilities.Reasoning = strings.EqualFold(fields[thinkingColumn], "yes")
			capabilitySource = "runtime-model-table"
		}
		if imageColumn, ok := columns["images"]; ok && imageColumn < len(fields) {
			capabilities.ImageInput = strings.EqualFold(fields[imageColumn], "yes")
			capabilitySource = "runtime-model-table"
		}
		models = append(models, modelOption{
			Provider: fields[0], ID: fields[1], Name: fields[1], Capabilities: capabilities, CapabilitySource: capabilitySource,
		})
	}
	return models
}

func markDefaultModel(models []modelOption) {
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if model == "" {
		model = "kimi-k3"
	}
	selection := normalizeModelSelection(model)
	if selection == "" {
		selection = joinModel("drobotics", model)
	}
	for index := range models {
		if joinModel(models[index].Provider, models[index].ID) == selection {
			models[index].Default = true
			return
		}
	}
	// Gateway model groups may contain a slash while still belonging to the
	// D-Robotics provider, for example deepseek/deepseek-v4-flash.
	droboticsSelection := joinModel("drobotics", model)
	for index := range models {
		if joinModel(models[index].Provider, models[index].ID) == droboticsSelection {
			models[index].Default = true
			return
		}
	}
	for index := range models {
		if models[index].Provider == "drobotics" && models[index].ID == "kimi-k3" {
			models[index].Default = true
			return
		}
	}
	for index := range models {
		if models[index].Provider == "drobotics" {
			models[index].Default = true
			return
		}
	}
	if len(models) > 0 {
		models[0].Default = true
	}
}

func browseWorkspace(params workspaceParams) (workspaceListing, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return workspaceListing{}, fmt.Errorf("user home directory is unavailable")
	}
	requested := strings.TrimSpace(params.Path)
	if requested == "" {
		requested = home
	}
	path, err := normalizeWorkingDirectory(requested)
	if err != nil {
		return workspaceListing{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return workspaceListing{}, err
	}
	directories := make([]workspaceEntry, 0)
	for _, entry := range entries {
		if len(directories) >= maximumWorkspaceEntries {
			break
		}
		if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() {
			continue
		}
		directories = append(directories, workspaceEntry{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.ToLower(directories[i].Name) < strings.ToLower(directories[j].Name)
	})
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	return workspaceListing{Path: path, Parent: parent, Home: home, Directories: directories}, nil
}

func createWorkspace(params createWorkspaceParams) (workspaceListing, error) {
	parent, err := normalizeWorkingDirectory(params.Parent)
	if err != nil {
		return workspaceListing{}, err
	}
	name := strings.TrimSpace(params.Name)
	if !workspaceNamePattern.MatchString(name) || name == "." || name == ".." || filepath.Base(name) != name {
		return workspaceListing{}, fmt.Errorf("workspace name is invalid")
	}
	target := filepath.Join(parent, name)
	if err := os.Mkdir(target, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return workspaceListing{}, fmt.Errorf("workspace already exists: %s", target)
		}
		return workspaceListing{}, err
	}
	return browseWorkspace(workspaceParams{Path: target})
}

type sessionLine struct {
	Raw      json.RawMessage
	Type     string
	ID       string
	ParentID string
	Role     string
	Text     string
	Stop     string
}

func readSessionLines(path string) (json.RawMessage, []sessionLine, error) {
	content, err := readPrivateRegularFile(path, maxRequestBytes*32)
	if err != nil {
		return nil, nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), maxEventRecordBytes)
	var header json.RawMessage
	lines := make([]sessionLine, 0)
	for scanner.Scan() {
		raw := append(json.RawMessage(nil), scanner.Bytes()...)
		var value struct {
			Type     string `json:"type"`
			ID       string `json:"id"`
			ParentID string `json:"parentId"`
			Message  struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, nil, fmt.Errorf("invalid session entry: %w", err)
		}
		if header == nil {
			if value.Type != "session" {
				return nil, nil, fmt.Errorf("session header is missing")
			}
			header = raw
			continue
		}
		parts := make([]string, 0)
		for _, part := range value.Message.Content {
			if part.Type == "text" && part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		lines = append(lines, sessionLine{Raw: raw, Type: value.Type, ID: value.ID, ParentID: value.ParentID, Role: value.Message.Role, Text: strings.Join(parts, "\n"), Stop: value.Message.StopReason})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if header == nil || len(lines) == 0 {
		return nil, nil, fmt.Errorf("session does not contain a resumable branch")
	}
	return header, lines, nil
}

func sessionBranch(lines []sessionLine, leafID string) ([]sessionLine, error) {
	byID := make(map[string]sessionLine, len(lines))
	for _, line := range lines {
		if line.ID != "" {
			byID[line.ID] = line
		}
	}
	branch := make([]sessionLine, 0)
	seen := make(map[string]bool)
	for leafID != "" {
		if seen[leafID] {
			return nil, fmt.Errorf("session branch contains a cycle")
		}
		seen[leafID] = true
		line, ok := byID[leafID]
		if !ok {
			return nil, fmt.Errorf("session branch parent is missing: %s", leafID)
		}
		branch = append(branch, line)
		leafID = line.ParentID
	}
	for left, right := 0, len(branch)-1; left < right; left, right = left+1, right-1 {
		branch[left], branch[right] = branch[right], branch[left]
	}
	return branch, nil
}

func safeSessionLeaf(lines []sessionLine) string {
	if len(lines) == 0 {
		return ""
	}
	leaf := lines[len(lines)-1]
	unfinished := leaf.Type == "custom_message" || (leaf.Type == "message" && (leaf.Role == "user" || leaf.Role == "toolResult" || (leaf.Role == "assistant" && leaf.Stop == "toolUse")))
	if !unfinished {
		return leaf.ID
	}
	branch, err := sessionBranch(lines, leaf.ID)
	if err != nil {
		return ""
	}
	for index := len(branch) - 1; index >= 0; index-- {
		if branch[index].Type == "custom_message" || (branch[index].Type == "message" && branch[index].Role == "user") {
			return branch[index].ParentID
		}
	}
	return ""
}

func writeSessionFork(sessionDir, sourcePath, cwd, leafID string, lines []sessionLine) (string, error) {
	branch, err := sessionBranch(lines, leafID)
	if err != nil {
		return "", err
	}
	id, err := newTaskID()
	if err != nil {
		return "", err
	}
	header := map[string]any{
		"type": "session", "version": 3, "id": id, "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"cwd": cwd, "parentSession": sourcePath,
	}
	var output bytes.Buffer
	encodedHeader, _ := json.Marshal(header)
	output.Write(encodedHeader)
	output.WriteByte('\n')
	for _, line := range branch {
		output.Write(line.Raw)
		output.WriteByte('\n')
	}
	file, err := os.CreateTemp(sessionDir, ".fork-*.jsonl")
	if err != nil {
		return "", err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := file.Write(output.Bytes()); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	target := filepath.Join(sessionDir, "fork-"+id+".jsonl")
	if err := os.Rename(temporary, target); err != nil {
		return "", err
	}
	return target, nil
}
