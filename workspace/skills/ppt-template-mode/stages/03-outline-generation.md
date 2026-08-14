# 阶段 3：大纲生成

## outline.md 骨架契约（最高优先级）

生成前必须重新读取用户确认后的 `outline.md`。页数、页序、`page_type`、页标题、一级/二级要点名称、顺序和父子关系必须严格一致；标题禁止润色。本阶段只补正文、图表、资产、布局、模板槽位和机械字段。用户修改优先于初始页数参数。模板容量冲突应回到阶段 2 重选模板；无可用模板则显式失败，不得改动 Markdown 骨架。写完必须运行 `outline_md_preflight.py --against-json`。

本文件是 template-outline subagent 的阶段 3 执行说明。完成阶段 2 并写入 `template_map.json` 后必须读取本文件。

## 输入

- 用户确认或编辑后的最新 `outline.md`：页数、页序、页型、标题和要点层级的唯一权威来源。
- `task_pack.json`：语言、受众、场景、视觉策略和其他任务约束。
- `info_pack.json`：内容来源和信息缺口的总汇。
- `template_map.json`：阶段 2 的输出，提供每页的 page_id / page_number / template_id / 页面角色 / 布局类别 / 模板摘要 / 模板约束。

## 输出

- `outline.json`：必须写入输入包指定地址，所有页面使用同一固定 schema。

## 字段定义（单一权威 schema）

以下 class 定义是本阶段输出的**唯一权威 schema**。**凡未在 class 中声明的 key 一律视为非法**（顶层、页级、`content` 内、sub_point 内同此约束）。本文件其它段落只补充无法由类型表达的语义/跨字段约束，不重复定义字段。

```python
class Outline:
    schema_version: Literal["ppt_template_outline_v1"]
    deck_dir: str
    ppt_id: str                            # = Path(deck_dir).name
    language: str                          # 从 task_pack.language 取（如 "zh" / "en" / "es" 等）；缺失默认 "zh"
    title: str                             # 整份 deck 标题
    total_pages: int
    pages: list["OutlinePage"]
    unresolved: list[str]


class OutlinePage:
    page_id: str                           # 必须与 template_map.json 对应页一致；形如 page_{NNN}（template_map 若用 slug 应回到阶段 2 修正，不在本阶段重命名）
    page_number: int                       # 必须与 template_map.json 对应页一致；1 基；连续无跳号
    page_number_label: str                 # 必须含总页数；language=zh 形如 "第N页，共M页"，language=en 形如 "Page N of M"
    page_type: Literal["title", "content", "catalog", "transition", "summary", "ending"]
    title: str                             # 页面标题/副标题都收在这里
    layout: Layout                         # 必须是对象，不得写成字符串
    content: dict[str, "SubPoint"]         # key 必须形如 sub_point{N}（无前导零：sub_point1, sub_point2, ...）；除 sub_point{N} 外不得有任何其它 key
    unresolved: list[str]

    # —— template 模式扩展字段 ——
    template_id: str                       # 必须来自 template_map.json，不得在本阶段重新选择
    page_goal: str                         # 本页目标的一句话说明


class Layout:
    category: str                          # 可直接复用 template_map.json 的 layout_category
    custom_description: str                # 把内容如何放入所选模板的简短说明；所有视觉/版式描述统一收在这里，不另开字段


class SubPoint:
    icon: str                              # 语义图标标识，如 "fa-chart-bar"
    sub_point_name: str
    text: str                              # language=zh 时用中文引号「」包裹，形如 "「...」"；其它语言用该语言惯用标点（如 en 用引号或不加），不强制「」
    table: str                             # 仅 template_constraints.table_num > 0 时允许非空
    chart: str                             # 仅 template_constraints.chart_num > 0 时允许非空
    picture: str                           # 本块配图的 picture-NNN（连字符+三位，全 deck 唯一，本阶段直接铸造）；image_num=0 时必须为空字符串
                                           # ↑ 永远是 picture-NNN，禁止填 info_pack 既有图 id；table / chart / picture 三者至多一个非空
    picture_source_ref: str                # 复用 info_pack 既有图时填该图 image_id（info_pack.available_images[].image_id）；需新搜/生成则空字符串
    sub_sub_points: dict[str, "SubSubPoint"]  # key 形如 sub_sub_point{N}；无则空 dict {}

    # —— template 模式扩展字段 ——
    slot_id: str                           # 必须来自 template_map.json.pages[].layout_slots（模板 DOM 槽，与 picture 命名空间无关）


class SubSubPoint:
    icon: str
    sub_sub_point_name: str
    text: str
```

