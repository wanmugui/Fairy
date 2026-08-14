---
name: ppt-creative-mode
description: 当 PPT 任务确认使用创意模式（ppt_mode=creative），且 task_pack.json / info_pack.json / deck 工作目录均就绪时使用。负责风格生成、大纲生成、逐页并发调用 creative_page_render API 出图的全链路。只要 ppt-maker 分发到创意模式，都必须进入本 Skill。
---

# PPT 创意模式 Skill

## 🚫 最高优先级红线：主 agent 禁止直接产出阶段文件

**本节为本 Skill 最高优先级规则，进入后先读完。**

从阶段 2 开始，以下文件**必须**由对应阶段的 subagent 通过 `create_subtask` 启动后产出；主 agent 用 `execute_code` / `execute_shell_command` / 任何写文件能力**直接写入**这些文件，即视为违反本 Skill：

- `style_spec.md`（阶段 2，由 `style-outline` subagent 产出）
- `outline.json`（阶段 3，由同一 `style-outline` subagent 产出）
- `pages/page_xxx.png`（阶段 4，每页由一个独立 `page-render` subagent 产出）

**自检触发点**：若发现自己将在 subtask 外直接写入以上文件，立即停止，改用 `create_subtask` 启动对应阶段的 subagent。

主 agent 合法动作仅限：读 `task_pack.json` / `info_pack.json`、用 `test -f/-d` 做前置检查、调 `scripts/outline_md_generate.py` / `scripts/outline_md_preflight.py`、调 `scripts/slice_page_inputs.py` 做纯数据切片、调 `create_subtask`、等待返回验收产物、推进下一阶段、汇总失败页并输出收口通知。

## 用户可见输出守则

本节只约束**面向用户的自然语言阶段性回复**（assistant 文本中用户能看到的句子）。**不约束** `create_subtask` 的工具参数、`<ppt_task_finished>` 等协议 XML、`<cite>` 引用标签——这些该出现的地方按规范照常出现。

**禁止**在用户可见文本里出现：

- 任何内部字段名、文件名、工具名、脚本名、模式枚举值（包括但不限于 `task_pack` / `info_pack` / `style_spec` / `outline` / `pages` / `page_xxx` / `subagent` / `subtask` / `create_subtask` / `creative_page_render` / `execute_code` / `*.json` / `*.md` / `*.png` / `*.py` 等内部术语和文件名）。
- 工作目录绝对路径、UUID（如 `pptid_xxxxxxxx-...`）、`deck_dir` 等路径变量名。
- 中英文术语混杂。

**判定CoT只能用中文。**复述阶段进展时用纯中文自然语言（例如"风格定好了，开始排大纲"、"页面在生成"、"完成"），禁止携带任何英文字段名 / 枚举值 / 文件名 / 阶段号。派发子任务时**不要复述 payload**：不要把交付路径、`<criteria>` / `<goal>` / `<todo>` 段内容、`<result>` 返回要求念给用户。

## Skill 目标与边界

在 `task_pack.json`、`info_pack.json` 和 deck 工作目录就绪后，执行创意模式下的 PPT 页面图片生成链路。最终产物是一组 `pages/page_xxx.png`，不输出 HTML，不输出"最终 json"。

本 `SKILL.md` 负责模式边界、阶段顺序、subagent 调度；字段定义、单阶段处理细节、页面渲染硬性要求放在 `stages/*.md`。

## 输入与前置条件

输入：

- `task_pack.json`、`info_pack.json`
- 用户图片（可选）

进入本 Skill 前必须同时满足：`task_pack.json`、`info_pack.json`、deck 工作目录均真实存在且可读，且 `task_pack.json.ppt_mode` 为 `creative`（或 `ppt-maker` 已明确分发到 `ppt-creative-mode`）。任一条件不满足，返回 `ppt-maker` 补齐输入。

## Subagent 调度统一规则

本 Skill 的每一个阶段都必须通过 `create_subtask` 启动对应 subagent。`create_subtask` 的独立参数及填写规范以工具自身定义为准；主 agent 必须通过 `relevant_files`、`criteria`、`addition` 等参数把完整输入包（包括阶段 stage md 地址、所有输入/输出文件绝对路径、页码、验收要求、返回格式约束）传给 subagent，不得依赖任何未传入工具参数的隐式上下文。

subagent 的最终返回消息**必须且只能**包含四项，每项一行、**总字符数不得超过 200**：`完成状态` / `产物路径` / `自检结果` / `未解决项`。**严禁**添加过程说明、做了什么、读了什么、调了什么脚本，也**严禁**粘贴 HTML/JSON/文件正文/execute_code 输出——返回内容膨胀会直接导致主 agent context 爆炸。

所有阶段 subagent 进入工作前先完整读完对应 stage md。主 agent 发起 `create_subtask` 后必须等待返回并验收产物，再进入下一阶段。

## 🚫 主 agent 禁止修复和二次验证

subtask 返回成功后，主 agent **禁止**以下行为：

- 禁止读取 subtask 产出的文件内容来"核实"质量（尤其不得用 `image_vqa` 核对 PNG 图片内容）
- 禁止用 `execute_code` / `execute_shell_command` 编写修复脚本来修改 subtask 产出的文件
- 禁止启动 `reflection` agent 来检查 subtask 产出的完整性
- 禁止在主线程中重新生成或覆写任何 `pages/page_xxx.png`

