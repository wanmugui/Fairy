# V3 PPT 生成全流程：工具与 API 依赖梳理

# **V3 PPT 生成全流程：工具与 API 依赖梳理**



> 文档状态：初版
> 
> 版本基线：`agent-service` ，Chat Completions API 文档 v1\.8
> 
> 编制日期：2026\-07\-31
> 
> 
> 



## **1\. 文档目的与适用范围**



本文档基于当前项目版本，梳理 V3 PPT 生成全流程所依赖的全部工具、外部 API、脚本和产物文件，并逐一明确：



- 每个工具/API 在哪个流程节点被调用；

- 每个工具/API 的具体作用；

- 调用逻辑（入参、出参、超时、重试、终态信号、被谁调用）；

- 工具、脚本、产物之间的依赖链。

适用范围包括三个制作模式：



|模式|对应 Skill|最终产物|
|---|---|---|
|模板模式|`ppt-template-mode`|`htmls/page_xxx.html`|
|无模板模式|`ppt-no-template-mode`|`htmls/page_xxx.html` \+ `htmls/page_xxx.review.md`|
|创意模式|`ppt-creative-mode`|`pages/page_xxx.png`|



## **2\. 版本基线**



|基线项|位置 / 版本|
|---|---|
|Agent 3\.0 对外 API 文档|`docs/Agent_3.0_API_REFERENCE_V1.8.md`（`POST /api/v1/chat/completions`，SSE 流式）|
|Agent 核心引擎设计文档|`docs/Agent_Core.md`|
|工具 schema 定义|`configs/tools/schemas.json`|
|运行配置|`configs/config.yml`（本地）、`configs/config_dev_volces.yml`、`configs/config_prod_volces.yml`|
|统一工具服务配置|`agentV3.tools.unifiedToolService`（本地 `http://localhost:8080/api/agent/tool_call`，生产 `http://backend-api2:9000/api/agent/tool_call`）|
|PPT 制作 Skill|`skills/ppt-maker/SKILL.md` \+ `references/task_pack.md` \+ `references/info_pack.md`|
|模式 Skill|`skills/ppt-template-mode/SKILL.md`、`skills/ppt-no-template-mode/SKILL.md`、`skills/ppt-creative-mode/SKILL.md`|
|补充信息 Skill|`skills/deep-research/SKILL.md`、`skills/document-writing/SKILL.md`|



## **3\. 总体架构与调用链路**



```Plain Text
flowchart LR
    A[客户端 / 前端] -->|POST /api/v1/chat/completions SSE| B[Agent Service Go]
    B --> C[MainAgent 循环]
    C -->|create_subtask| D[SubtaskAgent 各阶段]
    C -->|LLM Stream/Complete| E[LLM Provider Clotho/OpenAI]
    C -->|ToolDispatcher| F[HTTP Tool / Builtin Tool]
    F -->|POST /api/agent/tool_call| G[backend 统一工具服务]
    D -->|execute_shell_command| H[模式 Skill 脚本]
    H -->|_tool_call.py 直连| G
    G -->|业务实现| I[外部 API: image_filter / html_page_generate / html_to_png / html_page_review / creative_page_render]
```



关键设计：



- Agent 引擎分三层执行单元：`MainAgent`（主 Agent）、`SubtaskAgent`（子任务 Agent）、`ReflectionAgent`（反思 Agent）。PPT 流程大量使用 `create_subtask` 把每个阶段派给子任务，避免主 Agent 上下文膨胀。

- 工具分两类：HTTP 工具（经统一工具服务执行，如 `read_file`、`image_search`、`image_generate` 等）和内置工具（`create_subtask`、`reflection`，由 Agent 引擎内部调度）。

- PPT 模式 Skill 的批处理脚本（`image_pipeline.py`、`html_page_generate_batch.py`、`html_page_review_batch.py`、`creative_page_render.py`）通过 `_tool_call.py` 直连同一套 `/api/agent/tool_call` 网关调用后端能力 API。

- 页面生成不再由模型手写 HTML/图片，统一收敛为「脚本并发调 API \+ 脚本 lint 门禁 \+ 单次重试」。

## **4\. 公共准备层（ppt\-maker）流程节点**



### **节点 0：参数确认**



- 工具：`ask_user`

- 调用逻辑：进入 `ppt-maker` 后先检查输入是否带 `<ppt_config>`；缺失时立即调用 `ask_user`，`ask_type=ppt_mode.confirm_params`，`questions=[]`，等待用户补齐参数。

- 作用：确认 `role / scene / audience / page_count_desc / ppt_mode / deck_dir / template_name / template_html_dir / template_tags_path` 等入口参数。

- 特性：`ask_user` 是独占轮次工具（`finish_tool=true`），调用后该轮不再执行其他工具，等待客户端续传 tool response。

### **节点 1：补充信息任务判定**



- 工具：`create_subtask`（委派深度研究/文件解析/数据分析子任务）、`read_file`（读取 `deep-research` 等 Skill）、`web_search` / `fetch_url`（子任务内使用，用于补文字信息）、`document_parser`（文件解析，能力存在时）。