## 处理要求

- **不**做模板选择：每页 `template_id` 必须来自 `template_map.json`，**禁止**在本阶段替换模板编号。
- **不**做图片搜索 / 下载 / 生图：仅声明图片需求（图片落地是资产规划阶段的职责）。
- `language` 从 `task_pack.language` 取，缺失默认 `"zh"`；**所有对用户可见文本**（`title` / `page_goal` / `sub_point_name` / `text` / `sub_sub_point` 文本 / `page_number_label` 等）必须使用该语言撰写，不得跟 query 语种回落成另一种语言。
- 根据 `info_pack.json` 组织内容，生成**逻辑连贯、层次分明、语言风格一致**的页面大纲。
- 每页必须包含 `title`、`page_goal` 和至少 1 个 sub_point；复杂内容用 `sub_sub_points` 进一步拆分。
- 内容必须来自 `info_pack.json` 和 `task_pack.json`；**不得**新增来源不明的事实、数据或图表结论；信息不足写入 `unresolved`。
- 按 `task_pack.visual_strategy` **充分利用模板已经提供的视觉槽**，不要让 stage 2 选中模板里的 chart / table / image 槽空着、退化成纯文字。
- **图槽必须用 sub_point 显式认领**（否则图会被孤立，stage 5 渲染时会回落到模板的 `fallback.png` 占位）。对每个 `template_map_page.layout_slots` 里 `slot_role=image`（或 `allowed_content_kinds` 含 `picture`）的图槽：
  - 在 `outline_page.content` 里创建一个 sub_point，`slot_id` = 该图槽（如 `left_image`），并把该 sub_point 的 `picture` 字段铸成一条 `picture-NNN`（连字符 + 三位递增数字，全 deck 唯一）。两者接成一条链：`sub_point.slot_id`（模板 DOM 槽）↔ `sub_point.picture`（picture-NNN）。
  - 这个图 sub_point 的 `text` 可以留空字符串或写一句配图说明（如「画面意象」），但 `slot_id` 和 `picture` **不得**留空——它们承担接线职责。若该图复用 info_pack 既有图，再把既有图的 `image_id` 填进 `picture_source_ref`。
  - 同样地，`chart_dominant` 时若模板有 chart/table 槽（`chart_num`/`table_num>0`），优先安排 sub_point 把可量化数据排成 `chart` / `table`；数据必须来自 `info_pack`，缺失写入 `unresolved`，不得编造。
- 模板未提供某类视觉槽（`*_num=0`）时不强求，保持空字符串即可——stage 3 不能突破模板上限。
- 文本服务用户 query、演讲者身份、听众身份和使用场合；术语深度、叙述视角和内容侧重点必须与 `info_pack.json` 的任务上下文一致。
- 每个 sub_point 保留 `icon`，使用稳定语义标识（如 `fa-chart-bar`、`fa-users`）；只作内容提示，后续渲染阶段决定具体 icon 实现。
- 完成 `outline.json` 写入后中止，等待主 agent 进入后续阶段。

## page_id / page_number 规则

- `page_id` 必须与 `template_map.json` 对应页**一致**，作为稳定标识；后续资产规划、页面生成阶段都通过它对齐。
- **禁止**用语义 slug 作为 `page_id`（如 `"cover"` / `"overview"` / `"day1"` / `"closing"` 等）。即使 `template_map.json` 用了 slug，也**不**在本阶段重命名——应回到阶段 2 修正 `template_map.json`。
- `page_number` 必须与 `template_map.json` 对应页一致；从 1 起，连续无跳号。
- `page_number_label` 必须显式包含总页数；`language=zh` 格式如 `"第1页，共8页"`，`language=en` 格式如 `"Page 1 of 8"`。

## content 承载原则

class 已限定 `content` 的 key 形态和 value 结构。本节只说**信息归属**：

- 页面级语义（标题/副标题）走 `title`；版式描述走 `layout.custom_description`。**不**进 content。
- 所有具体内容（要点正文、列表项、卡片文字、时间轴节点）**必须**全部承载在 `sub_pointN.text`（及其 `sub_sub_points`）里——**一个卡片 / 节点 / 分项 = 一个 sub_point**，并分配合法的 `slot_id`。
- 同一页所有 sub_point 的 `picture / chart / table` 数量保持一致（结构对称）。

## 模板约束（template 模式独有）

