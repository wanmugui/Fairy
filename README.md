# Agent Loop Demo — AI Agent 对话测试平台

AI Agent 对话测试平台，支持多模型、工具调用、流式输出、会话管理与 Token 用量统计。

---

## 快速启动

### 三平台统一入口（推荐）

```bash
# Windows、macOS、Linux 均使用此命令
pnpm dev
```

需要预先安装 Node.js 18+、pnpm 与 Go。启动器会把当前平台的 Agent 构建到已忽略的 `.tools/`，设置 `AGENT_LOOP_PATH` 后启动 Node API 与 Vite；退出时只清理本次启动的子进程，不会结束其他 Node 进程。Python 与浏览器仅在调用相应本地工具时需要。

浏览器访问 `http://127.0.0.1:5173`，Node API 默认监听 `http://localhost:8081`。首次运行会按 `frontend/pnpm-lock.yaml` 安装前端依赖。

### 兼容入口

现有 `启动.bat` 与 `启动.sh` 暂时保留给已有使用者，但它们只转发到 `pnpm dev`；新开发和测试统一使用该入口。

需要手动指定已构建 Agent 时：

```bash
AGENT_LOOP_PATH=/absolute/path/to/agent-loop pnpm dev
```

### 本地 Python、Shell 与浏览器环境

`execute_code`、`bash` 和 `html_to_png` 由 Agent 的跨平台 Local adapter 执行。Python 是项目保留的跨平台能力；首次使用前运行：

```bash
pnpm setup:python
```

该命令以 Python 3.10+ 创建已忽略的 `.tools/venv`，并按 `requirements.lock` 安装固定版本的 `requests`、`beautifulsoup4` 和 `Pillow`。新版 PPT skill 使用 Python 3.10 语法，因此 3.9 解释器会被跳过。`pnpm dev`、PPT batch 与 Agent 会优先使用该环境。

如果需要使用已有 Conda/venv，可显式覆盖：

```bash
AGENT_PYTHON_BIN=/absolute/path/to/python pnpm dev
```

也可以为当前机器显式指定可执行文件，避免改动共享 JSON 配置：

```bash
export AGENT_PYTHON_BIN="/Users/yourname/miniconda3/envs/your-env-name/bin/python"
export AGENT_SHELL_BIN=/bin/zsh
export AGENT_BROWSER_BIN="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

Python 选择顺序为“`AGENT_PYTHON_BIN` → 项目 `.tools/venv` → `tool_runtime.executables.python` → 当前 `PATH`”。显式路径不存在时工具会返回 `code=unavailable`，不会静默切换到其他 Python 环境；Shell 和浏览器仍遵循各自的 `AGENT_*_BIN`、配置和平台发现顺序。

### 服务型工具的本地 HTTP mock

服务型工具（搜索、图像、知识库、文档等）在共享配置中显式走 HTTP，不再依赖仓库内的 Windows 工具二进制。需要验证 HTTP 协议、前端工具卡片或错误处理时，可启动无需 PowerShell 的本地 mock：

```bash
pnpm gateway:mock --port 8080
```

可用 `pnpm gateway:status` 和 `pnpm gateway:stop` 管理该 mock。接入真实服务时，复制私有配置并设置 `unifiedToolService.endpoint`、认证信息和必要请求头；共享配置只指向本机地址，绝不自动访问生产内网。每个 schema 都必须在 `tool_runtime.tools` 中显式选择 `local` 或 `http`，避免新增工具意外走到不透明的默认实现。

### 跨平台回归检查

安装前端依赖后，在 Windows、macOS 或 Linux 的对应 runner 上运行同一条命令：

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm test:cross-platform
```

它会运行 Go race 测试、Windows 与 Linux Agent 交叉构建、Node/前端 Agent 路径测试、Python 环境与 Gateway 语法检查、PPT 主/子 Agent mock smoke，以及前端构建；不要求浏览器、生产服务或密钥。当前作为本地发版前检查使用，不扩展 CI 配置。

