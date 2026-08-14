---
name: ppt-no-template-mode
description: 当 PPT 任务已确认使用无模板模式（ppt_mode=no-template），且 task_pack.json、info_pack.json 和 deck 工作目录均就绪时使用。负责设计信息生成、大纲生成、资产规划、逐页 HTML 生成和 review/rewrite 全链路。只要 ppt-maker 分发到无模板模式，都必须进入本 Skill。
---

# PPT 无模板模式 Skill

## 🚫 最高优先级红线：主 agent 禁止直接产出阶段文件

**本节为本 Skill 最高优先级规则，进入后先读完。**

**无论任务看起来多简单、页数多少、是否以图为主，本模式都无一例外按阶段派 subtask——任务难易、页数多少与"是否走子任务"完全无关。** 从阶段 2 开始，以下阶段产物**必须**由对应阶段的子任务（subagent）通过子任务委派工具 `create_subtask` 启动后产出；主 agent 用 `execute_code` / `execute_shell_command` / `write_file` / 任何写文件能力**直接写入**这些文件，即视为违反本 Skill：

- 设计风格说明 `style_spec.json`（阶段 2，由"风格-大纲"子任务（`style-outline` subagent）产出）
- 大纲 `outline.json`（阶段 3，由同一个"风格-大纲"子任务产出）
- 资产规划 `asset_map.json`（阶段 4，由"资产规划"子任务（`asset-planning` subagent）产出）
- 页面 HTML —— **任何路径下的页面 `.html` / `.png`**，不只 `htmls/page_xxx.html`。主 agent 严禁自己写任何 deck 页面文件，也严禁把页面落到 `pages/`、`output/` 等**自创目录**绕开本红线（标准路径只有 `{deck_dir}/htmls/page_xxx.html`，且只能由阶段 5 生成子任务产出）。同样**禁止**主 agent 产出 spec 之外的任何文件（如 index 导航页、预览页、汇总页）。
- 页面复核报告 `htmls/page_xxx.review.md`（阶段 6，由**一个**"页面复核"子任务（`page-review-polish` subagent）跑批量脚本对所有页产出，每页一份 markdown，为该阶段评估产物）

**🚩 红旗念头自检（出现任一立即停手，它们就是"即将违规"的前兆）**：

- "这个任务很简单 / 才 6 页 / 以图为主，我直接写就行" —— **违规前兆**。
- "主线程直接编排 + 生成 HTML 更快，无需走子任务大循环" —— **违规前兆**。
- "我先把页面写到 `pages/`（或别的目录）" —— **违规前兆**。
- "顺手加个 index 导航页 / 预览页" —— **违规前兆**。
- "用自然语言收尾（'PPT 做好了🎉'）+ 贴 cite 链接" 代替 `<ppt_task_finished>` —— **违规前兆**。

冒出以上任一念头，**立即停止，回到 `create_subtask` 走正规阶段**。

**自检触发点**：若发现自己将在子任务外直接写入页面 / 风格 / 大纲 / 资产 / 复核任一产物（无论路径、无论用什么工具），立即停止，改用 `create_subtask` 启动对应阶段的子任务。

主 agent 合法动作仅限：读任务约束文件 `task_pack.json` / 信息素材文件 `info_pack.json`、用 `test -f/-d` 做前置检查、调用 `scripts/outline_md_generate.py` 生成可编辑大纲、调用 `scripts/outline_md_preflight.py` 校验 Markdown 及 md↔json 一致性、调用 `scripts/outline_preflight.py` 校验详细大纲、调切片脚本 `scripts/slice_stage5_inputs.py` 做纯数据切片、调 `create_subtask`（通过工具参数传入完整输入包）、等待返回验收产物、推进下一阶段、汇总失败并输出收口通知。**禁止以"节省时间 / 避免上下文膨胀 / 任务简单 / 页数少 / 以图为主 / 我直接写更快 / 无需子任务"等任何理由绕过**——这些都不是合法理由，无模板模式没有"简单到可以跳过子任务"的情形。

## Skill 目标与边界

在任务约束文件 `task_pack.json`、信息素材文件 `info_pack.json` 和工作目录 `deck` 就绪后，执行无模板约束下的 PPT HTML 生成链路。本 `SKILL.md` 负责模式边界、阶段顺序、子任务调度；字段定义、单阶段处理细节、页面生成硬性要求放在阶段说明 `stages/*.md`。最终产物是阶段 5 生成、阶段 7 原地修改的页面 HTML `htmls/page_xxx.html`，不是单页图片或"最终 json"。

无模板模式不读取模板配置、模板标签或任何模板资源目录。用户图片只作为风格参考或资产线索，不等同于模板。

