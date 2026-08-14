package main

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	maximumPiResourceEntries = 64
	maximumPiScanNodes       = 256
	maximumPiScanDepth       = 8
)

type piResourceSource struct {
	name    string
	status  string
	message string
	entries []extensionEntry
}

type piResourceSpec struct {
	kind        string
	runtime     string
	description string
	permissions []string
}

var piResourceSpecs = map[string]piResourceSpec{
	"extension": {kind: "extension", runtime: "pi-extension", description: "Pi extension discovered from a controlled resource directory.", permissions: []string{"current-user", "workspace"}},
	"skill":     {kind: "skill", runtime: "pi-skill", description: "Pi skill discovered from a controlled resource directory.", permissions: []string{"model-context", "agent-tools"}},
	"prompt":    {kind: "prompt", runtime: "pi-prompt", description: "Pi prompt template discovered from a controlled resource directory.", permissions: []string{"model-context"}},
	"theme":     {kind: "theme", runtime: "pi-theme", description: "Pi terminal theme discovered from a controlled resource directory.", permissions: []string{"tui"}},
}

func discoverPiResourceSources(paths configuredExtensionPathSet, context *extensionInventoryContext) []piResourceSource {
	agentDir := filepath.Clean(paths.agentDir)
	sources := []piResourceSource{
		piSettingsSource("pi-settings", filepath.Join(agentDir, "settings.json"), "user", true, agentDir),
		piDirectorySource("user-extensions", filepath.Join(agentDir, "extensions"), "user", "extension"),
		piDirectorySource("user-skills", filepath.Join(agentDir, "skills"), "user", "skill"),
		piDirectorySource("user-prompts", filepath.Join(agentDir, "prompts"), "user", "prompt"),
		piDirectorySource("user-themes", filepath.Join(agentDir, "themes"), "user", "theme"),
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if filepath.IsAbs(home) {
		sources = append(sources, piDirectorySource("shared-skills", filepath.Join(home, ".agents", "skills"), "user", "skill"))
	} else {
		sources = append(sources, piResourceSource{name: "shared-skills", status: "missing", message: piResourceDiagnosticMessage("shared-skills", "missing")})
	}

	if context == nil {
		return append(sources, piResourceSource{name: "project-resources", status: "contextual", message: piResourceDiagnosticMessage("project-resources", "contextual")})
	}
	if !context.ProjectTrusted {
		return append(sources, piResourceSource{name: "project-resources", status: "untrusted", message: piResourceDiagnosticMessage("project-resources", "untrusted")})
	}
	if !filepath.IsAbs(context.Cwd) {
		return append(sources, piResourceSource{name: "project-resources", status: "unsafe", message: piResourceDiagnosticMessage("project-resources", "unsafe")})
	}

	cwd, status := trustedProjectInventoryRoot(context.Cwd)
	if status != "ok" {
		return append(sources, piResourceSource{name: "project-resources", status: status, message: piResourceDiagnosticMessage("project-resources", status)})
	}
	piDir := filepath.Join(cwd, ".pi")
	if info, err := os.Lstat(piDir); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedInventoryInfo(info)) {
		return append(sources, piResourceSource{name: "project-resources", status: "unsafe", message: piResourceDiagnosticMessage("project-resources", "unsafe")})
	} else if err != nil && !os.IsNotExist(err) {
		return append(sources, piResourceSource{name: "project-resources", status: "unreadable", message: piResourceDiagnosticMessage("project-resources", "unreadable")})
	}
	sources = append(sources,
		piSettingsSource("project-settings", filepath.Join(piDir, "settings.json"), "project", false, agentDir),
		piDirectorySource("project-extensions", filepath.Join(piDir, "extensions"), "project", "extension"),
		piDirectorySource("project-skills", filepath.Join(piDir, "skills"), "project", "skill"),
		piDirectorySource("project-prompts", filepath.Join(piDir, "prompts"), "project", "prompt"),
		piDirectorySource("project-themes", filepath.Join(piDir, "themes"), "project", "theme"),
		projectSharedSkillsSource(cwd),
	)
	return sources
}