迁移的目标、运行时边界、与远端兼容策略和剩余验证项见[跨平台迁移说明](docs/cross-platform-migration.md)。

---

## 更新日志

### 2026-08-11
- **系统提示词模板化**：新增 `config/locales/system/zh.md`（按 manifest.yml 拼接 parts/zh，保留条件标签）；运行时用 Jinja 子集渲染 + 注入 skill 注册表；去掉 skill 全文拼接
- **web_search 提速**：DuckDuckGo + Bing 并行（谁先出结果用谁，DDG 超时 12s→6s）；支持 `queries` 批量一次查多个 query（多线程并行）
- **工具并发上限**：`maxConcurrentTools` 4→8（对齐生产实测单回合 8 并发）
- **fetch_url 内容压缩**：抓取后调 LLM 摘要模型（`MaaS_Cl_Sonnet_46_Multi_Modal`）提炼要点返回，未配置/失败回退 `text[:8000]`
- **工具 API 配置入库**：config 新增 `tools` 块（fetchUrl/readFile/imageVQA/documentParser/rerank），保留与生产配置的字段对齐
- **reflection 开关**：config `reflection.enabled`（默认 false），关闭时不再走“草稿→反思→最终版”，省一次 LLM 调用
- **已交付状态注入**：记录最终交付（`<report>` 标题），压缩后注入 `[会话状态]` 已交付清单，防止重复执行已完成内容；会话续接自动恢复
- **create_subtask 结果瘦身**：只把最终报告交付主线程（去掉 30KB 压缩快照），主线程上下文不再被子任务对话撑爆
- **前端时间统计修复**：`subtaskAgentStats` 不再重复计入主线程时长/token，头部 agent 时间 = 主线程 + 子任务
- **其他**：删除 `config/user.txt` 测试残留

## 架构总览

```
┌─ 浏览器 ───────────────────────────────────────────────────────┐
│  React 18 + Vite 6 (localhost:5173)                            │
│  ├─ 全量标签版 / 仅结果版 视图切换                             │
│  ├─ SSE 流式接收 AI 回复 + 工具调用 + Token 统计              │
│  ├─ 可折叠工具调用卡片（ToolBlock）                            │
│  ├─ 报告卡片（ReportCard 弹窗）                                │
│  ├─ 会话管理（新建/切换/删除/下载）                           │
│  └─ 模型选择 + 自动切换（session 记忆模型）                   │
└──────────┬────────────────────────────────────────────────────┘
           │ POST /api/chat {stream:true}
           ▼
┌─ Node.js API 服务器 ───────────────────────────────────────────┐
│  frontend/server.cjs (端口 8081)                               │
│  ├─ GET  /api/models              ← 模型列表                    │
│  ├─ GET  /api/sessions            ← 会话列表                    │
│  ├─ GET  /api/sessions/:name/usage← 用量统计                    │
│  ├─ POST /api/chat                ← 聊天 + SSE 流式            │
│  ├─ DELETE /api/sessions/:name    ← 删除会话                    │
│  └─ static file service (frontend/dist/)                      │
└──────────┬────────────────────────────────────────────────────┘
           │ resolve current-platform Agent executable
           ▼
┌─ Agent 引擎 (Go) ─────────────────────────────────────────────┐
│  .tools/agent-loop-<platform>-<arch>[.exe]（本机缓存）        │
│  ├─ main.go         → 入口 + 配置加载 + 会话持久化              │
│  ├─ agentloop.go    → 核心循环 (调 LLM → 工具调用 → 修复)      │
│  ├─ apiclient.go    → LLM API 客户端 (OpenAI 兼容)              │
│  ├─ config.go       → 配置结构 + JSON 加载                     │
│  ├─ tagrouter.go    → 内容合规路由 + 标签修复                  │
│  ├─ internal/dtypes/ → Tool 契约                               │
│  ├─ internal/biz/tool/ → Schema、注册与并发分发                │
│  ├─ internal/biz/tool/builtin/ → 文件、Todo、ask_user、时间    │
│  ├─ internal/biz/tool/local/ → 子任务、Shell、Python、浏览器   │
│  ├─ internal/biz/tool/httptool/ → 统一 HTTP 服务适配           │
│  ├─ sysprompt.go    → 系统提示词构建（含 locale 模块）         │
│  ├─ skillloader.go  → Skill 加载                               │
│                                                                 │
│  每步 SSE 事件: status → assistant → tool_call → tool_result    │
└──────────┬────────────────────────────────────────────────────┘
           │ Tool Registry / Dispatcher
           ▼
┌─ Tool backends ────────────────────────────────────────────────┐
│  Local: 文件、Todo、ask_user、create_subtask、Python/Shell/浏览器│
│  HTTP:  统一服务或 tool_gateway --mock                          │
└───────────────────────────────────────────────────────────────┘
```