## 输入与前置条件

输入：

- 任务约束文件、信息素材文件
- 用户图片（可选，仅作为风格参考或资产线索）

进入本 Skill 前必须同时满足：任务约束文件、信息素材文件、工作目录均真实存在且可读，且任务约束文件中的制作模式 `ppt_mode` 为 `no-template`、`mode` 为 `ppt-no-template-mode`。任一条件不满足，返回 `ppt-maker` 补齐输入，不得直接生成 HTML。

## 输出守则

**所有阶段性回复都必须先满足本节**, 本节只约束写在 assistant 文本里、用户能直接看到的句子；**不约束**工具参数、`<cite>` 标签、`<ppt_task_finished>` 协议 XML、子任务 payload 中的 `<goal>` / `<todo>` 等内部结构——这些该出现的地方按工具规范照常出现。

**禁止**在用户可见文本里出现：

- 任何内部字段名、文件名、工具名、脚本名、模式枚举值（包括但不限于：任务约束/信息素材文件本身的英文名、调研/搜索/抓取/委派/确认工具的英文名、各类策略枚举值、模板字段名、子任务 / 阶段 / stage md 等内部术语）。
- 工作目录绝对路径、UUID（如 `pptid_xxxxxxxx-...`）、`deck_dir` 等路径变量名。
- 中英文术语混杂。

**判定CoT只能用中文。**复述阶段进展或判定结论时（例如"风格定好了，开始排大纲"、"页面在出图"、"这页要重画"），必须用纯中文自然语言，禁止携带任何英文字段名 / 枚举值 / 工具名 / 文件名（如 `style_spec` / `outline` / `asset_map` / `subtask` / `review` / `rewrite` 等）。派发子任务时**不要复述 payload**：不要把交付路径、`<criteria>` / `<goal>` / `<todo>` 段内容、`<result>` 返回要求念给用户。

## 子任务调度统一规则

本 Skill 的每一个阶段都必须通过子任务委派工具 `create_subtask` 启动对应子任务。主 agent 必须通过 `relevant_files`、`criteria`、`addition` 等参数把完整输入包（包括阶段说明地址、所有输入/输出文件绝对路径、页码、验收要求、返回格式约束）传给子任务，不得依赖任何未传入工具参数的隐式上下文。

子任务的最终返回消息**必须且只能**包含四项，每项一行、**总字符数不得超过 200**：`完成状态` / `产物路径` / `自检结果` / `未解决项`。**严禁**添加过程说明、做了什么、读了什么、调了什么脚本，也**严禁**粘贴 HTML/JSON/文件正文/execute_code 输出——返回内容膨胀会直接导致主 agent context 爆炸。

所有阶段子任务进入工作前先按完整读完对应阶段说明 `stages/*.md`（遇 `is_truncated=true` 用新的 `offset` 续读，直到文件结束标记）。主 agent 发起 `create_subtask` 后必须等待返回并验收产物，再进入下一阶段。

## 🚫 主 agent 禁止修复和二次验证

子任务返回成功后，主 agent **禁止**以下行为：

- 禁止读取子任务产出的文件内容来"核实"质量。
- 禁止用 `execute_code` / `execute_shell_command` 编写修复脚本来修改子任务产出的文件。
- 禁止启动反思智能体（`reflection` agent）来检查子任务产出的完整性。
- 禁止在主线程中重新生成或覆写任何页面 HTML。

产出质量由子任务内的自检步骤保证（阶段 5 说明中的"输出自检"）。如果子任务返回的 `未解决项` 非空或状态为失败，主 agent 应**重新派发该子任务**（而不是自己修），或在收口通知中标记该页失败。
## 阶段 → 子任务映射

