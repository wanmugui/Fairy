---
name: ppt-template-mode
description: 当 PPT 任务已确认使用模板模式（ppt_mode=template），且 task_pack.json、info_pack.json、模板配置文件和模板 HTML 目录均就绪时使用。负责模板编排、大纲生成、资产规划和逐页 HTML 生成全链路。只要 ppt-maker 分发到模板模式，都必须进入本 Skill。
---

# PPT 模板模式 Skill


## 🚫 最高优先级红线：主 agent 读文件禁区

**本节与下节并列为最高优先级规则。**

主 agent **禁止**用 `read_file` / `document_parser` / `execute_shell_command`（含 `cat` / `head` / `less` / `find` / `ls -R`） / `image_vqa` 读取或探查以下文件或目录的**正文**。仅允许 `test -f/-d` 做存在性检查，或把这些路径作为字符串传进 `subtask` 输入包。

禁读清单：

- 模板目录下任何文件与子目录，**包括但不限于**：`tag_gen_results.json`、`htmls/*.html`、`htmls_png/*.png` / `user/*.png`（**严禁 `image_vqa`**）、模板目录的 `find` / `ls -R` / `tree` 递归列举。模板结构与样式由阶段 subagent 按 `stages/*.md` 指引自行消费——主 agent 不需要也不允许自己"先看看模板长什么样"。
- `stages/*.md`（只由对应阶段 subagent 读取）。
- deck 工作目录下的阶段产物：`template_map.json`、`outline.json`、`asset_map.json`、`htmls/page_xxx.input.json`、`htmls/page_xxx.html`，以及 subagent 在 stage md 中要求读的任何中间输出。

主 agent 合法动作仅限：读 `task_pack.json` / `info_pack.json`、`test -f/-d` 前置检查、调本 SKILL.md 规定的脚本（包括 `scripts/outline_md_generate.py`、`scripts/outline_md_preflight.py`、`scripts/preflight.py`、`scripts/enrich_template_map.py`、`scripts/slice_stage5_inputs.py`）、调 `create_subtask`（路径以字符串塞进输入包）、通过 subagent 的 `[返回格式]` 汇报项验收产物。

**自检触发点**：若发现自己即将读取禁读清单中的文件（无论动机），立即停止；改走 subagent 返回结果或按字符串路径构造 `subtask` 输入。

## 🚫 最高优先级红线：主 agent 禁止直接产出阶段文件

从阶段 2 开始，以下文件**必须**由对应阶段的 subagent 通过 `create_subtask` 启动后产出；主 agent 用 `execute_code` / `execute_shell_command` / 任何写文件能力**直接写入**这些文件，即视为违反本 Skill：

- `template_map.json`（阶段 2，由 template-outline subagent 产出）
- `outline.json`（阶段 3，由同一个 template-outline subagent 产出）
- `asset_map.json`（阶段 4，由 asset-planning subagent 产出）
- `htmls/page_xxx.html`（阶段 5，由一个 html-generation subagent 跑批量脚本并发产出所有页）

**自检触发点**：若发现自己将在 subtask 外直接写入以上文件，立即停止，改用 `create_subtask` 启动对应阶段的 subagent。禁止以"节省时间 / 避免上下文膨胀"为由绕过。

## Skill 目标与边界

在 `task_pack.json`、`info_pack.json` 和 deck 工作目录就绪后，执行模板约束下的 PPT HTML 生成链路。本 `SKILL.md` 负责模式边界、阶段顺序、subagent 调度；字段定义、单阶段处理细节、页面生成硬性要求放在 `stages/*.md`。最终产物是阶段 5 生成的 `htmls/page_xxx.html`，不是单页图片或"最终 json"。

## 输入、输出守则与前置条件

输入：

- `task_pack.json`、`info_pack.json`
- `task_pack.json.template_params.template_name`
- `task_pack.json.template_params.template_tags_path`（模板配置文件绝对地址，用于阶段 2 编排）
- `task_pack.json.template_params.template_html_dir`（模板 HTML 目录绝对地址，目录内文件名为 `<template_id>.html`，用于阶段 5 生成）

输出守则

**所有阶段性回复都必须先满足本节**, 本节只约束写在 assistant 文本里、用户能直接看到的句子；**不约束**工具参数、`<cite>` 标签、`<ppt_task_finished>` 协议 XML、子任务 payload 中的 `<goal>` / `<todo>` 等内部结构——这些该出现的地方按工具规范照常出现。