---

## 目录说明

### 核心组件

| 目录/文件 | 说明 |
|-----------|------|
| `agent/` | **Go Agent 引擎** — 组装层；工具实现位于 `internal/biz/tool/{builtin,local,httptool}`，无 PowerShell 依赖 |
| `.tools/agent-loop-<platform>-<arch>[.exe]` | 当前平台的本机缓存 Agent；主 Agent 与子任务共用。由 `pnpm dev`/`pnpm agent:build` 构建，永不提交 |
| `frontend/` | React 前端 + Node.js API 服务器 |
| `frontend/server.cjs` | API 服务器（端口 8081） |
| `frontend/src/` | React 源码 |
| `frontend/sessions/` | 会话 JSON 文件 + usage/请求日志（运行时自动生成） |
| `tool_gateway/` | 统一 HTTP 工具服务兼容网关；`--mock` 用于本地协议测试 |
| `config/` | 模型配置 + 系统提示词模块 |
| `config/system/parts/zh/` | 系统提示词分模块（中文） |
| `config/locales/` | Locale prompt 模块（summary, generate_title 等） |
| `skills/` | Skill 定义（Markdown 格式；由 config 中的注册表暴露，按需读取具体 `SKILL.md`） |
| `ppt_batch/` | PPT 批量自动化（测试集/runner/运行结果/文档） |
| `.tools/` | 本机缓存的 Agent 与 Python venv（已忽略） |
| `docs/cross-platform-migration.md` | 相对原远端的跨平台迁移说明、兼容策略与待办 |

### 废弃（已删除）

以下旧版 PowerShell 文件已被 **Go 实现**替代，已从仓库中删除：

| 旧文件 | 替代 |
|--------|------|
| `src/AgentLoop.ps1` | `agent/agentloop.go` |
| `src/TagRouter.ps1` | `agent/tagrouter.go` |
| `src/ToolLoader.ps1` | `agent/internal/biz/tool/schema.go` |
| `src/ApiClient.ps1` | `agent/apiclient.go` |
| `src/SystemPrompt.ps1` | `agent/sysprompt.go` |
| `src/SkillLoader.ps1` | `agent/skillloader.go` |
| `src/Config.ps1` | `agent/config.go` |
| `src/MockApi.ps1` | (deprecated, use config `use_mock`) |
| `agent_loop.ps1` | 历史兼容脚本；新入口使用 `pnpm dev` |

---

## 数据流：一次会话的生命周期

```
用户输入 "你好"
  → 前端 POST /api/chat {message:"你好", stream:true}
  → server.cjs 解析并启动当前平台 Agent
  → agent-loop 加载 config + tools + system prompt
  → 调 LLM API → 返回 assistant 回复（无 tool_calls）
  → 循环结束 → 保存 session.json + usage.json
  → SSE 推送 done 事件 → 前端渲染回复

用户输入 "查天气"
  → 同上，但 LLM 返回 tool_calls: [web_search]
  → 执行 web_search → 工具返回结果
  → 将结果加入 messages → 再次调 LLM
  → LLM 返回最终回复（<report> 包裹）
  → 循环结束 → 保存 session.json + usage.json
  → 每一步 SSE 实时推送（status → assistant → tool_call → tool_result）
```