- `layout.custom_description` 必须服从 `template_map.json` 的 `layout_category` / `template_summary` / `template_constraints`；优先消费 `layout_slots` / `layout_description` / `content_fit_hint` / `visual_requirements` / `template_raw_constraints`，不得只凭 `layout_category` 或自然语言猜版式。
- **每个 sub_point 必须填 `slot_id`**，且属于 `template_map.json.pages[].layout_slots`。
- 若 sub_point 需要 `table` / `chart` / `picture`，对应槽位的 `allowed_content_kinds` 必须允许该类型。
- `table` / `chart` / `picture` 数量不得超过 `template_constraints` 的对应上限；`*_num=0` 时对应字段必须为空字符串。
- 不得违背模板编排方向（约束为三分栏不得编成左文右图；约束为图文左右布局不得编成纯表格页）。
- `template_map.json` 信息不足以约束编排时，回到阶段 2 修正，**不**在本阶段猜测。

## sub_point 数量与模板容量对齐（硬约束）

每页 `len(content)` 必须严格落在本页 `template_constraints.available_text_subpoint_range = [min, max]` 内；单点 `text` 长度 ≤ `available_text_subpoint_character_number`。**不**得把超限/不足的页留给后续阶段处理。

- **超上限**（`N_raw > max`）：不得合并或删减用户要点；回到阶段 2 为该页重选容量足够的模板。确无可用模板则写入 `unresolved` 并失败退出。
- **低下限**（`N_raw < min`）：不得提升、拆分或新增用户未确认的要点；回到阶段 2 重选容量匹配的模板。确无可用模板则写入 `unresolved` 并失败退出。

禁止的反模式：**截断/丢弃信息**、把溢出塞进 `unresolved`、把溢出迁到别页或新增页、占位 sub_point（"待补充"/"暂无"）、重复同一 sub_point 凑数。模板容量不匹配时必须回到阶段 2 重选真实可承载的模板，但不得修改用户确认的 Markdown 骨架。

## 配图规则（picture-NNN）

- **id 铸造**：每处配图的 id 在本阶段直接铸造，写到 `sub_point.picture` 上；格式 `picture-NNN`（连字符 + 三位递增数字），全 deck 唯一、跨页递增。**不再有 needed_pictures 数组**。
- 仅在对应页槽位 `allowed_content_kinds` 允许 picture 时给 sub_point 配 `picture`；`image_num=0` 的页所有 sub_point 的 `picture` 必须为空字符串。
- **永远 picture-NNN**：`sub_point.picture` 只能填 picture-NNN，**禁止**填 `info_pack` 既有图 image_id，也禁止填模板槽 id（模板槽走独立的 `slot_id` 字段）。
- **既有图复用**：本页要用的图若已在 `info_pack.available_images` 里，仍照常铸一个 `picture-NNN` 给 `sub_point.picture`，并把该既有图 `image_id` 填进同一 sub_point 的 `picture_source_ref`；stage 4 见到 `picture_source_ref` 非空就直接复用、不再搜。需要新搜/生成的图，`picture_source_ref` 留空。

## 自检项（返回前自查）

只列 class 定义无法表达的跨字段一致性：

- 每页 `page_id` / `page_number` / `template_id` 与 `template_map.json` 一致。
- 每个 sub_point `slot_id` ∈ `template_map.pages[].layout_slots`，且未被重复占用（除非模板的同一 slot 允许多条，如 `step_flow` 这类可浮动槽位）。
- `min ≤ len(content) ≤ max`；每个 `sub_point.text` 长度 ≤ `available_text_subpoint_character_number`。
- 每个 sub_point 中 `table / chart / picture` 至多一个非空，且符合 `template_constraints`。
- 同一页所有 sub_point 的 `picture / chart / table` 数量一致（结构对称）。
- 所有 `sub_point.picture` 的值都是 `picture-NNN` 格式，**没有**填成 info_pack image_id 或模板槽 id；全 deck 唯一无重复。
- `picture_source_ref` 非空的 sub_point，其 `picture` 也非空（picture-NNN），且 `picture_source_ref` 能在 `info_pack.available_images[].image_id` 中找到。
- **图槽认领闭环**（防止图被孤立）：
  - 每个 `template_map_page.layout_slots` 中 `slot_role=image` 的图槽都必须被本页某个 sub_point 用 `slot_id` 认领（除非确实没图可放，且该槽允许空着）。
  - 每个 `sub_point.picture` 非空的 sub_point，其 `slot_id` 必须指向 `slot_role=image` / `allowed_content_kinds` 含 `picture` 的模板槽——**禁止孤立配图**（铸了 picture-NNN 但没接到任何模板图槽上）。

以上任一项不满足，本轮内返工，不得把不达标的 outline 写到 `outline.json`。
