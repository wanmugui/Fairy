# 阶段 2：模板编排

## outline.md 输入契约（最高优先级）

必须以用户确认后的最新 `outline.md` 规划完整页面集合。`template_map.json` 的页数、页序、页面角色和页面主题必须跟随该文件；用户调整页数或结构后必须全量重新选模板，不得沿用旧映射。模板容量不足时选择其他模板；确无可用模板则显式返回未解决项并停止，不得反向修改、吞掉或合并用户大纲。

本文件是 template-outline subagent 的阶段 2 执行说明。subagent 收到主 agent 传入的输入包后，必须先读取本文件并生成 `template_map.json`，再进入阶段 3。

## 输入

- `task_pack.json`
- 用户确认或编辑后的最新 `outline.md`
- `tag_gen_results.json` 的绝对文件地址

阶段 2 不读取 `info_pack.json`，不改写 `task_pack.json`。`task_pack.json` 保留 `ppt-maker` 阶段的原始设计，提供任务目标、目标页数、使用场合、受众、演讲者身份、语言、约束、模板名称和可能已有的页面/章节意图。

生成前必须读取最新 `outline.md`，按其中的实际页数、页序、页型、页面主题、要点层级和要点数量逐页选择模板并规划槽位。用户调整后的 Markdown 结构优先于 `task_pack.json` 的初始页数和页面意图；`task_pack.json` 继续提供受众、场景、语言和其他任务约束。禁止复用修改前的页码、页面角色或模板选择。

阶段 2 只读取模板配置文件，不在本文件重复定义 `tag_gen_results.json` 的内部格式。模板配置文件格式由输入文件自身和系统注入说明决定。

## 输出

- `template_map.json`

输出必须是固定 JSON 对象。所有页面必须使用同一个固定 schema，不能每次生成不同结构。
必须写入输入包指定的 `template_map.json` 输出文件地址，不得只在聊天消息中返回。

## 字段定义

subagent 只需要生成以下 **slim** 字段。`template_page_type`、`layout_category`、`template_constraints`、`template_raw_constraints` 由 enrich 脚本从 `tag_gen_results.json` 自动填充，subagent **不要输出**这四个字段。

```python
class TemplateMap:
    schema_version: str
    deck_dir: str
    source_files: TemplateMapSources
    page_count: int
    pages: list["TemplateMapPage"]
    unresolved: list[str]


class TemplateMapSources:
    task_pack: str
    template_tags: str


class TemplateMapPage:
    page_id: str
    page_number: int
    page_role: str
    template_id: str
    selection_reason: str
    content_fit_hint: str
    visual_requirements: list[str]
    template_summary: str
    layout_description: str
    layout_slots: list["TemplateLayoutSlot"]
    unresolved: list[str]
    # 以下字段由 enrich 脚本自动填充，subagent 不要输出：
    # template_page_type, layout_category,
    # template_constraints, template_raw_constraints


class TemplateLayoutSlot:
    slot_id: str
    slot_role: str
    position_hint: str
    allowed_content_kinds: list[str]
    capacity_hint: str
```

字段含义：

- `page_id`：稳定页面标识，使用 `page_001`、`page_002` 这类三位页码形式。
- `page_number`：1 基页码。
- `page_role`：本页在任务结构中的角色，如 `title`、`content`、`catalog`、`transition`、`ending`。
- `template_id`：选中的模板编号，必须来自 `tag_gen_results.json` 中的模板 id。
- `selection_reason`：说明该页为什么选择这个模板，保持简短。
- `content_fit_hint`：说明本页适合承载的内容组织方式，例如流程、对比、指标、时间线、图文说明、表格汇总、章节过渡。
- `visual_requirements`：模板天然需要的视觉元素类型，例如 image、chart、table、background_image、icon_cards；它不是新增图片任务。
- `template_summary`：对所选模板用途、布局或风格的短描述，可来自 `usage` 和 `layout.custom_description` 的压缩总结。
- `layout_description`：保留模板布局的原始语义描述，来自 `layout.category`、`layout.custom_description` 和必要的 `usage`，不要过度压缩。
- `layout_slots`：结构化槽位列表，描述 stage3 可以把内容块放到哪些区域。
- `slot_id`：槽位稳定标识，例如 `left_column`、`right_image`、`card_1`、`chart_area`、`whole_page`。
- `slot_role`：槽位用途，例如 `title`、`body_text`、`image`、`chart`、`table`、`summary`、`decorative`。
- `position_hint`：槽位在页面中的位置提示。
- `allowed_content_kinds`：槽位允许承载的内容类型，例如 `text`、`table`、`chart`、`picture`、`icon`。
- `capacity_hint`：槽位可承载的信息量提示，例如最多几个要点、适合长文本还是短标签。
- `unresolved`：模板选择阶段无法确认的问题。

## 处理要求