### SSE 事件流

| 事件类型 | 触发时机 | 关键字段 |
|---------|---------|---------|
| `status` | 开始调用 / 压缩 / 生成标题 | `step`, `message` |
| `assistant` | LLM 返回内容 | `content`, `route`, `compliant`, `intermediate_description`, `tool_calls` |
| `tool_call` | 工具开始/完成 | `tool`, `status`, `arguments`, `ok`, `result_preview` |
| `waiting_user_input` | ask_user 等待用户 | `ask_type`, `questions` |
| `done` | 循环结束 | `finish_reason` |

---

## Token 与用量统计机制

项目实现了**逐轮统计 + 累积统计**双层级用量追踪，保存在 session 文件中。

### 数据结构

#### session JSON（`frontend/sessions/<chat-name>/<chat-name>.json`）

```json
{
  "messages": [
    {
      "role": "assistant",
      "content": "...",
      "usage": {
        "prompt_tokens": 21149,
        "completion_tokens": 266
      },
      "duration_ms": 7900
    }
  ],
  "model": "gateway_claude_opus_47"
}
```

每条 assistant 消息携带自己的 `usage` 和 `duration_ms`。

#### usage JSON（`frontend/sessions/<chat-name>/usage.json`）

```json
{
  "prompt_tokens": 48823,
  "completion_tokens": 1983,
  "duration_ms": 88900,
  "turns": [
    {
      "message_index": 3,
      "prompt_tokens": 21149,
      "completion_tokens": 266,
      "duration_ms": 7900
    },
    {
      "message_index": 7,
      "prompt_tokens": 27674,
      "completion_tokens": 1717,
      "duration_ms": 81000
    }
  ]
}
```

- **顶层**：当前回话累积的总量（`total_usage`）
- **turns**：每一轮 assistant 回复的独立统计（`turn_usage`）

#### 请求日志（`frontend/sessions/<chat-name>/req_0001.json`）

每次模型请求的原始请求体和响应体，用于调试：

```json
{
  "index": 1,
  "timestamp": "2026-07-29T10:00:00+08:00",
  "request": { "model": "...", "messages": [...], "tools": [...] },
  "response": { "content": "...", "tool_calls": [...] },
  "usage": { "prompt_tokens": 21149, "completion_tokens": 266 },
  "duration_ms": 7900
}
```

### 前端显示

| 位置 | 显示内容 | 数据来源 |
|------|---------|---------|
| 顶部栏 | `↑48823 ↓1983 ⏱88.9s` | session usage.json 顶层 |
| 每条助理回复下方 | `↑21149 in ↓266 out ⏱7.9s` | 消息的 usage + duration_ms |

### 保存机制

- **中间保存**：每轮 assistant 回复后立即写入 session 文件（`agentloop.go` 中 `SaveSession` 调用）
- **最终保存**：循环结束后完整写入 session + usage + 请求日志
- 即使进程崩溃，已保存的中间轮次数据不会丢失
- 前端可在对话过程中随时刷新读取最新 session

---

## PPT 批量自动化（效果 + 成本测算）

无头批量跑测试集，统计每次 PPT 生成的耗时与成本（详细说明见 `ppt_batch/README.md`）。

### 运行
```
# 跨平台生命周期 smoke（Mock 主 Agent + Mock 子任务，不访问真实服务）
pnpm test:ppt:mock

# 批量跑（默认纯文本 case，no-template 模式）
node scripts/python.mjs ppt_batch/scripts/run_batch.py --model clotho-qn-claude-opus-47 --limit 3

# 手动运行同一条 Mock batch
node scripts/python.mjs ppt_batch/scripts/run_batch.py --model ppt-mock --limit 1
# 重算成本表
node scripts/python.mjs ppt_batch/scripts/run_batch.py --collect-only --out ppt_batch_xxx
```