func piSettingsSource(name, path, scope string, private bool, agentDir string) piResourceSource {
	var document map[string]any
	var status string
	if private {
		document, status = readPrivateInventoryConfig(path)
	} else {
		document, status = readProjectInventoryConfig(path)
	}
	if status != "ok" {
		return piResourceSource{name: name, status: status, message: piResourceDiagnosticMessage(name, status)}
	}
	entries, valid := configuredPiResources(document, scope, agentDir)
	if !valid {
		return piResourceSource{name: name, status: "invalid", message: piResourceDiagnosticMessage(name, "invalid")}
	}
	return piResourceSource{name: name, status: "ok", message: piResourceDiagnosticMessage(name, "ok"), entries: entries}
}

func configuredPiResources(document map[string]any, scope, agentDir string) ([]extensionEntry, bool) {
	entries := make([]extensionEntry, 0)
	for _, declaration := range []struct {
		field        string
		resourceType string
	}{
		{field: "extensions", resourceType: "extension"},
		{field: "skills", resourceType: "skill"},
		{field: "prompts", resourceType: "prompt"},
		{field: "themes", resourceType: "theme"},
	} {
		raw, exists := document[declaration.field]
		if !exists {
			continue
		}
		values, ok := raw.([]any)
		if !ok || len(values) > maximumPiResourceEntries {
			return nil, false
		}
		for index, value := range values {
			path, ok := value.(string)
			if !ok || !safeInventoryLabel(path, 2048) {
				return nil, false
			}
			if builtInPiResourcePath(path) || automaticPiResourcePath(path, agentDir, declaration.resourceType) {
				continue
			}
			entries = append(entries, declaredPiResource(scope, declaration.resourceType, index+1))
		}
	}

	if raw, exists := document["packages"]; exists {
		packages, ok := raw.([]any)
		if !ok || len(packages) > maximumPiResourceEntries {
			return nil, false
		}
		for index, rawPackage := range packages {
			source, ok := piPackageSource(rawPackage)
			if !ok {
				return nil, false
			}
			entries = append(entries, declaredPiPackage(scope, source, index+1))
		}
	}
	return entries, true
}

func piPackageSource(raw any) (string, bool) {
	if source, ok := raw.(string); ok {
		return source, safeInventoryLabel(source, 2048)
	}
	entry, ok := raw.(map[string]any)
	if !ok || len(entry) == 0 || len(entry) > 8 {
		return "", false
	}
	source, ok := entry["source"].(string)
	return source, ok && safeInventoryLabel(source, 2048)
}

func declaredPiResource(scope, resourceType string, index int) extensionEntry {
	spec := piResourceSpecs[resourceType]
	ordinal := strconv.Itoa(index)
	return extensionEntry{
		ID: "pi." + scope + "." + resourceType + ".declared-" + ordinal, Name: titleResource(resourceType) + " declaration " + ordinal,
		Version: "declared", Kind: spec.kind, ResourceType: resourceType, Description: "Additional Pi " + resourceType + " path declared in settings.",
		Origin: scope, Scope: scope, Runtime: spec.runtime, Entrypoint: scope + "/settings.json#" + resourceType + "s-" + ordinal,
		Trust: scope, DefaultEnabled: true, Provides: []string{"pi." + resourceType}, Requires: []string{}, Permissions: append([]string(nil), spec.permissions...), Targets: []string{},
		Status: "declared", StatusDetail: "Declared by settings; inventory does not claim that Pi loaded it",
	}
}

