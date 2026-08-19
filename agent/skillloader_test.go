package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillRegistryFindsDiskSkills(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ppt-maker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ppt-maker", "SKILL.md"), []byte("# ppt-maker\n制作 PPT 的技能"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := DiscoverSkillRegistry(root)
	if len(registry) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(registry))
	}
	if registry[0].Name != "ppt-maker" {
		t.Fatalf("unexpected skill name: %q", registry[0].Name)
	}
	if registry[0].Location != "local:///skills/ppt-maker/SKILL.md" {
		t.Fatalf("unexpected skill location: %q", registry[0].Location)
	}
}

func TestMergeSkillRegistriesKeepsConfiguredFirst(t *testing.T) {
	configured := []SkillReg{{Name: "ppt-maker", Description: "configured", Location: "/skills/ppt-maker"}}
	discovered := []SkillReg{{Name: "ppt-maker", Description: "disk", Location: "local:///skills/ppt-maker/SKILL.md"}, {Name: "new-skill", Description: "new", Location: "local:///skills/new-skill/SKILL.md"}}
	merged := mergeSkillRegistries(configured, discovered)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged skills, got %d", len(merged))
	}
	if merged[0].Description != "configured" {
		t.Fatalf("configured skill should win: %#v", merged[0])
	}
	if merged[1].Name != "new-skill" {
		t.Fatalf("unexpected appended skill: %#v", merged[1])
	}
}
