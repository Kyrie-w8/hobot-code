package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAndLoad(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "coding")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: coding\ndescription: Test skill\nrequired_tools: [fs_read]\nboards: [x5, s600]\n---\n\nRead before writing.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load([]string{"coding"}, "x5", map[string]bool{"fs_read": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Instructions != "Read before writing." {
		t.Fatalf("unexpected skill: %+v", loaded)
	}
	if _, err := catalog.Load([]string{"coding"}, "s100", map[string]bool{"fs_read": true}); err == nil {
		t.Fatal("expected board rejection")
	}
}
