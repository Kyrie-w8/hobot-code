package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maximumOpenExplorerSkills     = 32
	maximumOpenExplorerSkillItems = maximumOpenExplorerSkills * 2
	maximumOpenExplorerSkillBytes = 512 * 1024
	maximumOpenExplorerCatalog    = 256 * 1024
)

var openExplorerCatalogRowPattern = regexp.MustCompile(`^\|\s*` + "`" + `([a-z0-9][a-z0-9-]{0,63})` + "`" + `\s*\|`)

type openExplorerSkillRecord struct {
	Name        string
	Description string
	Version     string
	Path        string
	Cataloged   bool
	NeedsBuild  bool
	NeedsCUDA   bool
}

type openExplorerSkillPack struct {
	Root          string
	SkillsRoot    string
	Version       string
	Skills        []openExplorerSkillRecord
	CatalogCount  int
	DisabledCount int
	Status        string
	Message       string
}

func inspectConfiguredOpenExplorerSkillPack() (openExplorerSkillPack, bool) {
	root := strings.TrimSpace(os.Getenv(openExplorerRuntimeEnv))
	if root == "" {
		return openExplorerSkillPack{}, false
	}
	pack := openExplorerSkillPack{Root: filepath.Clean(root), Status: "invalid", Message: "OpenExplorer LLM Skill Pack is invalid"}
	if !filepath.IsAbs(root) {
		pack.Status = "unsafe"
		pack.Message = "OpenExplorer LLM Skill Pack root must be an absolute, owner-controlled directory"
		return pack, true
	}
	version, status := openExplorerRuntimeVersion(filepath.Join(root, "oellm_runtime", "include", "oellm_runtime_basic", "oellm_runtime_version.h"))
	if status != "ok" {
		pack.Status = status
		pack.Message = "OpenExplorer LLM Skill Pack cannot be bound to a verified package version"
		return pack, true
	}
	pack.Version = version
	pack.SkillsRoot = filepath.Join(pack.Root, ".skillshare", "skills")
	for _, directory := range []string{pack.Root, filepath.Join(pack.Root, ".skillshare"), pack.SkillsRoot} {
		handle, currentStatus := openOwnedInventoryDirectory(directory)
		if currentStatus != "ok" {
			pack.Status = currentStatus
			pack.Message = openExplorerSkillDiagnosticMessage(currentStatus)
			return pack, true
		}
		_ = handle.Close()
	}

	catalog, currentStatus := readOpenExplorerSkillCatalog(filepath.Join(pack.Root, "docs", "03_SKILLS_CATALOG.md"))
	if currentStatus != "ok" {
		pack.Status = currentStatus
		pack.Message = openExplorerSkillDiagnosticMessage(currentStatus)
		return pack, true
	}
	pack.CatalogCount = len(catalog)

	directory, currentStatus := openOwnedInventoryDirectory(pack.SkillsRoot)
	if currentStatus != "ok" {
		pack.Status = currentStatus
		pack.Message = openExplorerSkillDiagnosticMessage(currentStatus)
		return pack, true
	}
	children, err := directory.ReadDir(maximumOpenExplorerSkillItems + 1)
	_ = directory.Close()
	if err != nil {
		pack.Status = "unreadable"
		pack.Message = openExplorerSkillDiagnosticMessage(pack.Status)
		return pack, true
	}
	if len(children) == 0 || len(children) > maximumOpenExplorerSkillItems {
		pack.Status = "truncated"
		pack.Message = "OpenExplorer LLM Skill Pack exceeds the supported skill count"
		return pack, true
	}
	sort.Slice(children, func(left, right int) bool { return children[left].Name() < children[right].Name() })
	found := make(map[string]bool, len(children))
	for _, child := range children {
		name := child.Name()
		if openExplorerMetadataEntry(name, child.Type()) {
			continue
		}
		if !safeResourceBasename(name) || !validSkillName(name) {
			pack.Status = "unsafe"
			pack.Message = "OpenExplorer LLM Skill Pack contains an unsafe skill name"
			return pack, true
		}
		directoryPath := filepath.Join(pack.SkillsRoot, name)
		info, err := os.Lstat(directoryPath)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedInventoryInfo(info) {
			pack.Status = "unsafe"
			pack.Message = "OpenExplorer LLM Skill Pack contains an unsafe skill directory"
			return pack, true
		}
		skillPath := filepath.Join(directoryPath, "SKILL.md")
		metadata, currentStatus := readOpenExplorerSkillMetadata(skillPath)
		if currentStatus != "ok" || metadata.Name != name {
			pack.Status = currentStatus
			if currentStatus == "ok" {
				pack.Status = "invalid"
			}
			pack.Message = "OpenExplorer LLM Skill Pack contains invalid or mismatched Skill metadata"
			return pack, true
		}
		metadata.Path = skillPath
		metadata.Cataloged = catalog[name]
		metadata.NeedsBuild, metadata.NeedsCUDA = openExplorerSkillExecutionRequirements(name)
		if !metadata.Cataloged {
			pack.DisabledCount++
		}
		found[name] = true
		pack.Skills = append(pack.Skills, metadata)
	}
	if len(pack.Skills) == 0 || len(pack.Skills) > maximumOpenExplorerSkills {
		pack.Status = "truncated"
		pack.Message = "OpenExplorer LLM Skill Pack exceeds the supported skill count"
		return pack, true
	}
	for name := range catalog {
		if !found[name] {
			pack.Status = "invalid"
			pack.Message = "OpenExplorer LLM customer catalog references a missing Skill"
			return pack, true
		}
	}
	pack.Status = "ok"
	if pack.DisabledCount > 0 || len(pack.Skills) != pack.CatalogCount {
		pack.Status = "partial"
	}
	pack.Message = fmt.Sprintf("OpenExplorer LLM %s Skill Pack inspected: %d found, %d cataloged, %d disabled", pack.Version, len(pack.Skills), pack.CatalogCount, pack.DisabledCount)
	return pack, true
}