- 调用逻辑：主 Agent 判定 `supplemental_info_tasks` 三个任务类型（`deepresearch` / `file_parsing` / `data_analysis`）的状态；状态为 `required` 时用 `create_subtask` 派发，payload 里给出任务边界、交付物路径、验收标准，并把对应 Skill 路径写入 todo。

- 硬约束：入口阶段禁止为配图调用 `image_search` / `image_generate`；`available_images` 只登记本就存在的图（用户上传、半成品报告内嵌），入口不预收集配图。

- 深度研究子任务内部按 `deep-research` Skill 执行：`ask_user`（澄清需求）→ 主线程拆步骤 → `create_subtask` 拆细分子任务 → 子任务用 `web_search` / `fetch_url` / `read_file` 收集证据 → 主线程综合；成文阶段按需读取 `document-writing` Skill。

### **节点 2：生成任务约束文件\`task\_pack\.json\`**



- 工具：`read_file`（读 `references/task_pack.md`）、`execute_shell_command`（跑校验脚本）、`write_file` / `edit_file`（主 Agent 写入产物，本节点允许主 Agent 直接写）。

- 脚本：`scripts/pack_preflight.py`

    ```Bash
    python scripts/pack_preflight.py --check task_pack --deck <deck_dir>
    ```

- 作用：固定任务边界、模式选择、语言判定留痕、内容缺口、补充任务门控、视觉策略与内容密度 profile。

- 校验逻辑：脚本检查必填字段、`language_decision` 自洽（`source / matched_signal / chosen_language / note`）、`visual_strategy` 枚举、`ppt_mode → mode` 映射、模板模式下的模板参数与路径；`exit_code=0` 才放行。

### **节点 3：生成信息素材文件\`info\_pack\.json\`**



- 工具：`read_file`（读 `references/info_pack.md`）、`execute_shell_command`、`write_file` / `edit_file`。

- 脚本：`pack_preflight.py`

    ```Bash
    python scripts/pack_preflight.py --check info_pack --deck <deck_dir>
    ```

- 作用：汇总用户输入、文件解析、数据分析、深度研究、半成品报告的内容依据与缺口，供下游大纲消费。

- 约束：只汇总、不重新判定补充任务；信息不足写 `info_gaps`，不虚构；`available_images` 仅登记既有图。

### **节点 4：分发前收口**



- 脚本：`pack_preflight.py`

    ```Bash
    python scripts/pack_preflight.py --check all --deck <deck_dir>
    ```

- 作用：同时校验 `task_pack.json` 与 `info_pack.json`，通过后按 `ppt_mode` 分发到 `ppt-template-mode` / `ppt-no-template-mode` / `ppt-creative-mode`。

- 约束：分发后不得绕过模式 Skill 的子任务流程。

### **进入模式 Skill 的强制读取规则**



- 主 Agent 进入任一模式 Skill 前必须完整读目标 `SKILL.md`：首次 `read_file` 用 `offset=0` 且 `length >= 2000`；若返回 `is_truncated=true` 或未出现结束标记，用新的 `offset` 继续读到文件结束，期间不得插入其他工具调用。

- 每个阶段 subagent 进入工作前必须完整读对应 `stages/<阶段>.md`，不得只依赖 `SKILL.md` 的阶段摘要。

- 主 Agent 发起 `create_subtask` 后必须等待返回并验收产物，再进入下一阶段；subtask 最终返回消息必须且只能包含四行（完成状态 / 产物路径 / 自检结果 / 未解决项），总字符数不超过 200。

## **5\. 模板模式（ppt\-template\-mode）节点拆解**



|阶段|执行者|工具 / 脚本 / API|作用|
|---|---|---|---|
|1 建立上下文 \+ 前置 gate|主 Agent|`read_file`（task\_pack/info\_pack）；`execute_shell_command` 跑 `preflight.py`|承接输入；校验 deck 目录、task\_pack/info\_pack、模板参数、模板 HTML 目录至少含一个 `*.html`；`exit_code=0` 才能派发阶段 2|
|2 模板编排|`template-outline` subagent|`read_file`（stage md、`tag_gen_results.json`）；`write_file`（slim `template_map.json`）；`execute_shell_command` 跑 `enrich_template_map.py`|每页选择模板 id 并写 `template_map.json`；enrich 脚本自动补 `template_page_type / layout_category / template_constraints / template_raw_constraints` 并做 role/page\_type 硬校验（exit 2 需重排模板后重跑）|
|3 大纲生成|同一 `template-outline` subagent|`read_file`（stage md、info\_pack、template\_map）；`write_file`（`outline.json`）|按模板槽位生成 `outline.json`：sub\_point 绑定 `slot_id`、铸 `picture-NNN`、控制 chart/table/picture 不超过模板上限|
|大纲确认|主 Agent|`ask_user`（`ask_type=ppt_mode.confirm_outline`）|让用户确认大纲，确认后进入阶段 4|
|4 资产规划|`asset-planning` subagent|`execute_shell_command`（mkdir）；`image_search`（每槽 top\_k=5 取候选）；`write_file`（`_candidates.json`）；`build_pipeline_spec.py`；`image_pipeline.py`（内部 `fetch_url` stateless 下载 \+ 4 条代码检查 \+ `image_filter` 视觉检查）；`image_generate`（兜底生图，结束页背景优先）；`web_search` / `fetch_url`（内容补充）；`write_file`（`asset_map.json`）|产出真实可用的本地图片索引 `asset_map.json`；搜图来源必须经 `image_pipeline.py`|
|5 前置切片|主 Agent|`execute_shell_command` 跑 `slice_stage5_inputs.py`|从 template\_map/outline/asset\_map 生成每页 `htmls/page_xxx.input.json`，并复制模板静态目录（htmls\_png/user/themes）；stdout 返回 `page_count` 供验收|
|5 页面生成|单个 `html-generation` subagent|`execute_shell_command` 跑 `html_page_generate_batch.py`（`--mode template`）；内部调 `html_page_generate` API；内部跑 `lint_page_html.py`|并发 4 组装 prompt（prompt 模板 \+ 本页 JSON \+ 参考模板 HTML 全文）→ 调 API 出 HTML → 落盘 → lint；lint 不过追加问题重出一次|
|收口|主 Agent|`execute_shell_command`（`test`/目录检查）|检查 `htmls/` 产物齐全，输出 `<ppt_task_finished>`|



