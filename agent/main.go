package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// estMsgTokens is a rough token estimate (content chars / 3) used to size the
// resumed subtask's initial context.

// buildResumeContext loads a previous (interrupted) subtask session and reduces it
// to a compact initial context: the last few <summary> markers (cumulative; the
// newest is most complete) + the recent tail after the last summary, trimmed to
// ~60% of the summary threshold. This avoids feeding the full raw history to the
// resumed subtask (which could be millions of tokens) while keeping its knowledge.
func buildResumeContext(oldSessionFile string, cfg *Config) []Message {
	// Minimal resume seed: just let the new subtask know what the previous one did.
	// The newest <summary> is the most complete cumulative compression; fall back
	// to the last message (often the final subtask_result / last action) if the
	// old run had no summary yet.
	_ = cfg
	oldMsgs, _ := loadExistingSession(oldSessionFile)
	if len(oldMsgs) == 0 {
		return nil
	}
	for i := len(oldMsgs) - 1; i >= 0; i-- {
		if strings.HasPrefix(oldMsgs[i].Content, "<summary>") {
			return []Message{oldMsgs[i]}
		}
	}
	return []Message{oldMsgs[len(oldMsgs)-1]}
}

func main() {
	// Command-line flags accepted by the native Agent executable.
	configPath := flag.String("ConfigPath", "config/config.json", "Config file path (relative to repo or absolute)")
	useMock := flag.String("UseMock", "", "Override use_mock: true/false (empty = use config)")
	userOverrideFile := flag.String("UserOverrideFile", "", "File containing user prompt")
	sessionFile := flag.String("SessionFile", "", "Session JSON file for persistence")
	autoAnswerAskUser := flag.Bool("AutoAnswerAskUser", false, "Auto-answer ask_user (e.g. outline confirm) in headless/batch mode")
	resumeSessionFile := flag.String("ResumeSessionFile", "", "Session JSON of a previous (interrupted) subtask to continue from; its summaries + recent tail seed the new subtask context")
	flag.Parse()

	repoRoot := findRepoRoot()
	if strings.TrimSpace(os.Getenv("AGENT_REPO_ROOT")) == "" {
		_ = os.Setenv("AGENT_REPO_ROOT", repoRoot)
	}

	// Propagate the session file path to tool subprocesses (incl. create_subtask)
	// via env var, so child agents can persist their session under the same chat dir.
	if *sessionFile != "" {
		_ = os.Setenv("AGENT_SESSION_FILE", *sessionFile)
	}

	// 1. Load config
	cfg, err := LoadConfig(repoRoot, *configPath)
	if err != nil {
		failF("config: %v", err)
	}
	if *useMock == "true" {
		cfg.UseMock = true
	} else if *useMock == "false" {
		cfg.UseMock = false
	}

	// 2. Build system prompt (modular locale template + skill registry; no skill full-text injection)
	systemPrompt := BuildSystemPrompt(cfg)

	// 4. Read user message
	userPrompt := readUserMessage(*userOverrideFile, cfg)
	if userPrompt == "" {
		fmt.Fprintln(os.Stderr, "[harness] user prompt is empty. Aborting.")
		os.Exit(2)
	}
	userPrompt, err = preparePPTDeckWorkspace(cfg, userPrompt)
	if err != nil {
		failF("prepare PPT workspace: %v", err)
	}

	// 5. 构建与平台无关的工具注册表。各 backend 的路径和进程细节
	// 由 ToolFactory 及具体 backend 实现内部负责。
	registry, err := NewToolFactory().BuildRegistry(cfg)
	if err != nil {
		failF("build tool registry: %v", err)
	}
	toolDefs := registry.ListSchemas()

	// 6. Load session if continuing
	var initialMsgs []Message
	var initialUsage *SessionUsage
	if *resumeSessionFile != "" {
		// Continue from a previous (interrupted) subtask: seed the new subtask
		// with the old session\u2019s compressed view (its <summary> markers + a
		// recent tail trimmed to ~60% of the summary threshold) so it starts with
		// the old knowledge but with headroom, instead of the full raw history.
		initialMsgs = buildResumeContext(*resumeSessionFile, cfg)
		_, initialUsage = loadExistingSession(*resumeSessionFile)
	} else if *sessionFile != "" {
		initialMsgs, initialUsage = loadExistingSession(*sessionFile)
	}

	// 7. Locale prompts
	summaryPrompt := ReadSummaryPrompt(cfg)
	genTitlePrompt := ReadGenerateTitlePrompt(cfg)
	reflectionPrompt := ReadReflectionPrompt(cfg)
	finalizePrompt := ReadFinalizePrompt(cfg)

	// 8. Run agent loop
	result, err := RunAgentLoop(cfg, registry, systemPrompt, userPrompt,
		initialMsgs, initialUsage, summaryPrompt, genTitlePrompt, reflectionPrompt, finalizePrompt,
		*sessionFile, cfg.API.Model, *autoAnswerAskUser)
	if err != nil {
		failF("agent loop: %v", err)
	}

	// 9. Save session
	if *sessionFile != "" {
		if err := SaveSession(*sessionFile, result.Messages, cfg.API.Model); err != nil {
			fmt.Fprintf(os.Stderr, "[harness] WARN: save session: %v\n", err)
		}
		_ = SaveUsage(*sessionFile, result.Usage, result.Messages, result.PerMessageUsage)
	}

	// 10. Save run history
	saveRunLog(cfg, result, systemPrompt, toolDefs)

	// 11. Print final
	fmt.Fprintf(os.Stderr, "\n[harness] steps=%d\n", result.Steps)
	printFinalMessage(result.Messages)
}

