package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BuildSystemPrompt renders the modular system-prompt template
// (cfg.SystemPath(), normally config/locales/system/zh.md) with the runtime
// feature flags and the configured skill registry index. Full skill contents
// are intentionally NOT appended here: the agent reads SKILL.md on demand via
// read_file when a registry entry matches (see the skill-registry block).
func BuildSystemPrompt(cfg *Config) string {
	tmpl := ""
	if useModularSystemParts(cfg) {
		tmpl = assembleSystemPrompt(cfg)
	}
	if tmpl == "" {
		tmpl = readTextFile(cfg.SystemPath())
	}
	if tmpl == "" {
		tmpl = "You are a helpful assistant."
	}
	registryJSON := buildMergedSkillRegistryJSON(cfg)
	rendered, err := renderJinja(tmpl, systemTemplateVars(cfg, registryJSON))
	if err != nil {
		// Never send raw template tags to the model; degrade by removing them.
		rendered = stripTemplateTags(tmpl)
	}
	// The template provides "Today: {{ CURRENT_TIME }}"; keep a fallback so a
	// legacy fully-rendered system file still gets a Today line.
	if !strings.HasPrefix(rendered, "Today:") {
		rendered = fmt.Sprintf("Today: %s\n\n%s", time.Now().Format("2006-01-02 15:04:05 -07:00"), rendered)
	}
	return rendered
}

// useModularSystemParts enables parts assembly only for the default modular
// system prompt files; custom system_path values keep their single-file behavior.
func useModularSystemParts(cfg *Config) bool {
	if cfg == nil || strings.TrimSpace(cfg.SystemPartsDir) == "" {
		return false
	}
	path := strings.ReplaceAll(strings.TrimSpace(cfg.SystemPath()), "\\", "/")
	return strings.HasSuffix(path, "config/locales/system/zh.md") || strings.HasSuffix(path, "config/locales/system/en.md")
}

// assembleSystemPrompt builds the modular system prompt from parts/{lang}/
// using manifest.yml ordering when available. Falls back to sorted .md files.
func assembleSystemPrompt(cfg *Config) string {
	if cfg == nil || strings.TrimSpace(cfg.SystemPartsDir) == "" {
		return ""
	}
	partsDir := cfg.ResolvePath(cfg.SystemPartsDir)
	manifestPath := filepath.Join(filepath.Dir(partsDir), "manifest.yml")
	lang := filepath.Base(partsDir)
	var names []string
	if data, err := os.ReadFile(manifestPath); err == nil {
		names = systemPromptManifestNames(string(data), lang)
	}
	if len(names) == 0 {
		entries, err := os.ReadDir(partsDir)
		if err != nil {
			return ""
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
	}
	var builder strings.Builder
	for _, name := range names {
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(partsDir, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		if content == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(content)
	}
	return builder.String()
}

func systemPromptManifestNames(manifest, lang string) []string {
	var names []string
	inSection := false
	for _, raw := range strings.Split(manifest, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") && strings.TrimSuffix(line, ":") == lang {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "- ") {
				names = append(names, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
				continue
			}
			if line != "" {
				break
			}
		}
	}
	return names
}

func systemTemplateVars(cfg *Config, registryJSON string) map[string]any {
	tool := func(name string) bool {
		if t, ok := cfg.HTTPTools[name]; ok {
			return t.Enabled
		}
		return true
	}
	return map[string]any{
		"enable_web_search":              tool("web_search"),
		"enable_fetch_url":               tool("fetch_url"),
		"enable_image_vqa":               tool("image_vqa"),
		"enable_document_parser":         tool("document_parser"),
		"enable_read_file":               tool("read_file"),
		"enable_write_file":              tool("write_file"),
		"enable_edit_file":               tool("edit_file"),
		"enable_glob":                    tool("glob"),
		"enable_ask_user":                tool("ask_user"),
		"enable_execute_code":            tool("execute_code"),
		"enable_bash":                    tool("bash"),
		"enable_todolist":                tool("todolist_create") || tool("todolist_append") || tool("todolist_update"),
		"enable_create_subtask":          tool("create_subtask"),
		"enable_reflection":              tool("reflection"),
		"enable_memory_search":           tool("memory_search"),
		"enable_memory":                  tool("memory_search"),
		"enable_skill_registry":          registryJSON != "[]",
		"enable_date_memory":             tool("memory_search"),
		"enable_os_mac_linux":            false,
		"enable_result_dir":              true,
		"enable_agent_file_allow_list":   false,
		"is_delegate":                    false,
		"max_consecutive_web_tool_calls": 10,
		"CURRENT_TIME":                   time.Now().Format("2006-01-02 15:04:05 -07:00"),
		"SKILL_REGISTRY_JSON":            registryJSON,
		"LONG_TERM_MEMORY":               readMemoryFileCapped(configuredMemoryRoot(cfg), "memory.md", 6000),
		"USER_PROFILE":                   readMemoryFileCapped(configuredMemoryRoot(cfg), "user.md", 3000),
		"LONG_TERM_MEMORY_PATH":          "memory://memory.md",
		"USER_PROFILE_PATH":              "memory://user.md",
	}
}

// buildSkillRegistryJSON serializes the configured skill registry for the
// system template's {{ SKILL_REGISTRY_JSON|safe }} slot.
func buildSkillRegistryJSON(skills []SkillReg) string {
	if len(skills) == 0 {
		return "[]"
	}
	b, err := json.MarshalIndent(skills, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(b)
}

func buildMergedSkillRegistryJSON(cfg *Config) string {
	if cfg == nil {
		return "[]"
	}
	return buildSkillRegistryJSON(mergeSkillRegistries(cfg.Skills, DiscoverSkillRegistry(configuredSkillsRoot(cfg))))
}

// ReadTextFile reads a UTF-8 file, strips BOM, returns content or empty

func readMemoryFileCapped(memoryRoot, name string, maxRunes int) string {
	if strings.TrimSpace(memoryRoot) == "" {
		return ""
	}
	content := readTextFile(filepath.Join(memoryRoot, name))
	runes := []rune(content)
	if len(runes) > maxRunes {
		content = string(runes[:maxRunes])
	}
	return content
}
func readTextFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	return string(data)
}

// ReadLocalePrompt reads a modular locale prompt from config/locales/<type>/<lang>.md
func ReadLocalePrompt(cfg *Config, promptType, lang string) string {
	if cfg.SystemPartsDir == "" {
		return ""
	}
	// Navigate from system_parts_dir up to locales dir
	partsDir := cfg.ResolvePath(cfg.SystemPartsDir)
	localesDir := filepath.Dir(filepath.Dir(filepath.Dir(partsDir)))
	p := filepath.Join(localesDir, promptType, lang+".md")
	return readTextFile(p)
}

// ReadSummaryPrompt returns summary prompt
func ReadSummaryPrompt(cfg *Config) string {
	t := ReadLocalePrompt(cfg, "summary", "zh")
	return t
}

// ReadGenerateTitlePrompt returns title generation prompt
func ReadGenerateTitlePrompt(cfg *Config) string {
	return ReadLocalePrompt(cfg, "generate_title", "zh")
}

// ReadFinalizePrompt returns finalize prompt
func ReadFinalizePrompt(cfg *Config) string {
	return ReadLocalePrompt(cfg, "finalize", "zh")
}

// ReadReflectionPrompt returns reflection prompt
func ReadReflectionPrompt(cfg *Config) string {
	return ReadLocalePrompt(cfg, "reflection", "zh")
}