### **阶段 5 页面生成细节**



命令：

```Bash
python scripts/html_page_generate_batch.py \
  --deck <deck_dir> \
  --mode template \
  --prompt prompts/html_gen_template.md \
  --concurrency 4
```



每页流程：



1. 读 `htmls/page_xxx.input.json`；

2. 组装 prompt = `html_gen_template.md` 全文 \+ 本页 `outline_page / template_map_page / asset_map_page` JSON \+ 参考模板 HTML 全文；

3. 调 `html_page_generate`（`{"prompt": ...}`），取返回 `{"html": ...}`，去掉 markdown 包裹后写 `htmls/page_xxx.html`；

4. 跑 `lint_page_html.py --output ... --template ...`；

5. lint 失败则把 lint 问题追加进 prompt，重出一次；仍失败标记 `failed_pages`（HTML 仍保留）。

> 模板模式不执行 review/rewrite（SKILL 高层约束），阶段 5 生成的 `htmls/page_xxx.html` 即为最终产物。
> 
> 



## **6\. 无模板模式（ppt\-no\-template\-mode）节点拆解**



|阶段|执行者|工具 / 脚本 / API|作用|
|---|---|---|---|
|1 建立上下文|主 Agent|`read_file`（task\_pack/info\_pack、用户图片路径）|承接输入|
|2 设计信息生成|`style-outline` subagent|`read_file`（stage md、`references/design-styles.md`）；`write_file`（`style_spec.json`）|从 74 风格 / 22 色调 / 29 主色中选型，生成完整视觉规范|
|3 大纲生成|同一 `style-outline` subagent|`read_file`（stage md）；`write_file`（`outline.json`）；`execute_shell_command` 跑 `outline_preflight.py`|生成大纲并做确定性自检（页码连续、picture\-NNN 唯一、视觉软下限、结尾页收敛等）；fail 则整体重写再跑一次|
|大纲确认|主 Agent|`ask_user`（`ppt_mode.confirm_outline`）|用户确认大纲|
|4 资产规划|`asset-planning` subagent|`execute_shell_command`（mkdir assets）；`image_search`（每槽 top\_k=5 取候选）；`write_file`（`_candidates.json`）；`build_pipeline_spec.py`；`image_pipeline.py`（内部 `fetch_url` stateless 下载 \+ 4 条代码检查 \+ `image_filter` 视觉检查）；`image_generate`（兜底生图，结束页背景优先）；`web_search` / `fetch_url`（内容补充）；`write_file`（`asset_map.json`）|产出 `asset_map.json`（含 `background_picture` 整页底图槽）；搜图来源必须经 `image_pipeline.py`|
|5 前置切片|主 Agent|`execute_shell_command` 跑 `slice_stage5_inputs.py`|生成每页 `htmls/page_xxx.input.json`（含 `style_spec` / `outline_page` / `asset_map_page`）|
|5 页面生成|单个 `html-generation` subagent|`html_page_generate_batch.py`（`--mode no_template`）；内部 `html_page_generate` API \+ `lint_pages.py`|并发 4 生成 `htmls/page_xxx.html`|
|6\+7 页面复核 \+ 改写|单个 `page-review-polish` subagent|`html_page_review_batch.py`；内部 `html_to_png`（stateless）→ `html_page_review` → 按需 `html_page_generate` rewrite → `lint_pages.py`|产出 `htmls/page_xxx.review.md`；`needs_rewrite` 的页原地重出修正版|
|收口|主 Agent|`execute_shell_command`|检查产物齐全，输出 `<ppt_task_finished>`|



### **阶段 5 页面生成细节**



命令：

```Bash
python scripts/html_page_generate_batch.py \
  --deck <deck_dir> \
  --mode no_template \
  --prompt prompts/html_gen_no_template.md \
  --concurrency 4
```



画布默认 `1280x720`；仅当任务约束明确要求 `1600x900` 时，生成与复核命令都加 `--canvas 1600x900`。



### **阶段 6\+7 复核与改写细节**



命令：