const localPPTDeckRoot = "/mnt/data/result"

type localPPTConfig struct {
	DeckDir         string `xml:"deck_dir"`
	Mode            string `xml:"ppt_mode"`
	TemplateName    string `xml:"template_name"`
	TemplateHTMLDir string `xml:"template_html_dir"`
	TemplateTags    string `xml:"template_tags_path"`
}

type localPPTTemplate struct {
	Name    string
	HTMLDir string
	Tags    string
}

var localPPTTemplateNames = map[string]struct{}{
	"white":     {},
	"tech-blue": {},
}

// preparePPTDeckWorkspace mirrors the production PPT entrypoint's one job that
// matters to this local harness: a deck_dir included in <ppt_config> already
// exists before the Skill starts. The logical production root /mnt/data maps to
// the local configured workspace, never to the host's real /mnt directory.
func preparePPTDeckWorkspace(cfg *Config, userPrompt string) (string, error) {
	start := strings.Index(userPrompt, "<ppt_config>")
	if start < 0 {
		return userPrompt, nil
	}
	endRelative := strings.Index(userPrompt[start:], "</ppt_config>")
	if endRelative < 0 {
		return "", fmt.Errorf("ppt_config is missing its closing tag")
	}
	end := start + endRelative + len("</ppt_config>")
	configXML := userPrompt[start:end]
	var config localPPTConfig
	if err := xml.Unmarshal([]byte(configXML), &config); err != nil {
		return "", fmt.Errorf("parse ppt_config: %w", err)
	}

	if strings.TrimSpace(config.DeckDir) != "" {
		workspace := cfg.ResolvePath(cfg.WorkspaceDir)
		deckDir, err := localPPTDeckPath(workspace, config.DeckDir)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(deckDir, 0o755); err != nil {
			return "", fmt.Errorf("create deck directory: %w", err)
		}
	}

	if !strings.EqualFold(strings.TrimSpace(config.Mode), "template") {
		return userPrompt, nil
	}
	template, err := resolveLocalPPTTemplate(cfg, config.TemplateName)
	if err != nil {
		return "", err
	}
	configXML, err = replacePPTConfigField(configXML, "template_name", template.Name)
	if err != nil {
		return "", err
	}
	configXML, err = replacePPTConfigField(configXML, "template_html_dir", template.HTMLDir)
	if err != nil {
		return "", err
	}
	configXML, err = replacePPTConfigField(configXML, "template_tags_path", template.Tags)
	if err != nil {
		return "", err
	}
	return userPrompt[:start] + configXML + userPrompt[end:], nil
}