func declaredPiPackage(scope, source string, index int) extensionEntry {
	ordinal := strconv.Itoa(index)
	name := safePiPackageName(source)
	if name == "" {
		name = "Configured package " + ordinal
	}
	return extensionEntry{
		ID: "pi." + scope + ".package." + inventorySlug(name), Name: name, Version: "configured", Kind: "package", ResourceType: "package",
		Description: "Pi package declared in settings. Installation and load state are not inferred.", Origin: "package", Scope: scope, Runtime: "pi-package",
		Entrypoint: scope + "/settings.json#packages-" + ordinal, Trust: "third-party", DefaultEnabled: true,
		Provides: []string{"pi.package"}, Requires: []string{}, Permissions: []string{"current-user", "workspace", "model-network"}, Targets: []string{},
		Status: "declared", StatusDetail: "Declared by settings; package code may run with the current user authority",
	}
}

func safePiPackageName(source string) string {
	source = strings.TrimSpace(source)
	if parsed, err := url.Parse(source); err == nil && parsed.Scheme != "" {
		source = filepath.Base(strings.TrimSuffix(parsed.Path, "/"))
	} else if strings.Contains(source, "/") && !strings.HasPrefix(source, "@") {
		source = filepath.Base(strings.TrimSuffix(strings.SplitN(source, "#", 2)[0], "/"))
	}
	source = strings.TrimSuffix(source, ".git")
	if safeInventoryLabel(source, 120) {
		return source
	}
	return ""
}

func builtInPiResourcePath(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/usr/local/lib/hobot-code/extensions" || clean == "/usr/local/lib/hobot-code/skills"
}

func automaticPiResourcePath(path, agentDir, resourceType string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	return filepath.Clean(path) == filepath.Join(filepath.Clean(agentDir), resourceType+"s")
}

func piDirectorySource(name, root, scope, resourceType string) piResourceSource {
	entries, status := scanPiResourceDirectory(root, scope, resourceType)
	return piResourceSource{name: name, status: status, message: piResourceDiagnosticMessage(name, status), entries: entries}
}