- **阶段 1**：主 agent 建立执行上下文，承接任务约束文件、信息素材文件、用户图片和工作目录。
- **阶段 1.5（可编辑大纲）**：主 agent 运行 `python scripts/outline_md_generate.py --deck <deck_dir>` 生成 `outline.md`，再运行 `python scripts/outline_md_preflight.py --deck <deck_dir>`。通过后立即触发用户确认；后端会把编辑结果覆盖写回同一文件。继续前必须再次校验最新落盘版本。禁止缓存或恢复初次生成版本。
- **阶段 2 + 3**：由同一个"风格-大纲"子任务连续执行。阶段说明：`stages/02-style-spec.md` → `stages/03-outline-generation.md`。输入包必须包含确认后的最新 `outline.md` 绝对路径、任务约束文件、信息素材文件、**风格库 `references/design-styles.md` 绝对地址**（阶段 2 选型必读）、用户图片本地地址列表（如有）、工作目录、设计风格说明 `style_spec.json` 输出地址、大纲 `outline.json` 输出地址和两份阶段说明地址。阶段 2 必须先重新读取最新 Markdown，让线上风格库选型和完整视觉体系覆盖实际页型、内容结构与信息密度；阶段 3 再按同一份 Markdown 骨架扩写 `outline.json`，不得修改页数、页序、页型、页标题或要点层级。输出：设计风格说明（阶段 2 写完）和详细大纲（阶段 3 写完，随后子任务中止）。
- **阶段 3 双闸门（主 agent 强制执行）**："风格-大纲"子任务返回后，主 agent 必须依次运行 `python scripts/outline_preflight.py --deck <deck_dir>` 和 `python scripts/outline_md_preflight.py --deck <deck_dir> --against-json`。第一条校验详细 JSON schema、字段和资产槽约束，第二条校验用户 Markdown 骨架一致性；两条命令都必须 `exit 0` 才能进入阶段 4。任一失败时重新派发同一子任务整体重写 `outline.json`，不得跳过、调换顺序或只跑其中一个。
- **阶段 4**：由"资产规划"子任务执行。阶段说明：`stages/04-asset-planning.md`。输入包：大纲、设计风格说明、任务约束文件、工作目录、资产规划 `asset_map.json` 输出地址、`prompts/background_image_prompt.md`、`prompts/illustration_image_prompt.md`、`prompts/image_filter_check_prompt.md`。子任务用 `image_search(download=false)` 取候选 URL 后，由 `scripts/image_pipeline.py` **并发（默认 16，每槽 top5）下载 + 代码级硬检查 + 调 `image_filter` API 做视觉检查**，不再由模型逐张看图。输出：资产规划。
- **阶段 5 前置**：资产规划验收通过后、分发"页面生成"子任务之前，主 agent 必须从此文件所在目录执行：

  ```bash
  python scripts/slice_stage5_inputs.py --deck <deck_dir>
  ```

  本脚本为每一页生成页面输入切片 `htmls/page_xxx.input.json`；切片内含 `style_spec`（完整）、`outline_page`（当前页）、`asset_map_page`（当前页）、`output_html_path`，是阶段 5 子任务的**唯一**结构化输入。脚本正常退出时 stdout 返回一行 JSON `{"deck_dir":..., "page_count":..., "pages":[...]}`；主 agent 读取该 JSON 确认 `page_count` 即可分发阶段 5，不得再自己读设计风格说明 / 大纲 / 资产规划的全量文件。脚本失败时停止流程。

- **阶段 5**：由**一个**"页面生成"子任务（`html-generation` subagent）执行——**不再每页一个子任务**，而是这一个子任务跑批量脚本 `scripts/html_page_generate_batch.py`，脚本内部**并发（默认 4）**对所有页组装 prompt 调 `html_page_generate` API 出 HTML、落盘、lint、失败重出一次。阶段说明：`stages/05-html-generation.md`。输入包：工作目录、prompt 模板 `prompts/html_gen_no_template.md` 路径、阶段说明绝对地址。页面输入切片已由前置脚本产出，子任务只跑批量脚本、读其汇总 JSON（`ok_pages`/`failed_pages`），**不得**再读全量文件、**不得**自己手写 HTML 或自调 API。输出：所有页 HTML（路径来自各页切片 `output_html_path`）。主 agent 只做前置切片、启动这一个子任务和收口检查。
- **阶段 6 + 7（review/rewrite）**：由**一个**"页面复核"子任务执行——跑批量脚本 `scripts/html_page_review_batch.py`，脚本内部**并发（默认 4）**对所有页：读 HTML → 把 `../assets/` 本地图内联成 `data:` URL → `html_to_png`（**stateless 模式**，传 HTML 字符串、返回 `png_base64`）→ `html_page_review` 视觉评审 → 落 `page_xxx.review.md`；对 `needs_rewrite` 的页把「当前 HTML + 评审 issues」塞进 `html_page_generate` 重出修正版 → 覆盖 → 重跑 lint。阶段说明：`stages/06-html-review.md`（rewrite 已并入 06，`stages/07-html-rewrite.md` 仅备查）。输入包：deck 工作目录、评审 prompt `prompts/html_review_prompt.md` 路径、生成 prompt `prompts/html_gen_no_template.md` 路径、阶段说明地址。命令：

  ```bash
  python scripts/html_page_review_batch.py --deck <deck_dir> --review-prompt prompts/html_review_prompt.md --gen-prompt prompts/html_gen_no_template.md --concurrency 4
  ```

  子任务只跑批量脚本、读其汇总 JSON（`reviewed`/`needs_rewrite`/`rewritten`/`rewrite_failed`），**不得**自己截图、自己调 API、自己手改 HTML。输出：每页 `page_xxx.review.md` 和（按需）原地覆盖的 `page_xxx.html`。**禁止**写 `review.json` / `tmp_shot_*.py` / `*_render.html` / `*.bak` 等副产物。