```Bash
python scripts/html_page_review_batch.py \
  --deck <deck_dir> \
  --review-prompt prompts/html_review_prompt.md \
  --gen-prompt prompts/html_gen_no_template.md \
  --concurrency 4
```



每页流程：



1. 读 `page_xxx.html`，把 `../assets/` 本地图内联成 `data:` URL；

2. 调 `html_to_png`（`{"mode":"stateless","html_file_content": 内联 HTML}`），拿 `png_base64`（过大缩到 ≤1600px）；

3. 调 `html_page_review`（`{"prompt": 评审 prompt, "image_base64": ...}`），解析返回 JSON；

4. 写 `page_xxx.review.md`（`is_ok / score / needs_rewrite / issues / suggestion`）；

5. `needs_rewrite=true` 且有 issues：把当前 HTML 全文 \+ issues 追加进生成 prompt，调 `html_page_generate` 重出整页 → 覆盖 → 重跑 `lint_pages.py`；

6. 每页只 review \+ rewrite 一次，不循环。

## **7\. 创意模式（ppt\-creative\-mode）节点拆解**



|阶段|执行者|工具 / 脚本 / API|作用|
|---|---|---|---|
|1 建立上下文|主 Agent|`read_file`|承接 task\_pack/info\_pack、用户图片|
|2 风格生成|`style-outline` subagent|`read_file`（`prompts/query2style.md` 或 `prompts/image2style.md`）；可选 `image_vqa` 读参考图；`write_file`（`style_spec.md`）|生成 Markdown 视觉风格与美术指导；有用户图用 `image2style.md`，无图用 `query2style.md`|
|3 大纲生成|同一 subagent|`read_file`（stage md）；`write_file`（`outline.json`）|生成创意大纲，含 `needed_pictures` 需求声明（不搜图）|
|大纲确认|主 Agent|`ask_user`（`ppt_mode.confirm_outline`）|用户确认大纲|
|4 前置切片|主 Agent|`execute_shell_command` 跑 `slice_page_inputs.py`|每页生成 `pages/page_xxx.input.json`，内联 `style_spec.md` 全文，把 `needed_pictures` 映射为 API 旧格式|
|4 页面渲染|每页一个 `page-render` subagent|`execute_shell_command` 跑 `creative_page_render.py`|调 `creative_page_render` API 出 `pages/page_xxx.png`（脚本最多 3 次尝试）|
|收口|主 Agent|`execute_shell_command`|检查 PNG 齐全，输出 `<ppt_task_finished>`|



渲染命令：

```Bash
python scripts/creative_page_render.py --input <deck_dir>/pages/page_xxx.input.json
```



> 渲染脚本会创建图片文件，subagent 调用前需按文件操作预播报协议（File Action Protocol）通知。
> 
> 



## **8\. 工具清单（含流程节点与调用逻辑）**



### **8\.1 工具总览**



|工具|类型|主要流程节点|核心作用|
|---|---|---|---|
|`ask_user`|HTTP 工具（finish\_tool）|ppt\-maker 参数确认；三种模式的大纲确认|向用户确认参数/大纲；独占轮次|
|`create_subtask`|内置工具（SignalDelegate）|补充信息任务；三种模式各阶段|派发独立子任务给 SubtaskAgent|
|`reflection`|内置工具（SignalDelegate）|一般不用于 PPT 流程（模式 Skill 红线禁止）|触发反思 Agent|
|`read_file`|HTTP 工具|入口；各阶段读 stage md / prompt / 产物|读取本地、知识库、memory 文件|
|`write_file` / `edit_file`|HTTP 工具|各阶段子任务写产物|写/改阶段产物（主 Agent 被红线禁止写页面级产物）|
|`bash`（技能文档亦称 `execute_shell_command`）|HTTP 工具|入口校验；各阶段跑脚本|执行 Python 脚本、mkdir、cp、test|
|`execute_code`|HTTP 工具|一般不在 PPT 流程中使用（红线禁止手写 HTTP/图片处理）|沙箱执行 Python|
|`web_search`|HTTP 工具|深度研究子任务；阶段 4 内容补充|发现式搜索，返回候选链接|
|`fetch_url`|HTTP 工具|深度研究；阶段 4 内容补充；`image_pipeline.py` 下载图片（stateless）|抓取网页/文件；`file_mode=stateless` 返回 `file_base64`|
|`image_search`|HTTP 工具|阶段 4 资产规划|按关键词取候选图（`top_k` 默认 5）|
|`image_generate`|HTTP 工具|阶段 4 资产规划兜底|生图并落盘|
|`image_vqa`|HTTP 工具|创意模式阶段 2（可选读参考图）|视觉问答；阶段 4 明确禁止用它判图|
|`image_filter`|外部 API（经 `_tool_call.py`）|阶段 4 图片视觉检查|水印/文字/清晰度/语义检查，返回 `pass/reason`|
|`html_page_generate`|外部 API（经 `_tool_call.py`）|阶段 5 生成；无模板阶段 7 rewrite|输入 prompt 返回整页 HTML|
|`html_to_png`|外部 API（经 `_tool_call.py`；同时是 `schemas.json` 注册的 HTTP 工具）|无模板阶段 6 截图|`mode=stateless` 传 HTML 字符串返回 `png_base64`|
|`html_page_review`|外部 API（经 `_tool_call.py`）|无模板阶段 6 评审|截图视觉评审，返回 JSON 评审结果|
|`creative_page_render`|外部 API（经 `creative_page_render.py`）|创意模式阶段 4|输入切片返回 `result_page_image_base64`，落盘 PNG|
|`document_parser`|HTTP 工具|补充信息任务（文件解析，能力存在时）|解析文档为结构化文本|
|`todolist_create/append/update`|HTTP 工具（finish\_tool）|PPT 流程禁止使用|任务清单维护；`ppt-maker` 明确要求 PPT 模式不调用|
|`knowledge_retrieve` / `knowledge_download` / `memory_search` / `ls_directory` / `retrieve` / `glob`|HTTP 工具|平台通用能力，非 PPT 核心节点|知识库检索/下载、记忆检索、目录浏览、glob|