**禁止**在用户可见文本里出现：

- 任何内部字段名、文件名、工具名、脚本名、模式枚举值（包括但不限于：任务约束/信息素材文件本身的英文名、调研/搜索/抓取/委派/确认工具的英文名、各类策略枚举值、模板字段名、subagent / subtask / stage md 等内部术语）。
- 工作目录绝对路径、UUID（如 `pptid_xxxxxxxx-...`）、`deck_dir` 等路径变量名。
- 中英文术语混杂。

**判定CoT只能用中文。**复述阶段进展或判定结论时（例如"模板编排好了，开始排大纲"、"页面在生成"），必须用纯中文自然语言，禁止携带任何英文字段名 / 枚举值 / 工具名 / 文件名（如 `template_map` / `outline` / `asset_map` / `subtask` / `tag_gen_results` 等）。派发子任务时**不要复述 payload**：不要把交付路径、`<criteria>` / `<goal>` / `<todo>` 段内容、`<result>` 返回要求念给用户。

进入本 Skill 前必须同时满足：`task_pack.json`、`info_pack.json`、deck 工作目录、`template_tags_path`、`template_html_dir` 均真实存在且可读，且 `template_html_dir` 中存在对应模板 id 的 `<template_id>.html` 文件。任一条件不满足，返回 `ppt-maker` 补齐输入，不得猜测模板结构或直接生成 HTML。

### 阶段 2 前置校验（强制 gate）

主 agent 在调用 `create_subtask` 启动阶段 2 的 `template-outline` subagent **之前**，必须先运行一次前置校验脚本：

```bash
python scripts/preflight.py --deck <deck_dir>
```

看 `exit_code`：

- `0`：stdout 是一行 JSON `{"status":"ok", "deck_dir":..., "template_name":..., "template_tags_path":..., "template_html_dir":...}`。主 agent 直接把这些字段以字符串形式塞进阶段 2 的 subtask 输入包，**不得**自己再去读 `task_pack.json` 验证字段。
- 非 `0`：stdout 是一行 JSON `{"status":"fail","reason":"...","missing":[...]}`。主 agent 必须停止本 Skill，回到 `ppt-maker` 重新补齐（或让用户补齐）缺失项，不得继续 dispatch 任何 subtask。

本脚本已覆盖：`task_pack.json` / `info_pack.json` 存在性和 JSON 合法性、`ppt_mode=template`、`mode=ppt-template-mode`、`template_params` 三个字段完整、`template_tags_path` 是真实文件、`template_html_dir` 是真实目录且至少含一个 `*.html`。主 agent 不需要、也不允许用额外的 `test -f` / `read_file` 去重做这些检查。

## Subagent 调度统一规则

本 Skill 的每一个阶段都必须通过 `create_subtask` 启动对应 subagent。`create_subtask` 的独立参数及填写规范以工具自身定义为准；主 agent 必须通过 `relevant_files`、`criteria`、`addition` 等参数把完整输入包（包括阶段 stage md 地址、所有输入/输出文件绝对路径、页码、验收要求、返回格式约束）传给 subagent，不得依赖任何未传入工具参数的隐式上下文。

subagent 的最终返回消息**必须**包含四项，每项一行、**总字符数不得超过 200**：`完成状态` / `产物路径` / `自检结果` / `未解决项`。**严禁**添加过程说明、做了什么、读了什么、调了什么脚本，也**严禁**粘贴 HTML/JSON/文件正文/execute_code 输出——返回内容膨胀会直接导致主 agent context 爆炸。

所有阶段 subagent 进入工作前先完整读完对应 stage md（遇 `is_truncated=true` 用新的 `offset` 续读，直到文件结束标记）。主 agent 发起 `create_subtask` 后必须等待返回并验收产物，再进入下一阶段。

## 🚫 主 agent 禁止修复和二次验证

subtask 返回成功后，主 agent **禁止**以下行为：

- 禁止读取 subtask 产出的文件内容来"核实"质量（已在禁读清单中）。
- 禁止用 `execute_code` / `execute_shell_command` 编写修复脚本来修改 subtask 产出的文件。
- 禁止启动 `reflection` agent 来检查 subtask 产出的完整性。
- 禁止在主线程中重新生成或覆写任何 `htmls/page_xxx.html`。