### 数据落盘
- 会话 + usage：`ppt_batch/runs/<run>/sessions/<case>/`（不进 frontend/sessions）
- PPT 产物：`workspace/result/<run>/<case>/`；runner 将其以逻辑路径 `/mnt/data/result/<run>/<case>/` 传给 Agent。
- 成本表：`ppt_batch/runs/<run>/costs.csv`

### ask_user 处理机制（两种入口）
| 入口 | 处理方式 | 说明 |
|---|---|---|
| 批处理（无头） | agent 侧自动应答 | runner 传 `-AutoAnswerAskUser`，ask_user 不阻塞，自动注入答案继续（大纲确认→“确认通过继续”）；不改 skill、不砍 LLM 轮次，成本数据与真实交互一致 |
| UI 手动 | 前端 30s 无应答自动选择 | ask_user 弹窗倒计时 30s，无操作自动勾选第一项并推进；用户操作即重置倒计时 |

### 成本口径（重要）
- `llm_call_count` / tokens = **主循环 + 全部子任务**（create_subtask 派生的独立 agent）汇总，CSV 中 main/subtask 分列
- 子任务通常占 60%+ 的调用和 token
- `real_ms` 仅 UI 流程有值；批量无头看 `elapsed_s`（墙钟）
- `ppt-mock` 只验证 Agent、子任务、session 和统计生命周期；不替代真实模型、搜索、图片或页面生成验收。

---

## 内容合规协议（标签路由）

Agent 引擎实现了严格的内容格式合规检查，确保 LLM 输出符合协议要求。

### 中间轮（含工具调用）

```xml
<process>
  <message>面向用户的简短处理说明</message>
  <file_action type="create">文件目标路径</file_action>
</process>
```

- **检测**：`GetContentRoute()` 路由分类为 `behavior`
- **合规**：`CheckIntermediateTurnCompliant()` 要求必须包含 `<process>`
- **修复**：缺少 `<process>` 时自动 `WrapAsProcess()` 包裹
- **兼容**：旧格式 `<behavior><des>...</des></behavior>` 仍被识别

### 最终轮（无工具调用）

```xml
<report>
回复内容（可包含 Markdown 图片、引用等）
</report>
```

- **检测**：路由分类为 `report`
- **合规**：`CheckFinalTurnCompliant()` 要求包含 `<report>`
- **修复**：`RepairFinalContent()` 自动包裹或转换
- **旧格式兼容**：`<response>` → 自动转换为 `<report>`

### 标签修复函数

| 函数 | 作用 |
|------|------|
| `GetContentRoute` | 分类内容路由 |
| `CheckIntermediateTurnCompliant` | 中间轮次合规检查 |
| `CheckFinalTurnCompliant` | 最终回复合规检查 |
| `WrapAsProcess` / `WrapAsBehavior` | 自动包裹中间标签 |
| `WrapAsReport` | 自动包裹最终标签 |
| `RepairIntermediateContent` | 自动修复中间内容 |
| `RepairFinalContent` | 自动修复最终内容 |
| `GetIntermediateDescription` | 提取用户可见处理说明 |
| `GetUserVisibleText` | 提取纯文本（剥离标签） |

---

## 模型配置

`config/` 目录下的 JSON 文件管理模型连接：

| 文件 | 用途 |
|------|------|
| `config.json` | 默认配置 |
| `config.minimax.json` | MiniMax 模型 |
| `config.clotho.json` | Clotho/Claude 模型 |
| `config.deepseek.json` | DeepSeek 模型 |

### 配置字段

```json
{
  "api": {
    "base_url": "https://api.example.com",
    "api_key": "sk-xxx",
    "model": "model-name",
    "timeout_sec": 120,
    "temperature": 0.7,
    "max_tokens": 4096
  },
  "workspace_dir": "workspace",
  "unifiedToolService": {
    "endpoint": "",
    "timeout": 120,
    "bearerToken": ""
  },
  "tool_runtime": {
    "tools": {
      "bash": { "backend": "local" },
      "web_search": { "backend": "http" }
    }
  },
  "httpTools": {}
}
```