func openExplorerMetadataEntry(name string, mode os.FileMode) bool {
	return mode.IsRegular() && (name == ".DS_Store" || strings.HasPrefix(name, "._"))
}

func readOpenExplorerSkillCatalog(path string) (map[string]bool, string) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, "missing"
	}
	if err != nil {
		return nil, "unreadable"
	}
	if !ownedRegularInventoryInfo(info) || info.Size() <= 0 || info.Size() > maximumOpenExplorerCatalog {
		return nil, "unsafe"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "unreadable"
	}
	result := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		match := openExplorerCatalogRowPattern.FindStringSubmatch(scanner.Text())
		if len(match) != 2 {
			continue
		}
		if result[match[1]] {
			return nil, "invalid"
		}
		result[match[1]] = true
	}
	if scanner.Err() != nil {
		return nil, "unreadable"
	}
	if len(result) == 0 || len(result) > maximumOpenExplorerSkills {
		return nil, "invalid"
	}
	return result, "ok"
}

func readOpenExplorerSkillMetadata(path string) (openExplorerSkillRecord, string) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return openExplorerSkillRecord{}, "missing"
	}
	if err != nil {
		return openExplorerSkillRecord{}, "unreadable"
	}
	if !ownedRegularInventoryInfo(info) || info.Size() <= 0 || info.Size() > maximumOpenExplorerSkillBytes {
		return openExplorerSkillRecord{}, "unsafe"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return openExplorerSkillRecord{}, "unreadable"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "---" {
		return openExplorerSkillRecord{}, "invalid"
	}
	end := -1
	for index := 1; index < len(lines) && index <= 80; index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			end = index
			break
		}
	}
	if end < 2 {
		return openExplorerSkillRecord{}, "invalid"
	}
	values := make(map[string]string)
	for index := 1; index < end; index++ {
		line := lines[index]
		separator := strings.Index(line, ":")
		if separator < 1 || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if value == ">" || value == ">-" || value == "|" || value == "|-" {
			parts := make([]string, 0)
			for index+1 < end {
				next := lines[index+1]
				if next != "" && next[0] != ' ' && next[0] != '\t' {
					break
				}
				index++
				if trimmed := strings.TrimSpace(next); trimmed != "" {
					parts = append(parts, trimmed)
				}
			}
			value = strings.Join(parts, " ")
		}
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = strings.TrimSpace(value)
	}
	name := values["name"]
	description := values["description"]
	version := values["version"]
	if version == "" {
		version = "unversioned"
	}
	if !validSkillName(name) || !safeInventoryLabel(description, 1024) || !safeInventoryLabel(version, 64) {
		return openExplorerSkillRecord{}, "invalid"
	}
	return openExplorerSkillRecord{Name: name, Description: description, Version: version}, "ok"
}