产出质量由 subtask 内的自检步骤保证（stage 05 md 中的"输出自检"）。如果 subtask 返回的 `未解决项` 非空或状态为失败，主 agent 应**重新 dispatch 该 subtask**（而不是自己修），或在收口通知中标记该页失败。

## 阶段 → subagent 映射

- **阶段 1**：主 agent 建立执行上下文，承接 `task_pack.json`、`info_pack.json`、模板名称、`tag_gen_results.json` 绝对地址和模板 HTML 目录绝对地址。阶段 1 的收口动作即为本 SKILL.md 上文"阶段 2 前置校验（强制 gate）"一节描述的 `scripts/preflight.py --deck <deck_dir>` 校验；该校验既是阶段 1 的收口，也是阶段 2 的准入 gate（同一次调用、同一个 `exit_code=0` 判定），两处描述指同一步，不需要跑两次。
- **阶段 1.5（可编辑大纲）**：前置校验通过后，主 agent 运行 `python scripts/outline_md_generate.py --deck <deck_dir>` 和 `python scripts/outline_md_preflight.py --deck <deck_dir>`，随后触发用户确认。后端写回用户编辑结果后，必须重新读取并校验最新文件。
- **阶段 2 + 3**：由同一个 `template-outline` subagent 连续执行，输入包必须包含确认后的最新 `outline.md`、`task_pack.json`、`info_pack.json`、`tag_gen_results.json` 绝对地址、模板 HTML 目录绝对地址、deck 工作目录、`template_map.json` 输出地址、`outline.json` 输出地址和两份 stage md 地址。阶段 2 必须先重新读取最新 `outline.md`，并按最新页数、页序、页型、页面主题、要点层级和要点数量全量重做模板编排；不得复用修改前的页码或模板映射。无可承载模板时显式失败，不得合并、拆分或改名用户要点。阶段 3 再按同一份 Markdown 只扩写 JSON 字段。完成后运行 `outline_md_preflight.py --against-json`，严格一致才进入阶段 4。
- **阶段 4**：由 `asset-planning` subagent 执行。stage md：`stages/04-asset-planning.md`。输入包：`outline.json`、`task_pack.json`、deck 工作目录、`asset_map.json` 输出地址、`prompts/background_image_prompt.md`、`prompts/illustration_image_prompt.md`、`prompts/image_filter_check_prompt.md`。子任务用 `image_search(download=false)` 取候选 URL 后，由 `scripts/image_pipeline.py` **并发（默认 16，每槽 top5）下载 + 代码级硬检查 + 调 `image_filter` API 做视觉检查**，不再由模型逐张看图。输出：`asset_map.json`。
- **阶段 5 前置**：`asset_map.json` 验收通过后、分发 html-generation subtask 之前，主 agent 必须从此文件所在目录执行：

  ```bash
  python scripts/slice_stage5_inputs.py --deck <deck_dir>
  ```

  本脚本在 deck 工作目录上原地切片，为每一页生成 `htmls/page_xxx.input.json`；切片内含当前页的 `template_map_page`、`outline_page`、`asset_map_page`、参考模板 HTML 的绝对路径 `template_html_path`、输出 HTML 的绝对路径 `output_html_path`，是 stage5 subagent 的**唯一**结构化输入。脚本正常退出时 stdout 返回一行 JSON `{"deck_dir":..., "page_count":..., "pages":[...]}`；主 agent 读取该 JSON 确认 `page_count == len(outline.json.pages)` 即可分发阶段 5，不得再自己读 `template_map.json` / `outline.json` / `asset_map.json`。脚本失败时停止流程。

- **阶段 5**：由**一个** `html-generation` subagent 执行——**不再每页一个 subtask**，而是这一个 subtask 跑批量脚本 `scripts/html_page_generate_batch.py`，脚本内部**并发（默认 4）**对所有页组装 prompt（含整份参考模板 HTML + 照模板生成约束）调 `html_page_generate` API 出 HTML、落盘、lint、失败重出一次。stage md：`stages/05-template-html-generation.md`。输入包：deck 工作目录、prompt 模板 `prompts/html_gen_template.md` 路径、stage md 绝对地址。各页切片 `htmls/page_xxx.input.json`（含 template_map/outline/asset_map 片段、`template_html_path`、`output_html_path`）已由前置脚本产出，subagent 只跑批量脚本、读其汇总 JSON（`ok_pages`/`failed_pages`），**不得**再读全量文件、**不得**自己手写 HTML 或自调 API。输出：所有页 `htmls/page_xxx.html`。主 agent 只做前置切片、启动这一个 subtask 和收口检查。`page_xxx` 统一用三位页码编号，例如 `page_001`。