---

## 关键参数（团队须知）

### LLM / 网关
| 参数 | 当前值 | 位置 | 说明 |
|---|---|---|---|
| model | `gateway_claude_opus_47` / `gateway_qn_claude_opus_47` | `config/config.clotho.json` | 前端选 `clotho-claude-opus-47` 或 `clotho-qn-claude-opus-47`；session 保存 override 后的 API 模型名 |
| base_url | `https://clotho-test.xiaohuanxiong.com/v2/llm` | 同上 | 测试网关 |
| timeout_sec | **900**（15 分钟） | 同上 | 单次 LLM 调用超时；默认 120（`agent/config.go`） |
| temperature | **0.4** | 同上 | |
| max_tokens | **16384**（16K） | `config/config.clotho.json`（config.json 未设 → 默认 8192，`agent/config.go`） | 单次输出上限；输出被截断（completion_tokens ≥ max_tokens）时，下一轮自动注入「精简输出、先执行工具」引导，预防空参数/循环截断 |

### Agent 循环
| 参数 | 默认/当前 | 位置 | 说明 |
|---|---|---|---|
| max_steps | **60** | `config/config.clotho.json` | 单个 agent 最大循环轮数 |
| summary_threshold_tokens | **100000**（100K） | `config/config.clotho.json`（config.json 未设 → 默认 60000） | 上下文（上次调用 prompt_tokens）超过阈值触发 summary 压缩。触发条件、自适应比例、压缩窗口、分级保护详见下方「Summary 压缩机制」 |
| maxConcurrentTools | **8** | `agent/agentloop.go` 硬编码 | 工具并行执行上限（对齐生产实测单回合 8 并发） |
| max_network_calls | **0（不限）** | `config/config.clotho.json`（0 = 不限，负数才回退默认 20） | 网络工具配额。web_search/fetch_url/image_search 共享配额，达到后强制“基于已有资料直接给结论”。**建议设 10~15 控制搜索量**（子任务曾 33 次网络调用） |
| reflection.enabled | **false** | `config/config.clotho.json`（config.json 同） | reflection 注入开关：false 直接出最终答复（省一次 LLM 调用）；true 走“草稿→反思→最终版” |

### Summary 压缩机制（详细）

> 代码：`agent/agentloop.go`；摘要模板：`config/locales/summary/zh.md`；阈值参数：`summary_threshold_tokens`

**触发条件**
- `summaryPrompt != "" && lastPromptTokens > summary_threshold_tokens`
- `lastPromptTokens` = 上一次 LLM 调用返回的 `usage.prompt_tokens`（即当前上下文大小），不是按消息条数
- 阈值取值：`config.clotho.json` = **100000**；`config.json` 未设置 → 默认 **60000**（`agent/config.go`）

**自适应压缩比例（compressionRatio）**
- 初始 **0.8**，范围 **`[0.70, 0.95]`**
- 距上次压缩 ≤ 2 步（频繁触发，例如每轮工具调用都触发）→ `+0.05`（多压一些，给后续留空间）
- 距上次压缩 ≥ 8 步（有余量）→ `-0.02`（少压一些，尽量保留原文信息）

**压缩窗口**
- `compressStart = 1`：**system 消息永不压缩**（index 0 跳过）
- `compressEnd = len(messages) × ratio`，并向后吞掉相邻的 tool 结果消息，保证发给 API 的载荷合法
- 段长 < 4 条时不压缩（避免小上下文反复折腾）
- **用户意图保护**：从截断点向前回退到最后一个 user 消息之前，最新用户请求永远原样保留

**分级保护（已读文件清单）**
- 全程跟踪本会话 `read_file` 成功读过的路径（`readFiles`），跨压缩保留
- 压缩时在 summary prompt 末尾追加「已读文件清单（必须保留）」：要求 summary 的 `[Important Files and Artifacts]` 栏目逐条保留这些路径，并为每个文件写 1-2 句关键内容/约束摘要
- 目的：压缩后模型仍知道“读过什么、关键规则是什么”，避免反复重读技能文档
- 优先级（graded）：**system > 用户请求 > 技能文档（已读清单）> 工具调用轨迹（正常概括即可）**