产出质量由 subtask 内的自检步骤保证。如果 subtask 返回的未解决项非空或状态为失败，主 agent 应**重新 dispatch 该 subtask**（而不是自己修），或在收口通知中标记该页失败。

## 阶段 → subagent 映射

- **阶段 1**：主 agent 建立执行上下文，承接 `task_pack.json`、`info_pack.json`、用户图片和 deck 工作目录。
- **阶段 1.5（可编辑大纲）**：主 agent 运行 `python scripts/outline_md_generate.py --deck <deck_dir>`，再运行 `python scripts/outline_md_preflight.py --deck <deck_dir>`；通过后触发用户确认。后端写回修改后，继续前重新校验最新文件。
- **阶段 2 + 3**：由同一个 `style-outline` subagent 连续执行；输入包必须包含确认后的最新 `outline.md`。阶段 2 必须先重新读取它，并让创意风格覆盖其中实际存在的页型、页面主题和信息结构；不得只依据初始页数或修改前的大纲设计。阶段 3 再按同一份 Markdown 扩写正文和创意模式字段。
  - stage md：`stages/02-style-spec.md` → `stages/03-outline-generation.md`
  - 输入包：`task_pack.json`、`info_pack.json`、确认后的 `outline.md` 绝对地址、`user_images` 列表（可空）、`deck_dir`、`style_spec.md` 输出地址、`outline.json` 输出地址、两份 prompt md 地址（`prompts/query2style.md`、`prompts/image2style.md`）、两份 stage md 地址
  - 输出：`style_spec.md`（阶段 2 写完）和 `outline.json`（阶段 3 写完，随后 subagent 中止）
- **阶段 3 闸门**：运行 `python scripts/outline_md_preflight.py --deck <deck_dir> --against-json`；页数、页序、页型、标题和要点层级严格一致后才能切片。
- **阶段 4 前置**：`outline.json` 验收通过后、分发 `page-render` subtask 之前，主 agent 必须从此文件所在目录执行：

  ```bash
  python scripts/slice_page_inputs.py --deck <deck_dir>
  ```

  本脚本为每一页生成 `pages/page_xxx.input.json`；切片内含 `ppt_id` / `ppt_title` / `page_style_md`（style_spec.md 全文内联） / `page_outline`（已按 API 映射），是阶段 4 subagent 的**唯一**结构化输入。脚本正常退出时 stdout 返回一行 JSON `{"deck_dir":..., "page_count":..., "pages":[...]}`；主 agent 读取该 JSON 确认 `page_count` 即可分发阶段 4。脚本失败时停止流程。
- **阶段 4**：由 `page-render` subagent 执行，**每一页必须启动一个独立 subtask**，subtask 数量必须等于前置脚本返回的 `page_count`。
  - stage md：`stages/04-page-render.md`
  - 输入包：`pages/page_xxx.input.json` 绝对地址、`page_number`、`output_png_path`、stage md 绝对地址
  - 输出：`pages/page_xxx.png`
  - 主 agent 只做任务拆分、启动 subtask 和收口检查。

`page_xxx` 统一用三位页码编号，例如 `page_001`。

## 渲染脚本说明

`page-render` subagent 只允许通过 shell 执行以下脚本，**禁止**用 `execute_code` 自写 HTTP 请求：

```bash
python scripts/creative_page_render.py \
  --input <input_json_path>
```


## 完成通知

所有 `page-render` subtask 都完成并中止后，主 agent 必须检查 `pages/` 目录中 PNG 是否齐全（文件存在且 size > 0），统计失败页列表，然后在最后一条 assistant 消息中输出：

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
- **不得**在输出该标记后再发起任何 tool call、subtask 或新的页面渲染
- 一旦输出该标记，本次任务立即停止

`status` 规则：

- 全部页成功 → `"success"`，**无** `failed_pages` 字段
- 部分失败 → `"partial_success"`，带 `failed_pages`（失败的页码，用逗号分隔）
- 全部失败 / 切片脚本已崩 → `"error"`，带 `"reason"` 字段（字符串，简要描述）；可省略 `failed_pages`

输出此结构化标记后本次任务停止。**不得**再对任何页面发起新的渲染 subtask。

## 输出

- `style_spec.md`
- `outline.md`、`outline.json`
- `pages/page_xxx.input.json`（每页一份，阶段 4 前置切片产物）
- `pages/page_xxx.png`（每页一份，阶段 4 最终产物）

## 高层约束

- 必须先生成 `style_spec.md`（markdown），再生成 `outline.json`（JSON）
- `style_spec.md` 是 markdown，不是 JSON；`outline.json` 的 pages[] 字段结构必须对齐 `creative_page_render` API 消费格式
- 用户图片只能作为风格参考，不能被当作模板
- 最终输出一组 PNG，不输出 HTML，不输出最终 JSON
- 不做单页迭代、内容解析、资产规划

## 依赖

- `ppt-maker`、`task_pack.json`、`info_pack.json`、用户图片（可选）
- 工具：`create_subtask`、`execute_shell_command`、`image_vqa`（可选，用于阶段 2 的 `image2style` 读参考图）
- 脚本：`scripts/outline_md_generate.py`、`scripts/outline_md_preflight.py`、`scripts/slice_page_inputs.py`、`scripts/creative_page_render.py`
- prompt：`prompts/outline_md.md`、`prompts/query2style.md`、`prompts/image2style.md`
- 外部 API：`creative_page_render`（具体端点由 `scripts/creative_page_render.py` 内部管理，可通过 `CREATIVE_RENDER_API_URL` 环境变量覆盖）
