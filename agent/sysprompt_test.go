package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssembleSystemPromptUsesManifestOrder(t *testing.T) {
	root := t.TempDir()
	partsDir := filepath.Join(root, "parts", "zh")
	if err := os.MkdirAll(partsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partsDir, "02_core.md"), []byte("SECOND"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partsDir, "01_role.md"), []byte("FIRST"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "zh:\n  - 01_role.md\n  - 02_core.md\n"
	if err := os.WriteFile(filepath.Join(root, "parts", "manifest.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{RepoRoot: root, SystemPartsDir: "parts/zh"}

	got := assembleSystemPrompt(cfg)
	if got != "FIRST\n\nSECOND" {
		t.Fatalf("unexpected assembled prompt: %q", got)
	}
}

func TestAssembleSystemPromptFallsBackToSortedFiles(t *testing.T) {
	root := t.TempDir()
	partsDir := filepath.Join(root, "parts", "zh")
	if err := os.MkdirAll(partsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partsDir, "b.md"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partsDir, "a.md"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{RepoRoot: root, SystemPartsDir: "parts/zh"}

	got := assembleSystemPrompt(cfg)
	if got != "A\n\nB" {
		t.Fatalf("unexpected fallback order: %q", got)
	}
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("assembled prompt missing content: %q", got)
	}
}

func TestSystemPromptManifestNames(t *testing.T) {
	manifest := "zh:\n  - 01_role.md\n  - 02_core.md\n\nen:\n  - en_a.md\n"
	names := systemPromptManifestNames(manifest, "zh")
	if len(names) != 2 || names[0] != "01_role.md" || names[1] != "02_core.md" {
		t.Fatalf("unexpected manifest names: %#v", names)
	}
}