// resolveLocalPPTTemplate turns the small, user-selectable template name into
// verified local resource paths. The paths are intentionally concrete host
// paths: the existing PPT Python scripts consume them directly. They are never
// inferred by searching arbitrary directories on the developer machine.
func resolveLocalPPTTemplate(cfg *Config, requestedName string) (localPPTTemplate, error) {
	name := strings.TrimSpace(strings.ReplaceAll(requestedName, "\\", "/"))
	if name == "" {
		return localPPTTemplate{}, fmt.Errorf("template mode requires template_name (available local templates: white, tech-blue)")
	}
	if path.Base(name) != name || name == "." || name == ".." {
		return localPPTTemplate{}, fmt.Errorf("template_name %q is invalid", requestedName)
	}
	if _, ok := localPPTTemplateNames[name]; !ok {
		return localPPTTemplate{}, fmt.Errorf("template_name %q is unavailable (available local templates: white, tech-blue)", requestedName)
	}

	templatesRoot := filepath.Join(cfg.ResolvePath(cfg.SkillsDir), "ppt-template-mode", "templates")
	templateRoot := filepath.Join(templatesRoot, name)
	tags := filepath.Join(templateRoot, "tag_gen_results.json")
	htmlDir := filepath.Join(templateRoot, "htmls")
	if info, err := os.Stat(tags); err != nil || info.IsDir() {
		return localPPTTemplate{}, fmt.Errorf("bundled template %q is unavailable: missing tag_gen_results.json", name)
	}
	if info, err := os.Stat(htmlDir); err != nil || !info.IsDir() {
		return localPPTTemplate{}, fmt.Errorf("bundled template %q is unavailable: missing htmls directory", name)
	}
	entries, err := os.ReadDir(htmlDir)
	if err != nil {
		return localPPTTemplate{}, fmt.Errorf("read bundled template %q: %w", name, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".html") {
			return localPPTTemplate{Name: name, HTMLDir: htmlDir, Tags: tags}, nil
		}
	}
	return localPPTTemplate{}, fmt.Errorf("bundled template %q is unavailable: htmls contains no .html files", name)
}

func replacePPTConfigField(configXML, field, value string) (string, error) {
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
		return "", fmt.Errorf("escape %s: %w", field, err)
	}
	replacement := "<" + field + ">" + escaped.String() + "</" + field + ">"
	pattern := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(field) + `>.*?</` + regexp.QuoteMeta(field) + `>`)
	if pattern.MatchString(configXML) {
		return pattern.ReplaceAllString(configXML, replacement), nil
	}
	closingTag := "</ppt_config>"
	if !strings.Contains(configXML, closingTag) {
		return "", fmt.Errorf("ppt_config is missing its closing tag")
	}
	return strings.Replace(configXML, closingTag, "  "+replacement+"\n"+closingTag, 1), nil
}

func localPPTDeckPath(workspace, logicalDeckDir string) (string, error) {
	logicalDeckDir = strings.TrimSpace(strings.ReplaceAll(logicalDeckDir, "\\", "/"))
	prefix := localPPTDeckRoot + "/"
	if !strings.HasPrefix(logicalDeckDir, prefix) {
		return "", fmt.Errorf("ppt deck_dir %q must be under %s", logicalDeckDir, localPPTDeckRoot)
	}
	relative := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(logicalDeckDir, prefix)))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("ppt deck_dir %q is invalid", logicalDeckDir)
	}
	return filepath.Join(workspace, "result", relative), nil
}

// findRepoRoot locates the repository root from an explicit override, the
// native executable's location, or the current directory.
func findRepoRoot() string {
	if configuredRoot := strings.TrimSpace(os.Getenv("AGENT_REPO_ROOT")); configuredRoot != "" {
		if absRoot, err := filepath.Abs(configuredRoot); err == nil {
			return filepath.Clean(absRoot)
		}
	}
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		parent := filepath.Dir(dir)
		if _, err := os.Stat(filepath.Join(parent, "config", "config.json")); err == nil {
			return parent
		}
		if _, err := os.Stat(filepath.Join(dir, "config", "config.json")); err == nil {
			return dir
		}
		return parent
	}
	cwd, _ := os.Getwd()
	return cwd
}

// readUserMessage reads user prompt from file, override, or config
func readUserMessage(overrideFile string, cfg *Config) string {
	if overrideFile != "" {
		if _, err := os.Stat(overrideFile); err == nil {
			data, err := os.ReadFile(overrideFile)
			if err == nil {
				return strings.TrimSpace(string(data))
			}
		}
	}
	return readTextFile(cfg.UserPath())
}

