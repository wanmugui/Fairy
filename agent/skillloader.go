package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name    string
	Content string
}

// LoadSkills loads all skills from skills_dir, recursing into subdirectories.
// Each subdirectory is loaded as a skill (directory name = skill name).
func LoadSkills(skillsDir string) ([]Skill, error) {
	if skillsDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil
	}

	var skills []Skill
	for _, e := range entries {
		if e.IsDir() {
			// Load skill from subdirectory (dir name = skill name)
			subDir := filepath.Join(skillsDir, e.Name())
			subSkills, _ := loadSkillsFromDir(subDir, e.Name())
			skills = append(skills, subSkills...)
			continue
		}
		// Top-level .md files (backward compat)
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(skillsDir, name))
		if err != nil {
			continue
		}
		skillName := strings.TrimSuffix(name, ".md")
		content := strings.TrimSpace(string(data))
		if content != "" {
			skills = append(skills, Skill{Name: skillName, Content: content})
		}
	}
	return skills, nil
}

// loadSkillsFromDir loads skill .md files from a subdirectory.
func loadSkillsFromDir(dir string, dirName string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		// Use directory name as skill name
		skillName := dirName
		if !strings.EqualFold(name, "SKILL.md") {
			skillName = dirName + "." + strings.TrimSuffix(name, ".md")
		}
		skills = append(skills, Skill{Name: skillName, Content: content})
	}
	return skills, nil
}

// DiscoverSkillRegistry walks the skills root and builds a registry of
// SKILL.md / plugin.json / manifest.json entries without requiring the
// config file to list them.
func DiscoverSkillRegistry(skillsDir string) []SkillReg {
	root := strings.TrimSpace(skillsDir)
	if root == "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	var registry []SkillReg
	seen := map[string]bool{}
	_ = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		base := strings.ToLower(entry.Name())
		if base != "skill.md" && base != "plugin.json" && base != "manifest.json" {
			return nil
		}
		relative, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return nil
		}
		dirRel := filepath.ToSlash(filepath.Dir(relative))
		name := filepath.Base(filepath.Dir(relative))
		if dirRel == "." {
			name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		if name == "" || seen[name] {
			return nil
		}
		seen[name] = true
		location := "local:///skills/" + filepath.ToSlash(relative)
		registry = append(registry, SkillReg{Name: name, Description: skillDescription(path), Location: location})
		return nil
	})
	sort.SliceStable(registry, func(i, j int) bool { return registry[i].Name < registry[j].Name })
	return registry
}

func mergeSkillRegistries(configured, discovered []SkillReg) []SkillReg {
	merged := append([]SkillReg{}, configured...)
	seen := map[string]bool{}
	for _, skill := range merged {
		seen[strings.ToLower(skill.Name)] = true
	}
	for _, skill := range discovered {
		key := strings.ToLower(skill.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, skill)
	}
	return merged
}

func skillDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		var meta map[string]any
		if json.Unmarshal(data, &meta) == nil {
			if desc, ok := meta["description"].(string); ok {
				return strings.TrimSpace(desc)
			}
		}
	}
	lines := strings.Split(content, "\n")
	var description strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if description.Len() > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "---") {
			continue
		}
		description.WriteString(trimmed)
		description.WriteString(" ")
		if description.Len() > 400 {
			break
		}
	}
	desc := strings.TrimSpace(description.String())
	if desc != "" {
		return desc
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
		}
	}
	return ""
}