func validSkillName(value string) bool {
	if value == "" || len(value) > 64 || value[0] == '-' || value[len(value)-1] == '-' || strings.Contains(value, "--") {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return true
}

func openExplorerSkillExecutionRequirements(name string) (bool, bool) {
	switch name {
	case "skill-creator", "skillshare-manage", "s600-model-adapt-research":
		return false, false
	case "model-type-identify":
		return true, false
	default:
		return true, true
	}
}

func openExplorerSkillEntries(pack openExplorerSkillPack) []extensionEntry {
	if pack.Status != "ok" && pack.Status != "partial" {
		return nil
	}
	entries := make([]extensionEntry, 0, len(pack.Skills)+1)
	entries = append(entries, extensionEntry{
		ID: "external.openexplorer-llm.skills", Name: "OpenExplorer LLM Skill Pack", Version: pack.Version, Kind: "package", ResourceType: "package",
		Description: "Official external OpenExplorer LLM customer Skill Pack. Skills remain in the user-supplied package and are not redistributed by Hobot Code.",
		Origin:      "user", Scope: "user", Runtime: "pi-package", Entrypoint: "openexplorer-llm/.skillshare/skills", Trust: "third-party", DefaultEnabled: true,
		Provides: []string{"skills.openexplorer-llm"}, Requires: []string{"hobot.rdk-core"}, Permissions: []string{"model-context", "agent-tools", "subprocess", "model-network"}, Targets: []string{"s600"}, Status: "available",
		StatusDetail: fmt.Sprintf("%d Skills discovered; %d listed by the official customer catalog; vendor test evidence was not supplied", len(pack.Skills), pack.CatalogCount),
	})
	for _, skill := range pack.Skills {
		requires := []string{"skills.openexplorer-llm"}
		permissions := []string{"model-context", "agent-tools"}
		if skill.NeedsBuild {
			requires = append(requires, "builder.linux-x86-64")
			permissions = append(permissions, "subprocess", "model-network")
		}
		if skill.NeedsCUDA {
			requires = append(requires, "builder.cuda")
		}
		status := "available"
		detail := "Listed in the official customer catalog; vendor test status was not supplied"
		if !skill.Cataloged {
			status = "disabled"
			detail = "Present in the package but absent from the official 23-Skill customer catalog; disabled by default"
		}
		entries = append(entries, extensionEntry{
			ID: "external.openexplorer-llm.skill." + skill.Name, Name: skill.Name, Version: skill.Version, Kind: "skill", ResourceType: "skill",
			Description: boundedOpenExplorerInventoryText(skill.Description, 512), Origin: "user", Scope: "user", Runtime: "pi-skill", Entrypoint: "openexplorer-llm/.skillshare/skills/" + skill.Name + "/SKILL.md",
			Trust: "third-party", DefaultEnabled: skill.Cataloged, Provides: []string{"skill.openexplorer-llm." + skill.Name}, Requires: requires,
			Permissions: permissions, Targets: []string{"s600"}, Status: status, StatusDetail: detail,
		})
	}
	return entries
}

func boundedOpenExplorerInventoryText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	limit := maximum - 3
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return strings.TrimSpace(value[:limit]) + "..."
}

func configuredOpenExplorerSkillPaths() ([]string, string, error) {
	pack, configured := inspectConfiguredOpenExplorerSkillPack()
	if !configured || (pack.Status != "ok" && pack.Status != "partial") {
		return nil, "", nil
	}
	paths := make([]string, 0, pack.CatalogCount)
	for _, skill := range pack.Skills {
		if skill.Cataloged {
			paths = append(paths, skill.Path)
		}
	}
	sort.Strings(paths)
	if len(paths) != pack.CatalogCount {
		return nil, "", fmt.Errorf("OpenExplorer LLM Skill Pack catalog and loadable paths differ")
	}
	return paths, pack.SkillsRoot, nil
}

func mergeOpenExplorerSkillsIntoSettings(content []byte) ([]byte, error) {
	paths, _, err := configuredOpenExplorerSkillPaths()
	if err != nil || len(paths) == 0 {
		return content, err
	}
	settings := make(map[string]any)
	if len(content) > 0 {
		if err := json.Unmarshal(content, &settings); err != nil {
			return nil, fmt.Errorf("parse settings.json for OpenExplorer LLM Skills: %w", err)
		}
	}
	rawSkills, exists := settings["skills"]
	skills := make([]any, 0, len(paths)+4)
	seen := make(map[string]bool, len(paths)+4)
	if exists {
		values, ok := rawSkills.([]any)
		if !ok {
			return nil, fmt.Errorf("settings.json skills must be an array")
		}
		for _, raw := range values {
			value, ok := raw.(string)
			if !ok || !safeInventoryLabel(value, 2048) {
				return nil, fmt.Errorf("settings.json contains an invalid skill path")
			}
			if !seen[value] {
				skills = append(skills, value)
				seen[value] = true
			}
		}
	}
	for _, path := range paths {
		if !seen[path] {
			skills = append(skills, path)
			seen[path] = true
		}
	}
	if len(skills) > maximumPiResourceEntries {
		return nil, fmt.Errorf("settings.json exceeds the supported skill path count after OpenExplorer LLM import")
	}
	settings["skills"] = skills
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func openExplorerSkillDiagnosticMessage(status string) string {
	switch status {
	case "missing":
		return "Configured OpenExplorer LLM package does not contain the official Skill Pack"
	case "unsafe":
		return "OpenExplorer LLM Skill Pack failed ownership, type, path, or size checks"
	case "unreadable":
		return "OpenExplorer LLM Skill Pack could not be inspected"
	case "truncated":
		return "OpenExplorer LLM Skill Pack exceeds bounded inventory limits"
	default:
		return "OpenExplorer LLM Skill Pack metadata or customer catalog is invalid"
	}
}
