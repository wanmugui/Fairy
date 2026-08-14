package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRepoRootPrefersExplicitEnvironmentRoot(t *testing.T) {
	want := filepath.Clean(t.TempDir())
	t.Setenv("AGENT_REPO_ROOT", want)

	if got := findRepoRoot(); got != want {
		t.Fatalf("unexpected repo root: got %q want %q", got, want)
	}
}

func TestPreparePPTDeckWorkspaceCreatesConfiguredLocalDeck(t *testing.T) {
	repoRoot := t.TempDir()
	cfg := &Config{RepoRoot: repoRoot, WorkspaceDir: "workspace"}
	prompt := `<ppt_config><ppt_mode>no-template</ppt_mode><deck_dir>/mnt/data/result/pptid_demo</deck_dir></ppt_config>`

	if _, err := preparePPTDeckWorkspace(cfg, prompt); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repoRoot, "workspace", "result", "pptid_demo")
	info, err := os.Stat(want)
	if err != nil || !info.IsDir() {
		t.Fatalf("deck directory was not created: %q err=%v", want, err)
	}
}

func TestPreparePPTDeckWorkspaceRejectsHostPath(t *testing.T) {
	cfg := &Config{RepoRoot: t.TempDir(), WorkspaceDir: "workspace"}
	prompt := `<ppt_config><deck_dir>/tmp/not-a-deck</deck_dir></ppt_config>`

	if _, err := preparePPTDeckWorkspace(cfg, prompt); err == nil {
		t.Fatal("expected host path to be rejected")
	}
}

func TestPreparePPTDeckWorkspaceResolvesBundledTemplate(t *testing.T) {
	repoRoot := t.TempDir()
	templateRoot := filepath.Join(repoRoot, "skills", "ppt-template-mode", "templates", "white")
	if err := os.MkdirAll(filepath.Join(templateRoot, "htmls"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateRoot, "tag_gen_results.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateRoot, "htmls", "1.html"), []byte("<html/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{RepoRoot: repoRoot, WorkspaceDir: "workspace", SkillsDir: "skills"}
	prompt := `<ppt_config><ppt_mode>template</ppt_mode><template_name>white</template_name><template_html_dir>/outside</template_html_dir><template_tags_path>/outside.json</template_tags_path><deck_dir>/mnt/data/result/pptid_demo</deck_dir></ppt_config>`

	got, err := preparePPTDeckWorkspace(cfg, prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<template_html_dir>"+filepath.Join(templateRoot, "htmls")+"</template_html_dir>") {
		t.Fatalf("template html directory was not resolved: %s", got)
	}
	if !strings.Contains(got, "<template_tags_path>"+filepath.Join(templateRoot, "tag_gen_results.json")+"</template_tags_path>") {
		t.Fatalf("template tags path was not resolved: %s", got)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "workspace", "result", "pptid_demo")); err != nil {
		t.Fatalf("deck directory was not created: %v", err)
	}
}

func TestPreparePPTDeckWorkspaceRejectsUnknownBundledTemplate(t *testing.T) {
	repoRoot := t.TempDir()
	templateRoot := filepath.Join(repoRoot, "skills", "ppt-template-mode", "templates", "other")
	if err := os.MkdirAll(filepath.Join(templateRoot, "htmls"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateRoot, "tag_gen_results.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateRoot, "htmls", "1.html"), []byte("<html/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{RepoRoot: repoRoot, WorkspaceDir: "workspace", SkillsDir: "skills"}
	prompt := `<ppt_config><ppt_mode>template</ppt_mode><template_name>not-present</template_name></ppt_config>`

	if _, err := preparePPTDeckWorkspace(cfg, prompt); err == nil {
		t.Fatal("expected missing bundled template to be rejected")
	}
}
