package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type APIConfig struct {
	BaseURL     string  `json:"base_url"`
	APIKey      string  `json:"api_key"`
	Model       string  `json:"model"`
	TimeoutSec  int     `json:"timeout_sec"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
}

type GatewayConfig struct {
	Endpoint    string            `json:"endpoint"`
	Timeout     int               `json:"timeout"`
	BearerToken string            `json:"bearerToken"`
	Headers     map[string]string `json:"headers"`
	HostPin     string            `json:"hostPin"` // "host=ip,host2=ip2"：HTTP 工具 DNS pin（本地解析不到公网域名时钉到可达 IP）
}

type ToolApisConfig struct {
	FetchUrl       FetchUrlToolConfig       `json:"fetchUrl"`
	ReadFile       ReadFileToolConfig       `json:"readFile"`
	ImageVQA       ImageVQAToolConfig       `json:"imageVQA"`
	DocumentParser DocumentParserToolConfig `json:"documentParser"`
	Rerank         RerankToolConfig         `json:"rerank"`
	PptTools       PptToolsToolConfig       `json:"pptTools"`
}

type FetchUrlToolConfig struct {
	SummaryModelName    string `json:"summaryModelName"`
	SummaryModelBaseUrl string `json:"summaryModelBaseUrl"`
}

type ReadFileToolConfig struct {
	SegmentReadMaxTokens int   `json:"segmentReadMaxTokens"`
	SegmentReadMinTokens int   `json:"segmentReadMinTokens"`
	MaxReadFileSizeBytes int64 `json:"maxReadFileSizeBytes"`
}

type ImageVQAToolConfig struct {
	ModelName string `json:"modelName"`
}

type DocumentParserToolConfig struct {
	ServiceUrl             string `json:"serviceUrl"`
	ParserEndpoint         string `json:"parserEndpoint"`
	UploadDir              string `json:"uploadDir"`
	OutputMaxTokens        int    `json:"outputMaxTokens"`
	OutputTruncateStrategy string `json:"outputTruncateStrategy"`
	TableMaxDisplayRows    int    `json:"tableMaxDisplayRows"`
}

type RerankToolConfig struct {
	ServiceUrl         string  `json:"serviceUrl"`
	ChunkMinTokens     int     `json:"chunkMinTokens"`
	ChunkOverlapTokens int     `json:"chunkOverlapTokens"`
	TopK               int     `json:"topK"`
	Threshold          float64 `json:"threshold"`
}

// PptToolsToolConfig 是新 PPT skill（ppt-maker 分发到 no-template/template/creative
// 三个模式）依赖的后端工具网关配置。creative_page_render / html_page_generate /
// html_page_review / html_to_png / image_filter 均经 POST {baseUrl}{apiPath}
// （/api/agent/tool_call）调用；生产工具链模型见 config.yml novaModelConfigs 的
// aipptv2 系列。本地需把 code-dev.xiaohuanxiong.com 钉到内网网关 172.30.17.27。
type PptToolsToolConfig struct {
	BaseUrl string `json:"baseUrl"`
	ApiPath string `json:"apiPath"`
	HostPin string `json:"hostPin"`
}

type ReflectionConfig struct {
	Enabled bool `json:"enabled"`
}

type SkillReg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

type ToolEntry struct {
	Enabled bool `json:"enabled"`
}

type ToolRuntimeConfig struct {
	Executables LocalExecutableConfig          `json:"executables"`
	Tools       map[string]ToolBackendOverride `json:"tools"`
	RetryCount  int                            `json:"retry_count"`
}

// BashPolicyConfig is the on-disk representation of the bash policy. An
// empty struct means "use the hard-coded defaults from
// local.DefaultBashPolicy()"; users opt out by setting `enabled: false`.
type BashPolicyConfig struct {
	Enabled        bool     `json:"enabled"`
	AllowCommands  []string `json:"allow_commands"`
	DenyCommands   []string `json:"deny_commands"`
	DenyPaths      []string `json:"deny_paths"`
	StrictDenyOnly bool     `json:"strict_deny_only"`
}

type LocalExecutableConfig struct {
	Python  string `json:"python"`
	Shell   string `json:"shell"`
	Browser string `json:"browser"`
}

type ToolBackendOverride struct {
	Backend ToolBackend `json:"backend"`
}

type Config struct {
	API          APIConfig `json:"api"`
	WorkspaceDir string    `json:"workspace_dir"`
	UseMock      bool      `json:"use_mock"`
	Prompts      struct {
		SystemPath string `json:"system_path"`
		UserPath   string `json:"user_path"`
	} `json:"prompts"`
	SystemPartsDir         string               `json:"system_parts_dir"`
	SkillsDir              string               `json:"skills_dir"`
	Skills                 []SkillReg           `json:"skills"`
	Reflection             ReflectionConfig     `json:"reflection"`
	Tools                  ToolApisConfig       `json:"tools"`
	HistoryDir             string               `json:"history_dir"`
	MockFile               string               `json:"mock_file"`
	Gateway                GatewayConfig        `json:"unifiedToolService"`
	HTTPTools              map[string]ToolEntry `json:"httpTools"`
	ToolRuntime            *ToolRuntimeConfig   `json:"tool_runtime,omitempty"`
	BashPolicy             BashPolicyConfig     `json:"bash_policy,omitempty"`
	ToolsSchemas           string               `json:"tools_schemas"`
	MaxSteps               int                  `json:"max_steps"`
	SummaryThresholdTokens int                  `json:"summary_threshold_tokens"`
	MaxNetworkCalls        int                  `json:"max_network_calls"`
	ConfigPath             string               `json:"-"`
	RepoRoot               string               `json:"-"`
	mockLLMState           *mockLLMState        `json:"-"`
}

func LoadConfig(repoRoot, configPath string) (*Config, error) {
	absPath := configPath
	if !filepath.IsAbs(configPath) {
		absPath = filepath.Join(repoRoot, configPath)
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	// Strip BOM
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		raw = raw[3:]
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.ConfigPath = absPath
	cfg.RepoRoot = repoRoot
	resolveAPIKeyFromFile(&cfg)
	if err := ensureToolRuntimeDefaults(&cfg); err != nil {
		return nil, err
	}

	// Default values
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 60
	}
	if cfg.SummaryThresholdTokens <= 0 {
		cfg.SummaryThresholdTokens = 60000
	}
	if cfg.MaxNetworkCalls < 0 {
		cfg.MaxNetworkCalls = 20
	} // 0 = unlimited; negative -> default 20
	if cfg.API.TimeoutSec <= 0 {
		cfg.API.TimeoutSec = 120
	}
	if cfg.API.MaxTokens <= 0 {
		cfg.API.MaxTokens = 8192
	}
	if cfg.HistoryDir == "" {
		cfg.HistoryDir = "runs"
	}
	return &cfg, nil
}

func isValidToolBackend(backend ToolBackend) bool {
	switch backend {
	case BackendLocal, BackendHTTP:
		return true
	default:
		return false
	}
}

// ensureToolRuntimeDefaults 为显式的工具路由应用默认值并校验 backend 名称。
// 每一个启用的 schema 都必须在 tools 中声明其 backend；不再回退到旧的外部进程工具。
func ensureToolRuntimeDefaults(cfg *Config) error {
	if cfg.ToolRuntime == nil {
		cfg.ToolRuntime = &ToolRuntimeConfig{
			Tools:      make(map[string]ToolBackendOverride),
			RetryCount: 1,
		}
	} else {
		if cfg.ToolRuntime.Tools == nil {
			cfg.ToolRuntime.Tools = make(map[string]ToolBackendOverride)
		}
		if cfg.ToolRuntime.RetryCount <= 0 {
			cfg.ToolRuntime.RetryCount = 1
		}
	}

	for name, override := range cfg.ToolRuntime.Tools {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("tool_runtime.tools: tool name is empty")
		}
		if !isValidToolBackend(override.Backend) {
			return fmt.Errorf("tool_runtime.tools.%s.backend: unknown backend %q", name, override.Backend)
		}
	}
	return nil
}

func (c *Config) ResolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.RepoRoot, p)
}

func (c *Config) SystemPath() string {
	if c.Prompts.SystemPath != "" {
		return c.ResolvePath(c.Prompts.SystemPath)
	}
	return filepath.Join(c.RepoRoot, "config", "system.txt")
}

func (c *Config) UserPath() string {
	if c.Prompts.UserPath != "" {
		return c.ResolvePath(c.Prompts.UserPath)
	}
	return filepath.Join(c.RepoRoot, "config", "user.txt")
}

func (c *Config) SchemasPath() string {
	if c.ToolsSchemas != "" {
		return c.ResolvePath(c.ToolsSchemas)
	}
	return filepath.Join(c.RepoRoot, "config", "tools", "schemas.json")
}


// resolveAPIKeyFromFile fills READ_FROM_* API key placeholders from key files
// at the repo root. MINIMAX_key.txt / DEEPSEEK_key.txt: first line key,
// optional second line model.
func resolveAPIKeyFromFile(cfg *Config) {
	key := cfg.API.APIKey
	if key == "" || !strings.HasPrefix(key, "READ_FROM_") {
		return
	}
	var fileName string
	switch {
	case strings.Contains(key, "DEEPSEEK"):
		fileName = "DEEPSEEK_key.txt"
	default:
		fileName = strings.TrimPrefix(key, "READ_FROM_") + ".txt"
	}
	data, err := os.ReadFile(filepath.Join(cfg.RepoRoot, fileName))
	if err != nil {
		return
	}
	text := string(data)
	if len(text) >= 3 && text[0] == 0xEF && text[1] == 0xBB && text[2] == 0xBF {
		text = text[3:]
	}
	lines := strings.Split(text, "\n")
	var secret string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if secret == "" {
			if idx := strings.Index(line, ":"); idx > 0 && !strings.Contains(line, "://") {
				secret = strings.TrimSpace(line[idx+1:])
			} else {
				secret = line
			}
		} else if cfg.API.Model == "" || strings.HasPrefix(cfg.API.Model, "READ_FROM") {
			cfg.API.Model = line
			break
		}
	}
	if secret != "" {
		cfg.API.APIKey = secret
	}
}