## 完成通知


所有阶段 5 `html-generation` subtask 都完成并中止后，主 agent 必须检查 `htmls/` 目录中 HTML 是否齐全（文件存在且 size > 0），统计失败页列表，然后在最后一条 assistant 消息中输出：

```
<ppt_task_finished>
  <deck_dir>deck 工作目录绝对路径</deck_dir>
  <status>success ｜ partial_success ｜ error</status>
  <failed_pages>1, 3</failed_pages>
  <reason>简短描述error的原因</reason>
</ppt_task_finished>
```

这是**唯一合法的最终消息格式**。主 agent 完成目录检查并得出最终状态后，最后一条 assistant 消息**必须且只能**输出一个完整的 `<ppt_task_finished>...</ppt_task_finished>` 代码块：

- 前后**不得**添加任何自然语言说明、总结、交付提示或建议
- **不得**再输出 `file_action`、其他 XML/HTML 标签、JSON、Markdown 列表或额外空行说明
- **不得**在输出该标记后再发起任何 tool call、subtask 或新的 HTML 生成动作
- 一旦输出该标记，本次任务立即停止

`status` 规则：

- 全部页成功 → `"success"`，**无** `failed_pages` 字段
- 部分失败 → `"partial_success"`，带 `failed_pages`（失败的页码，用逗号分隔）
- 全部失败 / 切片脚本已崩 → `"error"`，带 `"reason"` 字段（字符串，简要描述）；可省略 `failed_pages`


输出此结构化标记后本次任务停止。

## 输出

- `outline.md`、`template_map.json`、`outline.json`、`asset_map.json`
- `htmls/page_xxx.input.json`（每页一份，由 `slice_stage5_inputs.py` 产出）
- `htmls/page_xxx.html`（每页一份，阶段 5 最终产物）

## 高层约束

- `tag_gen_results.json` 决定编排，模板 HTML 目录服务页面生成，两者分开处理。
- `template_map.json` 必须先于 `outline.json`。
- 图片相关内容被改动后，资产规划必须重新执行。
- 模板若带可切换主题：`<head>` 里引入的主题样式链接（`../themes/*.css` 等）与正文里 `var(--xxx)` 主题色变量在生成时必须原样保留、不硬编码、不被其他配置覆盖；`slice_stage5_inputs.py` 会把模板的 `themes/` 目录随静态资源一并带入 deck 工作目录，使保留的 `<link>` 能正确解析。
- 模板模式不执行 review/rewrite；阶段 5 生成的 `htmls/page_xxx.html` 即为最终产物。
- 最终输出 HTML，不输出单页图片或最终 JSON。

## 依赖

- 输入：`ppt-maker`、`task_pack.json`、`info_pack.json`、`tag_gen_results.json` 绝对地址、模板 HTML 目录绝对地址
- 工具：`create_subtask`、`execute_shell_command`、`image_search`、`image_generate`
- 外部 API（经 `scripts/_tool_call.py` 调 `/api/agent/tool_call`，端点默认 code-stage、内置 host pin、env 可覆盖）：`html_page_generate`（阶段 1.5 的过渡文本通道及阶段 5 出 HTML）、`image_filter`（阶段 4 图片视觉检查）。后端注册专用 `outline_md_generate` 后，可通过 `PPT_OUTLINE_TOOL_NAME=outline_md_generate` 切换。
- 脚本：`scripts/outline_md_generate.py`、`scripts/outline_md_preflight.py`、`scripts/preflight.py`、`scripts/enrich_template_map.py`、阶段 4 拼 spec `scripts/build_pipeline_spec.py`、图片下载检查 `scripts/image_pipeline.py`（并发 16，每槽 top5）、`scripts/slice_stage5_inputs.py`、阶段 5 批量出图 `scripts/html_page_generate_batch.py`（并发 4，内部跑 lint）、`scripts/lint_page_html.py`（被 `html_page_generate_batch.py` 调用）