func scanPiResourceDirectory(root, scope, resourceType string) ([]extensionEntry, string) {
	if !filepath.IsAbs(root) {
		return nil, "unsafe"
	}
	if resourceType == "skill" {
		return scanPiSkills(root, scope)
	}
	directory, status := openOwnedInventoryDirectory(root)
	if status != "ok" {
		return nil, status
	}
	defer directory.Close()
	children, err := directory.ReadDir(maximumPiScanNodes + 1)
	if err != nil {
		return nil, "unreadable"
	}
	truncated := len(children) > maximumPiScanNodes
	if truncated {
		children = children[:maximumPiScanNodes]
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	entries := make([]extensionEntry, 0)
	partial := false
	for _, child := range children {
		if len(entries) >= maximumPiResourceEntries {
			truncated = true
			break
		}
		name := child.Name()
		if !safeResourceBasename(name) {
			partial = true
			continue
		}
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil || !ownedInventoryInfo(info) || info.Mode()&os.ModeSymlink != 0 {
			partial = true
			continue
		}
		relative := name
		if resourceType == "extension" && info.IsDir() {
			indexName := "index.ts"
			indexInfo, err := os.Lstat(filepath.Join(path, indexName))
			if err != nil {
				indexName = "index.js"
				indexInfo, err = os.Lstat(filepath.Join(path, indexName))
			}
			if err != nil || !ownedRegularInventoryInfo(indexInfo) {
				continue
			}
			relative = filepath.Join(name, indexName)
		} else if !info.Mode().IsRegular() || !piResourceFileMatches(name, resourceType) {
			continue
		}
		entries = append(entries, discoveredPiResource(scope, resourceType, relative))
	}
	if truncated {
		return entries, "truncated"
	}
	if partial {
		return entries, "partial"
	}
	return entries, "ok"
}

func scanPiSkills(root, scope string) ([]extensionEntry, string) {
	type pendingDirectory struct {
		path  string
		depth int
	}
	queue := []pendingDirectory{{path: root}}
	entries := make([]extensionEntry, 0)
	visited := 0
	partial := false
	for len(queue) > 0 {
		if visited >= maximumPiScanNodes || len(entries) >= maximumPiResourceEntries {
			return entries, "truncated"
		}
		current := queue[0]
		queue = queue[1:]
		directory, status := openOwnedInventoryDirectory(current.path)
		if status != "ok" {
			if current.depth == 0 {
				return nil, status
			}
			partial = true
			continue
		}
		children, err := directory.ReadDir(maximumPiScanNodes - visited + 1)
		_ = directory.Close()
		if err != nil {
			if current.depth == 0 {
				return nil, "unreadable"
			}
			partial = true
			continue
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			visited++
			if visited > maximumPiScanNodes || len(entries) >= maximumPiResourceEntries {
				return entries, "truncated"
			}
			if !safeResourceBasename(child.Name()) {
				partial = true
				continue
			}
			path := filepath.Join(current.path, child.Name())
			info, err := os.Lstat(path)
			if err != nil || !ownedInventoryInfo(info) || info.Mode()&os.ModeSymlink != 0 {
				partial = true
				continue
			}
			if info.IsDir() && current.depth < maximumPiScanDepth {
				queue = append(queue, pendingDirectory{path: path, depth: current.depth + 1})
				continue
			}
			if info.Mode().IsRegular() && (child.Name() == "SKILL.md" || (current.depth == 0 && strings.EqualFold(filepath.Ext(child.Name()), ".md"))) {
				relative, err := filepath.Rel(root, path)
				if err == nil {
					entries = append(entries, discoveredPiResource(scope, "skill", relative))
				}
			}
		}
	}
	if partial {
		return entries, "partial"
	}
	return entries, "ok"
}

func discoveredPiResource(scope, resourceType, relative string) extensionEntry {
	spec := piResourceSpecs[resourceType]
	cleanRelative := filepath.ToSlash(filepath.Clean(relative))
	name := filepath.Base(filepath.Dir(cleanRelative))
	if resourceType != "skill" || name == "." {
		name = strings.TrimSuffix(filepath.Base(cleanRelative), filepath.Ext(cleanRelative))
	}
	if name == "index" {
		name = filepath.Base(filepath.Dir(cleanRelative))
	}
	if !safeInventoryLabel(name, 120) {
		name = titleResource(resourceType)
	}
	return extensionEntry{
		ID: "pi." + scope + "." + resourceType + "." + inventorySlug(cleanRelative), Name: name, Version: "unversioned", Kind: spec.kind, ResourceType: resourceType,
		Description: spec.description, Origin: scope, Scope: scope, Runtime: spec.runtime, Entrypoint: scope + "/" + resourceType + "s/" + cleanRelative,
		Trust: scope, DefaultEnabled: true, Provides: []string{"pi." + resourceType}, Requires: []string{}, Permissions: append([]string(nil), spec.permissions...), Targets: []string{},
		Status: "discovered", StatusDetail: "Discovered by bounded read-only inventory; execution state is not asserted",
	}
}

func projectSharedSkillsSource(cwd string) piResourceSource {
	entries := make([]extensionEntry, 0)
	status := "missing"
	globalSharedSkills := ""
	if home := strings.TrimSpace(os.Getenv("HOME")); filepath.IsAbs(home) {
		globalSharedSkills = filepath.Clean(filepath.Join(home, ".agents", "skills"))
	}
	for _, root := range projectSharedSkillRoots(cwd) {
		if globalSharedSkills != "" && filepath.Clean(root) == globalSharedSkills {
			continue
		}
		agentsDir := filepath.Dir(root)
		if info, err := os.Lstat(agentsDir); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedInventoryInfo(info)) {
			if status == "missing" {
				status = "unsafe"
			}
			continue
		} else if err != nil && !os.IsNotExist(err) {
			if status == "missing" {
				status = "unreadable"
			}
			continue
		}
		discovered, currentStatus := scanPiResourceDirectory(root, "project", "skill")
		if currentStatus == "ok" || currentStatus == "partial" || currentStatus == "truncated" {
			status = currentStatus
			entries = append(entries, discovered...)
			if len(entries) >= maximumPiResourceEntries || currentStatus == "truncated" {
				status = "truncated"
				entries = entries[:minInt(len(entries), maximumPiResourceEntries)]
				break
			}
		} else if currentStatus != "missing" && status == "missing" {
			status = currentStatus
		}
	}
	return piResourceSource{name: "project-shared-skills", status: status, message: piResourceDiagnosticMessage("project-shared-skills", status), entries: entries}
}