### **8\.2 关键工具调用逻辑**



**\#\#\#\# \`ask\_user\`**



- Schema：`configs/tools/schemas.json` 中 `ask_user`，`ask_type` 枚举 `select_mode / ppt_mode.confirm_params / ppt_mode.confirm_outline`。

- 调用点：入口参数缺失 → `confirm_params`；大纲生成完成 → `confirm_outline`（两种确认模式 `questions=[]`）。

- 引擎行为：配置 `finish_tool=true`，调用后 Agent 循环返回 `finish_reason="tool_calls"`，等客户端续传 tool response；独占轮次，同一轮不执行其他工具。

**\#\#\#\# \`create\_subtask\`**



- 内置实现：`internal/biz/tool/builtin/create_subtask.go`。

- 入参：`title / goal / todo`（必填）\+ `relevant_files / criteria / skill / addition`（可选，内部交接通道）。

- 返回：`SignalDelegate`，由 Scheduler 启动 `SubtaskAgent`；返回消息按工具规范（PPT Skill 要求四行：完成状态/产物路径/自检结果/未解决项）。

- 约束：子任务 Agent 禁用 `create_subtask / reflection / todolist_* / ask_user`（`subtaskForbiddenTools`），因此阶段子任务不能嵌套派发、不能反问用户。

- 并发：同一轮多个 `create_subtask` 默认并行（`parallel_subtasks` 默认 true）；创意模式阶段 4 按页派发多个 page\-render 子任务即依赖此机制。

**\#\#\#\# \`read\_file\`**



- Schema 要求路径带协议前缀：`local://`、`knowledge://`、`memory://`；支持 UTF\-8/GBK 等多编码。

- PPT 流程用途：读 Skill/SKILL\.md、stage md、prompt、references、`task_pack.json` / `info_pack.json`、模板配置等。

- 红线：主 Agent 禁止用 `read_file` 读阶段产物正文（`template_map.json`、`outline.json`、`asset_map.json`、`htmls/*.html`、模板目录递归等），由 stage subagent 按 stage md 自行消费；收口只允许 `test -f/-d` 与 `size` 检查。

**\#\#\#\# \`bash\`（\`execute\_shell\_command\`）**



- 主 Agent 合法动作：跑 `pack_preflight.py`、`preflight.py`、`slice_stage5_inputs.py`、`slice_page_inputs.py`、`test -f/-d`、目录检查。

- 阶段 subagent 合法动作：跑 `enrich_template_map.py`、`outline_preflight.py`、`build_pipeline_spec.py`、`image_pipeline.py`、`html_page_generate_batch.py`、`html_page_review_batch.py`、`creative_page_render.py`，以及 `mkdir / cp / test -s`。

- 红线：禁止主 Agent 用 shell 修子任务产物；禁止 subagent 自写 `requests.post` 绕过脚本调 API。

- 引擎：超时配置 `timeout: 900`（bash）、`300`（execute\_code）、`600`（image\_generate）；5xx/429 按指数退避重试；`bash` / `execute_code` 不重试。

**\#\#\#\# \`web\_search\` / \`fetch\_url\`**



- `web_search`：只做发现，返回候选链接/摘要；不把摘要当最终证据，需 `fetch_url` 读全文。有 `maxConsecutiveWebToolCalls: 4` 连续调用预算。

- `fetch_url`：抓取 URL 内容；`image_pipeline.py` 下载图片时传 `{"url": ..., "file_mode": "stateless"}`，后端返回 `file_base64`（沙箱直连外网不通，统一借后端下载）。

- `disable_web_search=true` 时这两个工具从本轮工具列表中被过滤。

**\#\#\#\# \`image\_search\` / \`image\_generate\` / \`image\_vqa\`**



- `image_search`：阶段 4 每槽位最多调 2 次（首次 0 候选允许换一次近义关键词），`top_k=5`；只负责取候选，入选判定交给 `image_pipeline.py`。

- `image_generate`：阶段 4 兜底；结束页背景优先走生图；每槽最多 2 次；输出直接落 `{deck_dir}/assets/<asset_id>.<ext>`。

- `image_vqa`：仅创意模式阶段 2 可选用于读参考图；阶段 4 禁止用它逐张判图。

