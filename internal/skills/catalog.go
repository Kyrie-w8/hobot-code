package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version"`
	RequiredTools []string `json:"required_tools"`
	Boards        []string `json:"boards"`
	Instructions  string   `json:"-"`
	Path          string   `json:"path"`
}

type Catalog struct {
	items map[string]Skill
}

func Discover(roots []string) (*Catalog, error) {
	c := &Catalog{items: map[string]Skill{}}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name(), "SKILL.md")
			if _, err := os.Stat(path); err != nil {
				continue
			}
			skill, err := parse(path)
			if err != nil {
				return nil, err
			}
			if _, exists := c.items[skill.Name]; exists {
				return nil, fmt.Errorf("duplicate skill %q", skill.Name)
			}
			c.items[skill.Name] = skill
		}
	}
	return c, nil
}

func (c *Catalog) List() []Skill {
	items := make([]Skill, 0, len(c.items))
	for _, item := range c.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (c *Catalog) Load(names []string, board string, tools map[string]bool) ([]Skill, error) {
	var out []Skill
	for _, name := range names {
		skill, ok := c.items[name]
		if !ok {
			return nil, fmt.Errorf("unknown skill %q", name)
		}
		if len(skill.Boards) > 0 && !contains(skill.Boards, board) {
			return nil, fmt.Errorf("skill %q does not support board %q", name, board)
		}
		for _, required := range skill.RequiredTools {
			if !tools[required] {
				return nil, fmt.Errorf("skill %q requires unavailable tool %q", name, required)
			}
		}
		out = append(out, skill)
	}
	return out, nil
}

func parse(path string) (Skill, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Skill{}, fmt.Errorf("%s: missing frontmatter", path)
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return Skill{}, fmt.Errorf("%s: unclosed frontmatter", path)
	}
	meta, body := text[4:4+end], strings.TrimSpace(text[4+end+5:])
	s := Skill{Version: "1", Path: path, Instructions: body}
	for _, line := range strings.Split(meta, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "name":
			s.Name = value
		case "description":
			s.Description = value
		case "version":
			s.Version = value
		case "required_tools":
			s.RequiredTools = list(value)
		case "board_profiles", "boards":
			s.Boards = list(value)
		}
	}
	if s.Name == "" {
		s.Name = filepath.Base(filepath.Dir(path))
	}
	return s, nil
}

func list(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	if value == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(value, ",") {
		out = append(out, strings.Trim(strings.TrimSpace(item), `"'`))
	}
	return out
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value || item == "*" {
			return true
		}
	}
	return false
}