func trustedProjectInventoryRoot(cwd string) (string, string) {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return "", "unreadable"
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", "unreadable"
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedInventoryInfo(info) {
		return "", "unsafe"
	}
	return resolved, "ok"
}

func projectSharedSkillRoots(cwd string) []string {
	roots := make([]string, 0, 8)
	current := filepath.Clean(cwd)
	for depth := 0; depth < 16; depth++ {
		roots = append(roots, filepath.Join(current, ".agents", "skills"))
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return roots
}

func readProjectInventoryConfig(path string) (map[string]any, string) {
	if !filepath.IsAbs(path) {
		return nil, "unsafe"
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, "missing"
	}
	if err != nil {
		return nil, "unreadable"
	}
	if !ownedRegularInventoryInfo(info) || info.Size() <= 0 || info.Size() > maximumInventoryConfigBytes {
		return nil, "unsafe"
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "unreadable"
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || !ownedRegularInventoryInfo(openedInfo) {
		return nil, "unsafe"
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumInventoryConfigBytes+1))
	if err != nil {
		return nil, "unreadable"
	}
	if len(raw) == 0 || len(raw) > maximumInventoryConfigBytes {
		return nil, "unsafe"
	}
	var document map[string]any
	if rejectDuplicateJSONKeys(string(raw)) != nil || json.Unmarshal(raw, &document) != nil || document == nil {
		return nil, "invalid"
	}
	return document, "ok"
}

func openOwnedInventoryDirectory(path string) (*os.File, string) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, "missing"
	}
	if err != nil {
		return nil, "unreadable"
	}
	if !ownedInventoryInfo(info) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "unsafe"
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, "unreadable"
	}
	openedInfo, err := directory.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || !ownedInventoryInfo(openedInfo) || !openedInfo.IsDir() {
		_ = directory.Close()
		return nil, "unsafe"
	}
	return directory, "ok"
}

func ownedRegularInventoryInfo(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0o022 == 0 && ownedInventoryInfo(info)
}

func ownedInventoryInfo(info os.FileInfo) bool {
	if info == nil || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || int(stat.Uid) == os.Getuid()
}

func safeResourceBasename(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && safeInventoryLabel(name, 255)
}

func piResourceFileMatches(name, resourceType string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	switch resourceType {
	case "extension":
		return extension == ".ts" || extension == ".js"
	case "prompt":
		return extension == ".md"
	case "theme":
		return extension == ".json"
	default:
		return false
	}
}

func titleResource(resourceType string) string {
	if resourceType == "prompt" {
		return "Prompt"
	}
	if resourceType == "theme" {
		return "Theme"
	}
	if resourceType == "skill" {
		return "Skill"
	}
	return "Extension"
}

func piResourceDiagnosticMessage(source, status string) string {
	label := strings.NewReplacer("-", " ").Replace(source)
	switch status {
	case "ok":
		return label + " inspected"
	case "missing":
		return label + " is not configured"
	case "contextual":
		return "Project resources require a selected task"
	case "untrusted":
		return "Project resources were not inspected because the task has not trusted them"
	case "unsafe":
		return label + " failed ownership, type, or write-permission checks"
	case "unreadable":
		return label + " could not be read"
	case "partial":
		return label + " contains entries that were skipped by safety checks"
	case "truncated":
		return label + " reached its bounded inventory limit"
	default:
		return label + " is invalid"
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