**摘要如何落库**
- 被压段落 + summary prompt（作为 user 消息）再调一次 LLM，产物包成 `<summary>...</summary>` 的 assistant 消息插在压缩边界
- **完整 `messages` 不删除**（前端照常渲染完整对话）；只有发给模型的 working view 缩为 `system + <summary> + 尾部原文`（尾部为保留信息完整性不裁剪）
- 压缩后若尾部最后一条不是 user，追加中性 user 提示 `请继续。`（qn 网关要求最后一条必须为 user）

**会话续接**
- 加载 session 时找最后一个 `<summary>`：working view = `system + <summary> + 其后全部`
- `ResumeSessionFile`（续接被中断的子任务）走 `buildResumeContext`，同样只取最近的 summary 作为续接种子

**如何观察**
- 状态事件：`正在压缩历史上下文...`
- 会话 JSON：数 `<summary>` 开头的 assistant 消息 = 压缩次数；对照被压段落原文与 summary 检查保真度（关键约束 / 文件 / 决策是否保留）

---

### Reflection 反思机制（详细）

> 代码：`agent/agentloop.go`；反思模板：`config/locales/reflection/zh.md`

**触发条件**
- 本轮模型回复**无工具调用**（即将输出最终答案）且本循环尚未反思过
- 条件：`!hasToolCalls && reflectionPrompt != "" && !postReflection`

**执行流程**
1. 标记 `willReflect`，前端把该轮回复显示为 **draft**（“✍️ 草稿中…（反思后将更新为最终版）”）
2. 注入 `config/locales/reflection/zh.md` 作为 **system 消息**
3. 追加中性 user 触发语：`请根据以上反思，给出你的最终回答。`（qn/Claude 网关拒绝“最后一条是 system”）
4. `postReflection = true`，进入下一轮 LLM 调用，产出反思后的最终版；每轮循环**最多反思一次**

**反思模板内容**
- 支持 `{% if Focus %}` 注入反思焦点，默认“通用质量检查与证据完整性审视”
- 要求用 `<reflection>` 标签输出：`<original_task>` / `<findings>`（每项 `<item><category><finding><source>`）/ `<result>` / `<cite_files>` / `<todo>`
- 允许反思阶段调用工具补充证据；外部证据用 `<cite>` 引用

**如何观察**
- 状态事件：`正在起草稿并反思…`
- 会话中 draft 标记 → 后接的最终报告即为反思后的产物

---

### 工具实现说明

工具均由 `tool_runtime.tools` 显式路由：工作区、会话和本机环境工具走进程内 Go 实现；搜索、图像、知识库和文档等服务型工具走统一 HTTP 协议。关键点：

| 工具 | 说明 |
|------|------|
| web_search / image_generate / image_search | 服务型工具，通过统一 HTTP endpoint 调用；开发时可用 `pnpm gateway:mock` 返回确定性结果 |
| html_to_png | 本地 Chrome、Chromium 或 Edge headless 渲染 HTML→PNG；支持 stateless（传 HTML 字符串返回 png_base64）与 file 模式 |
| ask_user | 前端弹窗提问。对 qn 模型把 questions 数组序列化成含未转义引号的字符串做了容错（
repairQuotedJSON 转义后解析） |
| fetch_url / document_parser / image_vqa / knowledge_* / memory_search / reflection | 服务型工具，通过统一 HTTP endpoint 调用 |
| bash | 根据当前平台选择可用 shell（Windows 默认 PowerShell，macOS/Linux 依次发现 zsh、bash、sh），在受限 workspace 内执行 |
| execute_code | 使用本机可发现或配置的 Python 执行，支持超时控制 |
| create_subtask | 子任务结果**瘦身**：只把最终报告（`<subtask_result>`/`<report>`）交付主线程，去掉 30KB 压缩快照，主线程上下文不再被子任务对话撑爆；完整对话存 `subtasks/<title>.json` |