`page_xxx` 统一用三位页码编号，例如 `page_001`。


## 完成通知

**收口前自检（防止 freestyle 蒙混过关）**：到这一步前，页面**必须**是阶段 5 生成子任务写进 `{deck_dir}/htmls/page_xxx.html` 的。若你发现本次出现过以下任一情况，说明你**违规跳过了子任务流程**，必须先把这些违规产物删掉、回到 `create_subtask` 按阶段重走，**不得**直接收口：页面被写到 `pages/` 或其它非 `htmls/` 目录、存在 index/预览/汇总页、页面是主 agent 自己 `write_file`/`execute_code` 写的而非生成子任务产出的。

阶段 6+7 复核子任务完成并中止后，主 agent 必须检查 `htmls/` 目录中 HTML 是否齐全（文件存在且 size > 0），统计失败页列表，然后在最后一条 assistant 消息中输出：

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
- **不得**在输出该标记后再发起任何 tool call、子任务、复核或改写
- 一旦输出该标记，本次任务立即停止

`status` 规则：

- 全部页成功 → `"success"`，**无** `failed_pages` 字段
- 部分失败 → `"partial_success"`，带 `failed_pages`（失败的页码，用逗号分隔）
- 全部失败 / 切片脚本已崩 → `"error"`，带 `"reason"` 字段（字符串，简要描述）；可省略 `failed_pages`

输出此结构化标记后本次任务停止。**不得**对任何页面再发起新的复核或改写子任务。

## 输出

- 可编辑大纲 `outline.md`、设计风格说明 `style_spec.json`、扩写大纲 `outline.json`、资产规划 `asset_map.json`
- 页面输入切片 `htmls/page_xxx.input.json`（每页一份，阶段 5 切片产出）
- 页面 HTML `htmls/page_xxx.html`（每页一份，阶段 5 最终产物，阶段 7 按需原地覆盖）
- 页面复核报告 `htmls/page_xxx.review.md`（每页一份，阶段 6 评估产物，markdown 格式）

## 高层约束

- 必须先生成设计风格说明，再生成大纲。
- 用户图片只能作为风格参考或资产线索，不能被当作模板。
- 图片相关内容被改动后，资产规划必须重新执行。
- 页面复核发现溢出/遮挡/风格偏差/页脚安全区问题时**才**进入改写；复核判定无问题则跳过改写。复核和改写每页各只跑一次、不循环。
- 最终输出 HTML，不输出单页图片或最终 JSON。

## 依赖

- `ppt-maker`、任务约束文件、信息素材文件、用户图片（可选）
- 工具：网页搜索 `web_search`、图片搜索 `image_search`、网页抓取 `fetch_url`、图片生成 `image_generate`、子任务委派 `create_subtask`、代码执行 `execute_shell_command`
- 外部 API（经 `scripts/_tool_call.py` 调 `/api/agent/tool_call`，端点默认 code-stage、内置 host pin、env 可覆盖）：`html_page_generate`（阶段 1.5 的过渡文本通道、阶段 5 出 HTML、阶段 7 重出修正版）、`image_filter`（阶段 4 图片视觉检查）、`html_to_png`（阶段 6 出页面截图）、`html_page_review`（阶段 6 截图视觉评审）。后端注册专用 `outline_md_generate` 后，可通过 `PPT_OUTLINE_TOOL_NAME=outline_md_generate` 切换。
- **画布**：默认 1280×720（前端契约）。仅当用户/任务约束明确要求 1600×900 对比时，阶段 5 生成命令与阶段 6+7 复核命令**统一**加 `--canvas 1600x900`；否则一律不带此参数。
- 脚本：`scripts/outline_md_generate.py`、`scripts/outline_md_preflight.py`、阶段 4 拼 spec `scripts/build_pipeline_spec.py`（从 outline + 候选映射自动组装 image_pipeline 的 spec，免 LLM 手写）、图片下载检查 `scripts/image_pipeline.py`（并发 16，每槽 top5）、阶段 5 切片 `scripts/slice_stage5_inputs.py`、阶段 5 批量出图 `scripts/html_page_generate_batch.py`（并发 4，内部跑 lint）、阶段 6+7 批量复核 `scripts/html_page_review_batch.py`（并发 4，html_to_png + review + rewrite + lint）、页面结构自检 `scripts/lint_pages.py`（被生成/复核脚本调用）
