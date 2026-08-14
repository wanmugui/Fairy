# 跨平台迁移说明

## 目标与边界

本分支的目标是让开发和测试可以在 Windows、macOS、Linux 上使用同一套源码与启动入口，不再提交、硬编码或运行依赖 Windows `.exe`、`run.ps1`、PowerShell 工具链。

这不意味着“完全没有原生二进制”：Agent 是 Go 程序，`pnpm dev` 会为**当前平台与架构**构建本机二进制并缓存到 `.tools/`。缓存不提交，其他平台在各自机器上重新构建。Python 与浏览器同样按工具需要在本机发现或显式配置。

不在本阶段范围内：把 Python 能力改造成远程代码执行服务、替代生产 HTTP 工具服务、增加 CI Runner，或统一 `skills/` 与 `workspace/skills/` 的双副本。

## 架构变化

| 旧方式 | 当前方式 |
|---|---|
| `tools/<name>/main.go` 编译为仓库内 `run.exe`，由 `run.ps1` / 旧加载器调度 | schema、注册与实现都在 `agent/`；工具名只注册一个实现 |
| 每个工具以独立子进程运行，配置借由 `TOOL_*` 环境变量传递 | `builtin` 工具进程内执行；需要本机程序的工具由 `local` 执行；服务能力由 `httptool` 调用统一 HTTP 协议 |
| 启动脚本和子任务依赖固定 Windows 二进制 | `pnpm dev` 构建当前平台 Agent；主任务与子任务复用同一可执行文件 |
| 服务工具在仓库内保留 Windows stub/二进制 | 通过 `unifiedToolService` 调用真实服务，或通过 `pnpm gateway:mock` 验证协议 |

目录职责如下：

- `agent/internal/biz/tool/builtin/`：文件、Todo、`ask_user`、时间等纯 Go 本地能力。
- `agent/internal/biz/tool/local/`：子任务、Shell、Python、浏览器渲染及本机可执行文件发现。
- `agent/internal/biz/tool/httptool/`：搜索、图像、知识库、文档等服务型工具的统一 HTTP 协议适配。

`config/tools/schemas.json` 定义模型可见的工具 schema；`tool_runtime.tools` 为每个 schema 显式选择 `local` 或 `http`。注册表拒绝重名，因此模型只会看见 `bash`，不会同时看到 `bash` 与 `local_shell`。

## 启动与运行时

```text
pnpm dev
  -> Go build agent/ 到 .tools/agent-loop-<platform>-<arch>[.exe]
  -> 启动 Node API 与 Vite
  -> Node API 启动该 Agent，并传入 AGENT_REPO_ROOT
  -> Agent 从 schema + tool_runtime 建立唯一工具注册表
  -> create_subtask 复用当前 Agent 可执行文件
```

`.tools/` 是已忽略的本机缓存：包含当前平台 Agent、交叉编译验证产物和可选 Python venv。它不是发布物，不应提交。`agent/.tools/` 若出现二进制，是此前手工交叉编译留下的无用残留；启动链路不读取它。

本机能力的发现顺序：

- Python：`AGENT_PYTHON_BIN` → 项目 `.tools/venv` → `tool_runtime.executables.python` → `PATH`，要求 Python 3.10+。
- Shell：`AGENT_SHELL_BIN` → `tool_runtime.executables.shell` → 平台候选；Windows 默认 PowerShell，macOS/Linux 依次尝试 zsh、bash、sh。
- 浏览器：`AGENT_BROWSER_BIN` → `tool_runtime.executables.browser` → Chrome、Chromium、Edge 的平台候选。

找不到能力时，本地工具返回结构化 `code=unavailable`，而不是隐式调用 Windows 专用脚本。

### 本地执行与 PPT 模板

`bash` 与 `execute_code` 是本地开发测试工具：默认工作目录是 `workspace/`，`/mnt/data` 会映射到该目录；但它们**不是**宿主机级安全沙箱，不能用于执行不受信任的命令或代码。若后续需要真正阻断对宿主机任意路径的读写，应单列为按 macOS、Linux、Windows 分别实现和验证的进程隔离项目，不能用命令文本过滤伪造隔离。

模板模式只提供随仓库交付的 `skills/ppt-template-mode/templates/white` 与 `tech-blue`。前端把用户选择的模板名交给本地 Agent；入口会验证模板文件，并在本轮 `<ppt_config>` 中填入相应的真实绝对路径，供既有 Python 脚本使用。此过程不扫描 `/`、`/mnt/data/upload` 或开发者机器上的任意模板目录。

## 配置与远端兼容

`unifiedToolService` 是服务型工具的运行时地址、认证和超时配置。根级 `tools` 块保留生产配置字段以维持配置兼容：目前本地 Agent 直接消费 `readFile`；其他服务字段不会再自动转为旧 `TOOL_*` 环境变量。

远端新增的 `tools.pptTools` 已在新架构中保留：Local Shell、Python 和子任务会生成 PPT skill 所需的 `PPT_TOOL_API_BASE`、`BACKEND_TOOL_BASE`、host-pin 与 creative-render 环境变量。这样 PPT skill 不需要恢复旧 `toolloader` 或 `tools/` 目录。真实内网端点只放在专用配置（目前为 `config.clotho.json`），通用 `config.json` 不包含它们。

远端重新引入的 `reflection` 也被保留为服务型工具和配置开关；没有将它错误地恢复为旧本地可执行文件。

远端近期对旧 `tools/bash`、`tools/execute_code` 与 `tools/image_search` 的修改处理如下：

- Bash 与 Python 对生产虚拟根 `/mnt/data`、`/skills` 的路径映射，已经由 `local` backend 的平台解析与工作区路径处理覆盖。
- `image_search` 在当前架构中明确是 HTTP 服务型工具，因此不将其旧的本地抓取、缓存和下载代码复制回 `tools/`。
- PPT 网关环境变量是唯一仍需迁移的运行时行为，已由 Local backend 接管并有单元测试覆盖。

## 验证范围

`pnpm test:cross-platform` 会执行 Go race 测试、Node 启动器测试、Windows/Linux Go 交叉编译、当前平台 Agent 构建、Python 环境与语法检查、PPT 主/子任务 mock smoke，以及前端构建。它不需要生产密钥、真实浏览器或真实服务。

已覆盖的是 macOS 本机构建/测试和 Windows/Linux 的编译兼容性；仍应由对应平台的开发或测试人员各执行一次完整 `pnpm test:cross-platform`，验证真实进程启动、Shell、Python 与浏览器发现。CI 没有因本迁移而扩展。

## 后续事项

1. 以 `agent-service` 为参考，统一 `skills/` 与 `workspace/skills/` 的单一来源及加载方式。
2. 在真实 Windows、Linux 环境补一次完整本地回归记录。
3. 视生产 HTTP 服务契约决定是否删减未被本地 Agent 直接消费的兼容字段；在此之前保留它们，避免破坏与远端配置的同步。
4. 核实生产环境对 `local://<absolute path>` 的真实路径语义。本地运行时当前只公开 `/skills` 与 `/mnt/data` 两个虚拟根，因此生产提示词若传入宿主机绝对路径会被拒绝。暂不分叉或修改提示词；待与 `agent-service` 的实现对齐后，决定是否在本地运行时增加兼容映射。
5. 如需执行不受信任的 Bash/Python，另行设计并验证各平台的进程/文件系统隔离；当前本地开发工具不承担这一安全边界。