// loadExistingSession reads previous session messages and usage
func loadExistingSession(sessionFile string) ([]Message, *SessionUsage) {
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		return nil, nil
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	var session struct {
		Messages []Message `json:"messages"`
		Model    string    `json:"model"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, nil
	}

	// Filter out invalid messages: assistant with no content and no tool_calls (API rejects these)
	var cleanMsgs []Message
	systemSeen := false
	for _, m := range session.Messages {
		if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) == 0 {
			continue
		}
		// system role is only valid at the very start of the conversation for
		// Claude/Claude-compatible APIs. Frontend error lines were historically
		// appended as mid-stream "system" messages (SYSTEM ERROR: ...), which the
		// qn gateway rejects with 400. Keep the first system message (the real
		// system prompt) and demote any later system message to a user-role note
		// so the history stays valid while the error text is not lost.
		if m.Role == "system" {
			if !systemSeen {
				systemSeen = true
				cleanMsgs = append(cleanMsgs, m)
				continue
			}
			m.Role = "user"
			cleanMsgs = append(cleanMsgs, m)
			continue
		}
		cleanMsgs = append(cleanMsgs, m)
	}
	// NOTE: must assign unconditionally ? demotion (system->user) does not
	// change the slice length, so a length-based guard would silently discard
	// the fixed messages (bug observed: mid-stream system errors stayed system
	// and qn gateway rejected them).
	session.Messages = cleanMsgs

	// Repair orphaned tool_calls / tool responses. If the main thread crashed
	// while a tool (e.g. create_subtask) was still running, the assistant
	// message keeps its tool_calls but no matching tool response is persisted;
	// the API then rejects the sequence. Strip tool_calls from such assistant
	// messages and drop orphaned tool responses.
	session.Messages = repairToolPairing(session.Messages)

	// repairToolPairing can leave an assistant message with BOTH empty content
	// AND empty tool_calls (it strips tool_calls from the last orphaned call).
	// Claude/qn gateways reject such messages with 400 "field Content invalid".
	// Drop them now (the second pass catches what the pre-repair filter missed).
	afterRepair := session.Messages[:0]
	for _, m := range session.Messages {
		if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) == 0 {
			continue
		}
		afterRepair = append(afterRepair, m)
	}
	session.Messages = afterRepair

	// Accumulate usage from loaded messages
	usage := &SessionUsage{}
	for _, m := range session.Messages {
		if m.Usage != nil {
			usage.PromptTokens += m.Usage.PromptTokens
			usage.CompletionTokens += m.Usage.CompletionTokens
		}
		usage.DurationMs += m.DurationMs
	}
	return session.Messages, usage
}

// repairToolPairing ensures every assistant tool_calls message is followed by
// its tool responses and no tool response is orphaned.
func repairToolPairing(msgs []Message) []Message {
	var out []Message
	var pending []string // tool call IDs awaiting a response, in order
	stripLastCalls := func() {
		for j := len(out) - 1; j >= 0; j-- {
			if out[j].Role == "assistant" && len(out[j].ToolCalls) > 0 {
				out[j].ToolCalls = nil
				break
			}
		}
	}
	for _, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// A new assistant message means any previous pending calls were
			// orphaned (no tool responses arrived before the next turn).
			if len(pending) > 0 {
				stripLastCalls()
				pending = nil
			}
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					pending = append(pending, tc.ID)
				}
			}
			out = append(out, m)
			continue
		}
		if m.Role == "tool" {
			if len(pending) == 0 {
				continue // orphaned tool response: drop
			}
			if m.ToolCallID != "" {
				found := false
				for i, id := range pending {
					if id == m.ToolCallID {
						pending = append(pending[:i], pending[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					pending = pending[1:] // mismatch: consume the oldest anyway
				}
			} else {
				pending = pending[1:]
			}
			out = append(out, m)
			continue
		}
		// user / system / assistant-without-calls: boundary; any pending calls
		// that never got responses must be stripped.
		if len(pending) > 0 {
			stripLastCalls()
			pending = nil
		}
		out = append(out, m)
	}
	if len(pending) > 0 {
		stripLastCalls()
	}
	return out
}

// printFinalMessage outputs the last assistant message
func printFinalMessage(messages []Message) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].Content != "" {
			fmt.Println(messages[i].Content)
			return
		}
	}
}

// saveRunLog writes the run history JSON + text dump
func saveRunLog(cfg *Config, result *AgentResult, systemPrompt string, toolDefs []ToolDef) {
	stamp := time.Now().Format("20060102_150405")
	histDir := cfg.ResolvePath(cfg.HistoryDir)
	os.MkdirAll(histDir, 0755)

	// Build schema names
	var schemaNames []string
	for _, td := range toolDefs {
		if fn, ok := td.Function.(map[string]interface{}); ok {
			if n, ok := fn["name"]; ok {
				schemaNames = append(schemaNames, fmt.Sprintf("%v", n))
			}
		}
	}

	// JSON history
	// Categorized file name pattern:
	//   run_<chatName>_main_<ts>.json         (e.g. run_chat-20260803-094933-1_main_094948.json)
	//   run_<chatName>_subtask_<safeTitle>_<ts>.json
	//   run_orphan_main_<ts>.json             (no AGENT_SESSION_FILE set)
	//   run_orphan_subtask_<safeTitle>_<ts>.json
	chatName := ""
	if ps := os.Getenv("AGENT_SESSION_FILE"); ps != "" {
		chatDir := filepath.Dir(ps)
		// Child agents persist under frontend/sessions/<chat>/subtasks/;
		// normalize back to the parent chat name for run classification.
		if filepath.Base(chatDir) == "subtasks" {
			chatDir = filepath.Dir(chatDir)
		}
		chatName = filepath.Base(chatDir)
	}
	kind := os.Getenv("AGENT_RUN_KIND")
	if kind != "subtask" {
		kind = "main"
	}
	prefix := "run"
	if chatName != "" {
		prefix = "run_" + chatName + "_" + kind
	} else {
		prefix = "run_orphan_" + kind
	}
	if kind == "subtask" {
		if t := os.Getenv("AGENT_SUBTASK_TITLE"); t != "" {
			prefix += "_" + sanitizeRunTitle(t)
		}
	}
	fileBase := prefix + "_" + stamp
	outJSON := filepath.Join(histDir, fileBase+".json")

	// Use trace from agent loop result
	trace := result.Trace
	if trace == nil {
		trace = []map[string]interface{}{}
	}

	payload := map[string]interface{}{
		"generated_at":  time.Now().Format(time.RFC3339),
		"repo_root":     cfg.RepoRoot,
		"model":         cfg.API.Model,
		"use_mock":      cfg.UseMock,
		"system_prompt": systemPrompt,
		"tool_schemas":  schemaNames,
		"usage":         result.Usage,
		"steps":         result.Steps,
		"messages":      result.Messages,
		"trace":         trace,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err == nil {
		os.WriteFile(outJSON, data, 0644)
	}

	// Text dump
	outTXT := filepath.Join(histDir, fileBase+".txt")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== AGENT LOOP RUN @ %s ===\n", stamp))
	sb.WriteString(fmt.Sprintf("steps=%d  use_mock=%v  model=%s\n", result.Steps, cfg.UseMock, cfg.API.Model))
	if result.Usage != nil {
		sb.WriteString(fmt.Sprintf("usage: prompt_tokens=%d  completion_tokens=%d  duration_ms=%d\n",
			result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.DurationMs))
	}
	sb.WriteString("\n--- messages ---\n")
	for i, m := range result.Messages {
		sb.WriteString(fmt.Sprintf("[%d] role=%s\n", i+1, m.Role))
		if m.Content != "" {
			sb.WriteString(m.Content + "\n")
		}
		if len(m.ToolCalls) > 0 {
			sb.WriteString("(tool_calls:)\n")
			for _, tc := range m.ToolCalls {
				sb.WriteString(fmt.Sprintf("  - id=%s name=%s args=%s\n", tc.ID, tc.Function.Name, tc.Function.Arguments))
			}
		}
		if m.ToolCallID != "" {
			sb.WriteString(fmt.Sprintf("tool_call_id=%s name=%s\n", m.ToolCallID, m.Name))
		}
		sb.WriteString("\n")
	}
	os.WriteFile(outTXT, []byte(sb.String()), 0644)

	fmt.Fprintf(os.Stderr, "[harness] history JSON: %s\n", outJSON)
	fmt.Fprintf(os.Stderr, "[harness] history TXT : %s\n", outTXT)
}

// sanitizeRunTitle keeps ASCII alphanumerics and CJK; other chars become _.
// Length capped at 60 to keep total filename within Windows path limits.
func sanitizeRunTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_' || r == '-' || r == '.' || r == ' ':
			b.WriteRune(r)
		case r > 0x7f:
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.TrimSpace(b.String())
	// collapse underscores/spaces
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	out = strings.ReplaceAll(out, " ", "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return strings.Trim(out, "_.-")
}

func failF(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[harness] ERROR: "+format+"\n", args...)
	os.Exit(1)
}