## **9\. 外部 API 清单**



所有 PPT 批处理脚本通过 `_tool_call.py`（或 `creative_page_render.py`）调用统一网关：



```Plain Text
POST /api/agent/tool_call
请求：{"tool_call_id": "...", "tool_name": "...", "arguments": "<JSON 字符串>"}
响应（包装格式）：{"code": 0, "data": {"tool_call_id": "...", "result": "<JSON 字符串>"}}
```



> 说明：`html_to_png` 同时是 `configs/tools/schemas.json` 中注册的 HTTP 工具（LLM 也可直接调用）；`image_filter` / `html_page_generate` / `html_page_review` / `creative_page_render` 未在 `schemas.json` 注册，当前仅由批处理脚本经网关调用。
> 
> 



端点解析优先级：



|场景|优先级|
|---|---|
|模板/无模板脚本（`_tool_call.py`）|`PPT_TOOL_API_URL` → `PPT_TOOL_API_BASE` → `BACKEND_TOOL_BASE` → 默认 `https://code-stage.xiaohuanxiong.com`|
|创意渲染（`creative_page_render.py`）|`CREATIVE_RENDER_API_URL` → `BACKEND_TOOL_BASE` → 默认 `https://code-dev-public.xiaohuanxiong.com`|



Host pin（沙箱 DNS 不可达时钉到可达 IP）：



|Host|固定 IP|覆盖方式|
|---|---|---|
|`code-stage.xiaohuanxiong.com`|`180.153.172.2`|`PPT_TOOL_HOST_IP="host=ip"`|
|`code-dev-public.xiaohuanxiong.com`|`14.103.45.137`|脚本内置|



### **9\.1\`image\_filter\`**



|项|值|
|---|---|
|调用方|`image_pipeline.py`（阶段 4）|
|arguments|`{"prompt": "<check_prompt>", "image_base64": "<jpeg base64>"}`|
|响应|`{"pass": bool, "reason": str}`（脚本兼容 `ok`）|
|作用|视觉检查：明显水印、大段印刷文字、低清模糊、语义跑偏四类硬性不可用|
|判定三态|`pass` / `reject` / `error`；`error` 不算语义不通过，仅在槽位无任何通过候选时按序兜底（`vision_unavailable_default_pass`）|
|输入图片|送检前缩到最长边 ≤1024、JPEG q85，控 5MB 上限|
|超时/重试|`_tool_call.py` 默认 600s；最多 3 次尝试（含首次，退避 2s/4s/8s）|



### **9\.2\`html\_page\_generate\`**



|项|值|
|---|---|
|调用方|`html_page_generate_batch.py`（阶段 5）；`html_page_review_batch.py`（阶段 7 rewrite）|
|arguments|`{"prompt": "<完整 prompt>"}`|
|响应|`{"html": "<完整 HTML 文本>"}`|
|作用|按 prompt 生成一页 PPT HTML（模板模式含整份参考模板 HTML；无模板模式含 style\_spec/outline/asset\_map 切片）|
|失败处理|脚本去掉 markdown 包裹；空结果抛错；lint 不过追加问题重出一次|



### **9\.3\`html\_to\_png\`**



|项|值|
|---|---|
|调用方|`html_page_review_batch.py`（无模板阶段 6）|
|arguments|`{"mode": "stateless", "html_file_content": "<内联图片后的 HTML 字符串>"}`|
|响应|`{"png_base64": "..."}`|
|作用|远端渲染页面截图；stateless 模式不读沙盒，本地图必须先内联成 `data:` URL|
|大小保护|超过 4\.5MB 解码缩到最长边 ≤1600、JPEG q85 再送评审|



### **9\.4\`html\_page\_review\`**



|项|值|
|---|---|
|调用方|`html_page_review_batch.py`（无模板阶段 6）|
|arguments|`{"prompt": "<评审 prompt>", "image_base64": "<png/jpeg base64>"}`|
|响应|`{"review": "<JSON 文本>"}`，字段 `is_ok / score / needs_rewrite / issues / suggestion`|
|作用|截图视觉评审；解析失败视为评审失败（fail\-closed），不静默通过|
|issue 码|`OUT_OF_PAGE / OVERLAP / CLIPPED / EMPTY_BLOCK / WEAK_MOTIF / FAKE_IMAGE` 等|



### **9\.5\`creative\_page\_render\`**



|项|值|
|---|---|
|调用方|`creative_page_render.py`（创意模式阶段 4）|
|arguments|`{"mode": "stateless", "ppt_id": ..., "ppt_title": ..., "page_num": ..., "page_outline": "<JSON 字符串>", "page_style": "<style_spec.md 全文>"}`|
|响应|`{"result_page_image_base64": "..."}`（支持 `data:` 前缀剥离）|
|作用|生成一页创意 PNG|
|重试|最多 3 次尝试（含首次，退避 2s/4s/8s）；全部失败 exit\_code=1|



## **10\. 脚本清单**



所有命令均以对应 Skill 目录为工作目录（模式 Skill 文档明确要求从 skill 所在目录执行），`--deck` / `--outline` / `--spec` 等路径参数传绝对路径。



