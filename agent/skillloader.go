package main

import (
	"os"
	"path/filepath"
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