### 会话恢复消息清洗

继续对话加载 session 时会做清洗，避免 qn 网关 400：
- **system 消息降级**：首条 system（系统提示词）保留，后续 system 错误消息降级为 user（前端 appendErrorToSession 曾把崩溃错误写成 mid-stream system，qn 拒绝）
- **空 assistant 过滤**：
repairToolPairing 后清掉 content 和 tool_calls 都空的 assistant 消息（repair 会产生这类空消息）

### 团队注意事项
1. **文件编码**：配置/Go 源码部分带 UTF-8 BOM，读取用 `utf-8-sig`；Windows 下经管道写中文到 Go/Python 源码时用 `\u` 转义防乱码
2. **改动被测 skill 会影响对比口径**：分片生成等优化属于“优化版”，与原版分开记录
3. **run 命名**：`ppt_batch_<ts>`，不嵌入 case 名
4. **数据可追溯**：会话、deck、costs.csv 都保留，根据 run 名对应查找

---

## 会话管理

- 每个会话独立目录：`frontend/sessions/<chat-name>/`
- 会话文件：`<chat-name>.json`（messages + model）
- 用量文件：`usage.json`（总用量 + 逐轮统计）
- 请求日志：`req_0001.json`、`req_0002.json` ...
- **中间保存**：每轮助理回复后立即保存，崩溃不丢数据
- **模型记忆**：会话自动保存使用的模型，加载时自动切换
- 前端顶部栏：总用量实时统计 + 下载会话按钮

---

## 技术栈

| 层 | 技术 |
|----|------|
| 前端 | React 18, Vite 6, JavaScript (JSX) |
| API 服务器 | Node.js (原生 http 模块) |
| Agent 引擎 | **Go 1.24**（当前平台原生二进制） |
| 工具运行时 | **Go** Local backend、HTTP backend |
| 代码执行 | Python 3.10+（项目 `.tools/venv` 或本机已配置解释器） |
| 流式传输 | Server-Sent Events (SSE) |
| 模型 API | OpenAI 兼容格式 |

---

## 配置

### 工具 API 配置（config 的 tools 块）

`config.json` / `config.clotho.json` 顶层 `tools` 块保留生产工具配置字段，便于与生产配置对齐：
- `readFile`：本地 `read_file` 使用的大文件分段读取阈值。
- `fetchUrl` / `imageVQA` / `documentParser` / `rerank`：服务侧能力的兼容字段；本地 Agent 不会再把它们自动转换为旧 `TOOL_*` 子进程环境变量。服务型工具的调用地址、认证和重试由 `unifiedToolService` 与 `tool_runtime.tools` 决定。
- `pptTools`（目前在 `config.clotho.json`）：本地 Shell、Python 与子任务进程会继承 PPT skill 所需的端点与 host-pin 环境变量；通用示例配置不填写真实内网端点。

新增工具时，先在 `config/tools/schemas.json` 定义 schema，再在 `tool_runtime.tools` 显式选择 `local` 或 `http`；同一工具名只能注册一个 backend。

### config.json 基础配置

`config/` 目录下的 JSON 文件管理模型连接，支持多模型配置切换。

### 系统提示词

`config/system/parts/zh/` 按功能拆分为多个模块：

| 文件 | 说明 |
|------|------|
| `01_role.md` | 角色定义 + 语言红线 + 代码执行规则 |
| `02_core_capabilities.md` | 核心能力说明 |
| `03_file_system.md` | 文件系统说明 |
| `04_SOP.md` | 标准操作流程 |
| `05_memory_policy.md` | 记忆策略 |
| `06_long_term_memory.md` | 长期记忆 |
| `07_user_profile.md` | 用户画像 |
| `08_citation_rules.md` | 引用规则 |
| `09_output_principles.md` | 输出协议 + 终局规则 |