|脚本|所在 Skill|触发节点|调用方式|作用 / 输出|
|---|---|---|---|---|
|`pack_preflight.py`|ppt\-maker|入口节点 2/3/4|`--check task_pack / info_pack / all`|校验两个入口产物；exit 0/2|
|`preflight.py`|ppt\-template\-mode|阶段 1 gate|`--deck <deck_dir>`|校验模板模式前置条件；exit 0/2|
|`enrich_template_map.py`|ppt\-template\-mode|阶段 2|`--template-map ... --tags ...`|补全模板约束字段 \+ role/page\_type 硬校验；exit 0/2|
|`outline_preflight.py`|ppt\-no\-template\-mode|阶段 3 自检|`--deck <deck_dir>`|大纲确定性自检（errors/warnings）；exit 0/2|
|`build_pipeline_spec.py`|模板/无模板|阶段 4|`--outline ... --candidates ... --check-prompt ... --deck ... --out ...`|从 outline 自动拼 `image_pipeline` spec，避免 LLM 手写大 JSON|
|`image_pipeline.py`|模板/无模板|阶段 4|`--spec ... [--concurrency 16]`|并发下载\+4 条代码硬检查\+`image_filter` 视觉检查，选图落 `assets/`，输出 `results`；退出码恒为 0（除非 spec 不可读/非法），单槽失败以 `local_path=null` 表达|
|`slice_stage5_inputs.py`|模板/无模板|阶段 5 前置|`--deck <deck_dir>`|生成 `htmls/page_xxx.input.json`；模板模式顺带复制静态目录|
|`slice_page_inputs.py`|ppt\-creative\-mode|阶段 4 前置|`--deck <deck_dir>`|生成 `pages/page_xxx.input.json`，映射 API 字段|
|`html_page_generate_batch.py`|模板/无模板|阶段 5|`--deck ... --mode template/no_template --prompt ... [--concurrency 4]`|并发生成所有页 HTML \+ lint \+ 单次重出|
|`lint_page_html.py`|ppt\-template\-mode|阶段 5 内部|由 batch 脚本调用|模板模式 HTML 结构 lint|
|`lint_pages.py`|ppt\-no\-template\-mode|阶段 5/6 内部|`--deck ... [--page N] [--json]`|无模板模式 24 条静态规则 lint|
|`html_page_review_batch.py`|ppt\-no\-template\-mode|阶段 6\+7|`--deck ... --review-prompt ... --gen-prompt ... [--concurrency 4]`|截图评审 \+ 按需 rewrite \+ lint|
|`creative_page_render.py`|ppt\-creative\-mode|阶段 4|`--input <page_xxx.input.json>`|调渲染 API 落 PNG|
|`_tool_call.py`|模板/无模板|批处理内部|被各脚本 import|统一工具网关薄客户端（端点解析/host pin/最多 3 次尝试，退避 2s/4s/8s）|



`test_pack_preflight.py` 是 `pack_preflight.py` 的单元测试，不参与运行流程。



## **11\. 产物与数据依赖链**



```Plain Text
用户输入 + <ppt_config>
  → task_pack.json（入口任务包，ppt-maker 产出）
  → info_pack.json（内容素材包，ppt-maker 产出）

模板模式：
  task_pack + tag_gen_results.json → template_map.json（阶段 2）
  template_map + info_pack → outline.json（阶段 3）
  outline + image_search/image_generate → asset_map.json（阶段 4）
  template_map + outline + asset_map → htmls/page_xxx.input.json（阶段 5 前置）
  slice + 模板 HTML + prompt → htmls/page_xxx.html（阶段 5）

无模板模式：
  task_pack + info_pack + design-styles.md → style_spec.json（阶段 2）
  style_spec + info_pack → outline.json（阶段 3）
  outline + image_search/image_generate → asset_map.json（阶段 4）
  style_spec + outline + asset_map → htmls/page_xxx.input.json（阶段 5 前置）
  slice + prompt → htmls/page_xxx.html（阶段 5）
  html + html_to_png + html_page_review → htmls/page_xxx.review.md（阶段 6）
  html + issues → html_page_generate rewrite → htmls/page_xxx.html（阶段 7）

创意模式：
  task_pack + info_pack + user_images → style_spec.md（阶段 2）
  style_spec + info_pack → outline.json（阶段 3）
  style_spec + outline → pages/page_xxx.input.json（阶段 4 前置）
  slice → creative_page_render → pages/page_xxx.png（阶段 4）
```



统一命名：`page_xxx` 三位页码（`page_001` 起）；配图槽 `picture-NNN`；资产规范名 `<asset_id>.<ext>`。



## **12\. 关键调用机制与硬约束**



### **12\.1 Agent 工具过滤**



- `MainAgent`：默认全量已启用工具；`systemAgentForbiddenTools`（`retrieve` / `ls_directory`）在普通模式下禁用。

- `SubtaskAgent`：禁用 `create_subtask`、`reflection`、`todolist_create/append/update`、`ask_user`。

- `ReflectionAgent`：同上禁用列表。

- 请求级过滤：`tools` 白名单、`AllowedTools`、`disable_web_search`（过滤 `web_search` / `fetch_url`）。

### **12\.2 终态与独占机制**



