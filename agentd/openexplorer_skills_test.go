package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenExplorerSkillPackDiscoversAllAndLoadsOnlyCatalogedSkills(t *testing.T) {
	root := filepath.Join(t.TempDir(), "OpenExplorer_LLM")
	writeOpenExplorerSkillTestPackage(t, root,
		[]string{"cataloged-skill"},
		map[string]string{
			"cataloged-skill": `description: >-
  Cataloged x86 CUDA workflow for S600 deployment.
  Ask for model and output paths before running.`,
			"extra-skill": "description: Extra package Skill that is not in the customer catalog.",
		})
	for _, name := range []string{".DS_Store", "._cataloged-skill"} {
		if err := os.WriteFile(filepath.Join(root, ".skillshare", "skills", name), []byte("macOS metadata"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(openExplorerRuntimeEnv, root)

	pack, configured := inspectConfiguredOpenExplorerSkillPack()
	if !configured || pack.Status != "partial" || pack.Version != "2.0.4" || len(pack.Skills) != 2 || pack.CatalogCount != 1 || pack.DisabledCount != 1 {
		t.Fatalf("unexpected Skill Pack inventory: %+v configured=%v", pack, configured)
	}
	entries := openExplorerSkillEntries(pack)
	if len(entries) != 3 {
		t.Fatalf("Skill Pack entries = %d, want package plus two Skills", len(entries))
	}
	states := make(map[string]extensionEntry)
	for _, entry := range entries {
		states[entry.Name] = entry
	}
	if states["cataloged-skill"].Status != "available" || !states["cataloged-skill"].DefaultEnabled {
		t.Fatalf("cataloged Skill was not enabled: %+v", states["cataloged-skill"])
	}
	if states["extra-skill"].Status != "disabled" || states["extra-skill"].DefaultEnabled {
		t.Fatalf("uncataloged Skill was not disabled: %+v", states["extra-skill"])
	}
	encoded, _ := json.Marshal(entries)
	if strings.Contains(string(encoded), root) {
		t.Fatalf("Capability inventory leaked the external package path: %s", encoded)
	}

	paths, skillsRoot, err := configuredOpenExplorerSkillPaths()
	if err != nil || len(paths) != 1 || !strings.HasSuffix(paths[0], "/cataloged-skill/SKILL.md") || skillsRoot != filepath.Join(root, ".skillshare", "skills") {
		t.Fatalf("unexpected loadable Skill paths: paths=%v root=%q err=%v", paths, skillsRoot, err)
	}
	settings, err := mergeOpenExplorerSkillsIntoSettings([]byte(`{"skills":["/usr/local/lib/hobot-code/skills"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytesContainJSONText(settings, paths[0]) || strings.Contains(string(settings), "/extra-skill/SKILL.md") {
		t.Fatalf("settings did not import only cataloged Skills: %s", settings)
	}
}

func TestOpenExplorerSkillPackRejectsSymlinkedSkillMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "OpenExplorer_LLM")
	writeOpenExplorerSkillTestPackage(t, root, []string{"linked-skill"}, map[string]string{
		"linked-skill": "description: Linked metadata must not be accepted.",
	})
	skillPath := filepath.Join(root, ".skillshare", "skills", "linked-skill", "SKILL.md")
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(outside, []byte("---\nname: linked-skill\ndescription: outside\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(skillPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, skillPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv(openExplorerRuntimeEnv, root)
	pack, _ := inspectConfiguredOpenExplorerSkillPack()
	if pack.Status != "unsafe" || len(openExplorerSkillEntries(pack)) != 0 {
		t.Fatalf("symlinked Skill metadata was advertised: %+v", pack)
	}
}

func TestTaskAgentConfigurationImportsOpenExplorerSkills(t *testing.T) {
	cfg := testConfig(t)
	root := filepath.Join(t.TempDir(), "OpenExplorer_LLM")
	writeOpenExplorerSkillTestPackage(t, root, []string{"cataloged-skill"}, map[string]string{
		"cataloged-skill": "description: Imported into the private task settings snapshot.",
	})
	t.Setenv(openExplorerRuntimeEnv, root)
	if err := os.WriteFile(filepath.Join(cfg.AgentDir, "settings.json"), []byte(`{"skills":["/usr/local/lib/hobot-code/skills"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &task{manager: &taskManager{cfg: cfg}, metadata: taskMetadata{ID: "00112233445566778899aabb"}}
	directory, err := current.prepareTaskAgentConfiguration(networkModeOffline)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".skillshare", "skills", "cataloged-skill", "SKILL.md")
	if !bytesContainJSONText(raw, want) {
		t.Fatalf("private task settings do not contain imported Skill: %s", raw)
	}
}

func writeOpenExplorerSkillTestPackage(t *testing.T, root string, catalog []string, skills map[string]string) {
	t.Helper()
	versionDirectory := filepath.Join(root, "oellm_runtime", "include", "oellm_runtime_basic")
	if err := os.MkdirAll(versionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	version := "#define OELLM_VERSION_MAJOR 2\n#define OELLM_VERSION_MINOR 0\n#define OELLM_VERSION_PATCH 4\n"
	if err := os.WriteFile(filepath.Join(versionDirectory, "oellm_runtime_version.h"), []byte(version), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".skillshare", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	var catalogBody strings.Builder
	catalogBody.WriteString("| Skill 名称 | 功能简述 | 提示词示例 | 是否测过 |\n|---|---|---|---|\n")
	for _, name := range catalog {
		catalogBody.WriteString("| `" + name + "` | Test | Test | |\n")
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "03_SKILLS_CATALOG.md"), []byte(catalogBody.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, metadata := range skills {
		directory := filepath.Join(root, ".skillshare", "skills", name)
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\nversion: 1.0.0\n" + metadata + "\n---\n\n# Test\n"
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func bytesContainJSONText(raw []byte, value string) bool {
	encoded, _ := json.Marshal(value)
	return strings.Contains(string(raw), string(encoded))
}