- 根据最新 `outline.md` 的实际页数生成完整页面映射，`template_map.json.pages` 的长度必须等于 Markdown 页面数。仅在 Markdown 不存在或结构校验失败时停止并报告，不得回退到 `task_pack.json` 猜测或恢复初始页数。
- 不得根据总页数自行增加、删除或改写封面、目录、过渡、总结、结尾等页面。逐页角色完全取最新 `outline.md`：Markdown 有哪一页就映射哪一页，没有的页面不得补建。
- 对 Markdown 中 `page_type=title` / `ending` / `catalog` / `transition` 的页面，必须从 `tag_gen_results.json` 中选择真实 `page_type` 匹配的模板；不得把其他类型模板套用后在 `template_summary` / `selection_reason` 中伪造页面类型。`template_map` 中声称的 `page_type` 必须与该 `template_id` 的真实类型一致。
- 只有最新 `outline.md` 已包含章节过渡页时，才为其选择 `page_type=transition` 的模板；不得依据 `task_pack.json` 的章节结构自行插页。
- 根据页面主要信息类型、内容密度、图文图表表格需求和模板 `usage` 选择最合适模板；`layout.category` 和 `layout.custom_description` 作为布局适配依据。
- 生成 `layout_slots` 时，必须从 `layout.category`、`layout.custom_description`、`image_num`、`chart_num`、`table_num`、`available_text_subpoint_range` 和 `usage` 提取结构化槽位。
- 如果无法精确拆分复杂自定义布局，至少输出一个 `whole_page` 槽位，并把原始布局语义放入 `layout_description` 和 `capacity_hint`。
- 如果页面需要图片，必须选择 `image_num` 大于 0 的模板。
- 如果模板 `image_num` 为 0 或描述明确不需要图片，不得为该页规划图片。
- 选择模板时要保持适度多样性，但不能为了多样性选择适配度低的模板。
- 每个模板的使用次数不得过度集中；除章节过渡页外，若单个模板使用次数明显超过总页数的 1/4 或 5 次，需要优先改选其他适配模板，无法改选时写入 `unresolved`。
- 不得因为模板容量不足删减内容，应改选更合适的模板或标记 `unresolved`。
- `template_map.json` 只负责页面到模板编号的映射和语义描述，不生成大纲正文、不规划具体图片搜索词、不写 HTML。
- **禁止输出 `template_page_type`、`layout_category`、`template_constraints`、`template_raw_constraints` 四个字段**——这些由 enrich 脚本自动从 `tag_gen_results.json` 填充。

## 视觉策略与模板选择

本阶段必须读取 `task_pack.visual_strategy`（`chart_dominant` / `image_dominant` / `mixed`）和 `task_pack.content_density_profile`，让模板选择倾向于带视觉槽的模板。**关键**：页面能否承载图表/图片完全由所选模板的 `image_num` / `chart_num` / `table_num` 决定，stage 3 无法突破模板上限——若这一步把页面都选成纯文字模板，下游永远补不回视觉。目标是避免整份 deck 选成清一色纯文字模板。

- 按 visual_strategy 设选择倾向（在模板适配度足够的前提下）：
  - `chart_dominant`：含可量化数据（趋势/对比/占比/时间序列）的页优先选 `chart_num > 0` 或 `table_num > 0` 的模板。
  - `image_dominant`：`page_role=content` 的页优先选 `image_num > 0` 的模板。
  - `mixed`：有可量化数据的页倾向图表/表格模板，偏概念/场景/情绪的页倾向图片模板。
- `content_density_profile` 调文字密度：`showcase-light` 进一步偏向视觉占比高的模板，`analysis-heavy` 允许文字承载更重的模板，但仍受下面软下限约束。
- **软下限（每章节至少一处视觉）**：以 `page_role ∈ {transition, catalog}` 为章节分界（无分界页时退化为「每连续 3 个 `page_role=content` 的页」为一组），每个章节 / 每组内至少 1 页所选模板带视觉槽（`image_num` / `chart_num` / `table_num` 任一 > 0）。`page_role ∈ {title, ending, transition, catalog}` 的页不计入该约束。
- **deck 级硬下限**：禁止整份 deck 全部选成纯文字模板（全 deck 每页 `image_num` / `chart_num` / `table_num` 均为 0）。
- **可用性兜底**：以上优先级以 `tag_gen_results.json` 实际存在合适模板为前提。若候选模板集本身缺少带视觉槽的模板、或带视觉槽的模板适配度都过低（与上面「不能为了多样性选适配度低的模板」同理，视觉也不能压倒适配度），则按现有适配优先原则选，并把「模板集缺视觉承载、无法满足视觉下限」写入 `unresolved`；**不得**硬套低适配模板，也**不得**为不存在的视觉能力伪造 `visual_requirements`。

## Enrich 步骤

写完 slim `template_map.json` 后，必须从此skill所在目录执行：

```bash
python scripts/enrich_template_map.py \
    --template-map <template_map.json 绝对路径> \
    --tags <tag_gen_results.json 绝对路径>
```

脚本会原地覆写 `template_map.json`，补全 `template_page_type`、`layout_category`、`template_constraints`、`template_raw_constraints`。

脚本退出码：

- `0`：正常退出，stdout 打印 `{"enriched_pages":..., "total_pages":...}`。继续阶段 3。
- `2`：**硬校验失败**（role / page_type 不匹配，例如 `page_role=ending` 却选了 `page_type=content` 的模板）。stderr 打印一行 JSON `{"status":"fail","reason":"page_role/template_page_type mismatch after enrich","mismatches":[...]}`。subagent **必须**据此重排涉事页的模板：从 `tag_gen_results.json` 中挑选与 `page_role` 对应 `page_type` 的真实候选，重写 slim `template_map.json` 对应条目的 `template_id` / `template_summary` / `selection_reason`，然后**再次**跑 enrich 脚本，直到 exit 码为 `0` 再进入阶段 3。不得以任何方式绕过本校验（例如改 `page_role` 去迁就选错的模板）。
- 其他非零退出码（`--template-map` 不存在、JSON 非法等）按原样向主 agent 上报并停止阶段 2。

- 完成 enrich 后（exit 码 0），继续读取 `stages/03-outline-generation.md` 执行阶段 3；不得提前中止。