- `finish_tool=true`：`ask_user`、`todolist_*`；调用后返回 `finish_reason="tool_calls"`，等客户端续传。

- `SignalDelegate`：`create_subtask`、`reflection`；由 Scheduler 创建子 Agent，结果回填。

- PPT 完成通知：唯一合法收口格式 `<ppt_task_finished>`（`deck_dir / status / failed_pages / reason`）。

### **12\.3 超时与重试**



- Agent 引擎：`totalTimeout=7200s`、`turnTimeout=6000s`、`globalLoopsLimit=50`、`subtaskMaxLoops=20`、`reflectionMaxLoops=8`、`maxConsecutiveWebToolCalls=4`。

- Go HTTP 工具：统一工具服务默认超时 120s；`retryCount=2` 表示最多 2 次尝试（重试 1 次），5xx/429 按指数退避重试；`bash`/`execute_code` 不重试。

- Python 批处理脚本：`_tool_call.py` 与 `creative_page_render.py` 均为最多 3 次尝试（含首次，退避 2s/4s/8s）；`image_pipeline.py` 下载图片时 `fetch_url` 单次超时 15s。

### **12\.4 质量门禁**



- 阶段产物都有确定性校验脚本（`pack_preflight.py`、`preflight.py`、`enrich_template_map.py`、`outline_preflight.py`），`exit_code=0` 才能进入下一阶段。

- 页面 HTML 必须过 lint（模板 `lint_page_html.py`；无模板 `lint_pages.py`），失败允许一次重出，再失败进失败名单。

- 图片必须经 `image_pipeline.py` 的 4 条代码检查（≥8KB、≥200×200、像素多样性 ≥8 色桶、跨槽 sha256 去重）\+ `image_filter` 视觉检查。

### **12\.5 PPT 流程红线（主 Agent）**



- 禁止主 Agent 直接写阶段产物（`template_map.json` / `style_spec` / `outline.json` / `asset_map.json` / 页面 HTML / PNG）。

- 禁止主 Agent 读阶段产物正文做二次核实、禁止用 `reflection` 复查、禁止手工修子任务产物。

- 禁止绕过脚本自写 HTTP 调 API、禁止手写 HTML、禁止用 `execute_code`/VQA 伪造图片。

- PPT 模式禁止调用 todolist 工具。

## **13\. 输出物管理规则（飞书）**



### **13\.1 文档存储要求**



1. 所有梳理完成的流程文档、需求文档统一上传至指定飞书页面（页面地址待指定，见文末「待确认事项」）。

2. 各成员可在对应页面下新建子页面补充内容，不允许覆盖他人页面正文；补充内容以独立子页面呈现。

3. 文档命名建议：`[主题]_[日期]_[版本].md`；同一文档只保留一份权威版本，更新后在页面顶部标注版本与更新人。

4. 本仓库内保留一份源文件（`V3_PPT生成全流程工具与API依赖梳理.md`），飞书页面作为团队协作副本；两者需同步更新。

建议的飞书页面结构：



```Plain Text
V3 PPT 生成流程（根页面，URL 待指定）
├── 01 全流程工具与 API 依赖梳理   ← 本文档上传位置
├── 02 需求文档
│   ├── 需求 A（每需求一个子页面）
│   └── 需求 B
├── 03 沟通与同步记录
└── 04 版本变更记录
```



### **13\.2 沟通机制要求**



1. 项目推进过程中遇到问题需随时同步，不等待固定会议节点。

2. 阻塞性问题（API 不可用、模板配置缺失、验收不通过、依赖变更）第一时间在项目飞书群/指定频道同步，并给出影响面与建议方案。

3. 文档评审意见直接追加到对应飞书子页面评论区或独立子页面，避免散落在会话中。

## **14\. 待确认事项**



|事项|说明|
|---|---|
|指定飞书页面 URL|需要提供根页面地址，用于上传本文档并建立子页面结构|
|上传格式|确认直接粘贴 Markdown，还是转换为飞书原生文档|
|版本跟进|后续 API/skill 变更时由谁维护本文档并更新飞书副本|
|RTL 语言支持|`task_pack.md` 已注明当前对 RTL 语言（ar/he 等）未适配，开放对应 language 前需先补渲染链路|



## **15\. 核对说明**



- 工具覆盖：`configs/tools/schemas.json` 注册的 24 个工具已全部列入第 8\.1 节；PPT 流程实际使用、禁止使用或仅平台通用，均已标注。

- 外部 API 覆盖：5 个能力 API（`image_filter` / `html_page_generate` / `html_to_png` / `html_page_review` / `creative_page_render`）全部列入第 9 节；其中 `html_to_png` 同时是注册工具 schema。

- 脚本覆盖：14 个参与流程的脚本全部列入第 10 节；`test_pack_preflight.py` 为单元测试，不参与运行流程。

- 依据来源：本文档所有工具名、脚本名、端点、参数、超时/重试、阶段映射均以仓库内 `SKILL.md`、`stages/*.md`、`scripts/*.py`、`configs/tools/schemas.json`、`configs/*.yml`、`docs/Agent_3.0_API_REFERENCE_V1.8.md` 为准，未添加来源之外的内容。

