# 阶段 3：大纲生成

## outline.md 骨架契约（最高优先级）

生成前必须重新读取用户确认后的 `outline.md`。它是页结构的唯一权威来源：页数、页序、`page_type`、页标题、`sub_point_name`、`sub_sub_point_name` 及父子关系必须逐项一致。页标题只能去掉 Markdown 的编号和 `[页型]` 标记，禁止润色。用户增删、移动或改写后的结构优先于 `task_pack.page_count_desc`。本阶段只能补写正文、图表、资产、布局和机械字段。写完运行 `outline_md_preflight.py --against-json`；失败则重写 JSON，不得修改 Markdown。

本文件是 style-outline subagent 的阶段 3 执行说明。完成阶段 2 后必须读取本文件。

## 输入

- 用户确认或编辑后的最新 `outline.md`
- `info_pack.json`、`style_spec.json`、`task_pack.json`、deck 工作目录

## 输出

- `outline.json`：必须写入输入包指定地址，不得只在聊天消息中返回。

## 字段定义（单一权威 schema）

以下 class 定义是本阶段输出的**唯一权威 schema**。**凡未在 class 中声明的 key 一律视为非法**（顶层、页级、`content` 内、sub_point 内同此约束）。本文件其它段落只补充无法由类型表达的语义/跨字段约束，不重复定义字段。

```python
class Outline:
    schema_version: Literal["ppt_no_template_outline_v1"]
    deck_dir: str
    ppt_id: str                            # = Path(deck_dir).name
    language: str                          # 从 task_pack.language 取（如 "zh" / "en" / "es" 等）；缺失默认 "zh"
    title: str                             # 整份 deck 标题
    total_pages: int
    pages: list["OutlinePage"]
    unresolved: list[str]


class OutlinePage:
    page_id: str                           # 必须形如 page_{NNN}（三位数，page_001 起）；禁止 slug 如 "cover"/"day1"；插页删页保持不变
    page_number: int                       # 1 基；连续无跳号；全 deck 唯一
    page_number_label: str                 # 必须含总页数；language=zh 形如 "第N页，共M页"，language=en 形如 "Page N of M"
    page_type: Literal["title", "content", "catalog", "transition", "summary", "ending"]
    title: str                             # 页面标题/副标题/英雄条文字都收在这里
    layout: Layout                         # 必须是对象，不得写成字符串
    content: dict[str, "SubPoint"]         # key 必须形如 sub_point{N}（无前导零：sub_point1, sub_point2, ...）；除 sub_point{N} 外不得有任何其它 key
    background_picture: str                 # 整页背景大图的 picture-NNN（铺满页底、不属于任何 sub_point）；无背景图则空字符串
    unresolved: list[str]

    page_type_variant_id: str              # 必须存在于 style_spec.page_type_variants[].variant_id
    page_goal: str
    content_budget: ContentBudget


class Layout:
    category: str                          # 自由描述，如 "single_column" / "two_column" / "grid" / "centered" / "custom"
    custom_description: str                # 本页的「迷你设计稿」（3-5 行版式硬指令，HTML 阶段逐条执行）；所有视觉/版式描述统一收在这里，不另开字段


class SubPoint:
    icon: str                              # 语义图标标识，如 "fa-chart-line"
    sub_point_name: str
    text: str                              # language=zh 时用中文引号「」包裹，形如 "「...」"；其它语言用该语言惯用标点（如 en 用引号或不加），不强制「」
    table: str                             # 语义摘要；无则空字符串
    chart: str                             # 语义摘要；无则空字符串
    picture: str                           # 本块配图的 picture-NNN（连字符+三位，全 deck 唯一，本阶段直接铸造）；无则空字符串
                                           # ↑ 永远是 picture-NNN，禁止填 info_pack 既有图 id；table / chart / picture 三者至多一个非空
    picture_source_ref: str                # 复用 info_pack 既有图时填该图 image_id（info_pack.available_images[].image_id）；需新搜/生成则空字符串
    sub_sub_points: dict[str, "SubSubPoint"]  # key 形如 sub_sub_point{N}；无则空 dict {}
    block_type: Literal["heading", "body_text", "bullet_list", "chart", "table", "image", "quote", "kpi"]


class SubSubPoint:
    icon: str
    sub_sub_point_name: str
    text: str


class ContentBudget:
    max_content_blocks: int                # ≤ 6；每个 sub_point / chart / table / illustration 各算 1 块；
                                           # background_picture（整页底图）不占内容块名额，不计入
    require_chart_or_table: bool           # 本阶段按 visual_strategy 真实设置（见「视觉策略与配图规划」），非装饰字段
    require_image: bool                    # 同上：按 visual_strategy 真实设置，require_image=true 的页必须至少一个 sub_point.picture 非空（或 background_picture 非空）
```

## 处理要求

- 在 `style_spec.json` 约束下生成页面级大纲；每页 `page_type_variant_id` 必须存在于 `style_spec.page_type_variants[].variant_id`，不得新造 variant。
- **不**生成或消费 `template_map.json`（那是模板模式的产物，本模式无模板）。
- 页面数量符合 `task_pack.json` 中的页数描述；无法确定具体页数时，按 `task_pack.json` 的约束写入合理页数假设，并在 `unresolved` 中说明假设依据。
- 内容必须来自 `info_pack.json` 和 `task_pack.json`，**不得**新增来源不明的数据、事实或结论；信息不足写入 `unresolved`，不得补编。
- `language` 从 `task_pack.language` 取，缺失默认 `"zh"`；**所有对用户可见文本**（`title` / `text` / `sub_point_name` / `sub_sub_point` 文本 / `page_number_label` 等）必须使用该语言撰写，不得跟 query 语种回落成另一种语言。
- 每个 sub_point 保留 `icon`，使用稳定语义标识（如 `fa-chart-line`、`fa-lightbulb`）；只作内容提示，后续渲染阶段决定具体 icon 实现。
- 图片需求**仅做声明**：本阶段不生成搜索关键词、不调用 image_search、不评估图片质量、不写本地路径——这些全部是后续阶段（资产规划）的职责。
- 完成 `outline.json` 写入后中止，等待主 agent 进入后续阶段。

## page_id / page_number 规则

- `page_id` 命名：`page_{NNN}`（三位数，从 `page_001` 起）；作为稳定标识，插页删页时保持不变，后续 asset_map / HTML 阶段通过它对齐。
- **禁止**用语义 slug 作为 `page_id`（如 `"cover"` / `"overview"` / `"day1"` / `"highlights"` / `"closing"` 等都是非法的；这类语义信息只能放在 `title` 或 `page_type` 中）。
- `page_number` 从 1 起，连续无跳号；可能因手动改稿而变化，但本阶段输出时必须覆盖 `1..total_pages` 且唯一。
- `page_number_label` 必须显式包含总页数；`language=zh` 格式如 `"第1页，共8页"`，`language=en` 格式如 `"Page 1 of 8"`。

## 页面规划规则

- **1-4 页 deck**：不强制封面/结尾/目录页；允许压缩信息密度；不要凑页数填空洞结构。
- **5+ 页 deck**：必须有 `page_type=title` 封面和 `page_type=ending` 结尾页；目录/过渡页最多 1 页；封面日期精确到年月（若 task_pack 提供时间线索）。
- **结尾页内容收敛**（`page_type=ending`）：结尾页以**总结性结论 + 致谢 / 行动号召**为主，**不放比较详细的信息**（多条卖点逐项展开、数据点罗列、功能清单、多卡片堆叠都不要），`content_budget.max_content_blocks ≤ 2`。需要「关键卖点回顾 / 数据总结」这类详细收尾时，**单开一页** `page_type=summary`（用 `style_spec.page_type_variants` 里 purpose 为总结的 variant，如无则用 `content`）放在结尾页之前，不要把详细总结揉进结尾页。封面（`page_type=title`）不受此约束——可正常承载标题/副标题及少量引导性内容。
- 整份 deck 中 `max_content_blocks=6` 的页面数 ≤ 总页数 70%（配合"默认富填"策略，允许多数页面靠近内容上限承载；只有 `content_density_profile=showcase-light` 或用户明示克制时，主动把这个比例降低）。
- 同一 deck 内 sub_point 数量相同的页面，`picture / chart / table` 的分布也要一致（结构对称）。
- **版式节奏**：相邻两页的 `layout.category` 尽量不同（two_column 接 grid 接 single_column…），避免连续多页同一种版式。
- **`layout.custom_description` 是迷你设计稿**（HTML 阶段逐条执行的版式硬指令，不是装饰性描述），必须写清 3-5 行：
  1. **版式结构**：出血图放哪侧占多宽 / 几列网格 / 左右对峙 / KPI 数字卡几张横排——选一种明确写出；
  2. **图片处理**（本页有图时）：出血方向 + 渐隐遮罩方向（如「图右侧出血 40%，向左渐隐进底色」）；多图页写「全部图统一压暗加主色罩」；
  3. **视觉主角**：这页第一眼看什么——超大数字 / 主图 / 图表 / 对峙标题，并点名标题中哪 1-2 个词用强调色；
  例：`"左 60% 文字区右 40% 出血图向左渐隐；视觉主角=超大数字 48 与 104 两张 KPI 卡横排；标题中「48 强」用强调色"`。
- **结尾页禁出现任何含"聆听"的字样**（"感谢聆听 / 谢谢聆听"一律不用；用"谢谢 / Q&A / 愿景式收束"等替代）。
- `page_type=content` 的 variant 必须是 `style_spec.page_type_variants` 中 purpose 对应"正文内容页"的那一款。

## 视觉策略与配图规划

本阶段必须读取 `task_pack.visual_strategy`（`chart_dominant` / `image_dominant` / `mixed`）和 `task_pack.content_density_profile`，据此驱动每页 `content_budget.require_chart_or_table` / `require_image` 以及 `sub_point.picture` / `background_picture` / chart / table 的安排。**这两个字段不是装饰，必须真实消费**——目标是避免整份 deck 以纯文字页为主。

- 按 `visual_strategy` 设页面级视觉倾向：
  - `chart_dominant`：含可量化数据（趋势/对比/占比/时间序列）的页 `require_chart_or_table=true`，优先用 `block_type=chart` / `table` 的 sub_point 承载。**数据必须来自 `info_pack` 的 `chart_data` / `data_tables`，无数据不得编造**，缺口写入 `unresolved`。
  - `image_dominant`：`page_type=content` 的页 `require_image=true`，优先给 sub_point 配 `picture`（与正文搭配的示意图），或给页设 `background_picture`（氛围底图）。
  - `mixed`：按页面内容性质择一——有可量化数据走图表，偏概念/场景/情绪走配图。
- `content_density_profile` 只调文字密度、不取消视觉：`analysis-heavy` 允许更高文字承载（但仍受下面软下限约束）；`showcase-light` 进一步压低文字、抬高配图比重。

### 软下限：每章节至少一处视觉

- 章节切分：以 `page_type ∈ {transition, catalog}` 为章节分界；无分界页时退化为「每连续 3 个 `page_type=content` 的页」为一组。
- 每个章节 / 每组内**至少 1 页**带非文字视觉元素（该页某个 sub_point 的 `chart` / `table` / `picture` 非空，或该页 `background_picture` 非空）。
- `page_type ∈ {title, ending, transition, catalog}` 的页**不计入**该约束（允许纯文字或自带主视觉）。
- **不要求每页都有图**——信息量小的页不必硬塞视觉，避免版面 clutter。
- **deck 级硬下限**：整份 deck 禁止「零图表且零图片」。若 `visual_strategy=chart_dominant` 但 `info_pack` 完全无可用数据，必须退化为至少安排图片满足本下限，并在 `unresolved` 记录数据缺口。

## content 承载原则

class 已限定 `content` 的 key 形态和 value 结构。本节只说**信息归属**：

- 页面级语义（标题/副标题/英雄条文字）走 `title`；版式描述走 `layout.custom_description`。**不**进 content。
- 所有具体内容（要点正文、数据点、列表项、卡片文字、时间轴节点、tip group）**必须**全部承载在 `sub_pointN.text`（及其 `sub_sub_points`）里——**一个卡片 / 节点 / 分项 = 一个 sub_point**。**页面可见文字以 content 为唯一来源**：HTML 阶段每个 sub_point 渲染为一个挂 `data-display-id="<sub_point key>"` 的内容块，写不进 content 的文字就不会出现在页面上。
- `block_type=chart` 时 `chart` 字段写清图表类型、数据维度和**全部具体数值**（例如「柱状图：德国 4.2 / 巴西 3.8 / 法国 3.5（万亿美元 GDP）」）——HTML 阶段照此绘图，**没写数值的图表画不出来，也不得让下游编造**。
- `block_type=table` 时 `table` 字段写清表头、行数和**每个单元格的内容**（可用「行1：A / B / C；行2：…」紧凑表示），HTML 阶段照此填表，不得让下游补编。
- 同一页所有 sub_point 的 `picture / chart / table` 数量保持一致（结构对称）。

## 配图规则（picture-NNN）

- **id 铸造**：每处配图的 id 在本阶段直接铸造，写到 `sub_point.picture`（块级配图）或 `OutlinePage.background_picture`（整页底图）上；格式 `picture-NNN`（连字符 + 三位递增数字），全 deck 唯一、跨页递增。**不再有 needed_pictures 数组**——图片需求就由这两处 picture 字段承载。
- **永远 picture-NNN**：`sub_point.picture` / `background_picture` 只能填 picture-NNN，**禁止**填 `info_pack` 既有图的 image_id，也禁止填模板槽之类其它 id。下游 asset_map / HTML 全按 picture-NNN 对齐。
- **既有图复用**：本页要用的图若已在 `info_pack.available_images` 里（用户自备 / 上游下载），仍照常铸一个 `picture-NNN` 给 `sub_point.picture`，并把该既有图的 `image_id` 填到同一 sub_point 的 `picture_source_ref`；stage 4 见到 `picture_source_ref` 非空就直接复用那张图、不再搜。需要新搜/生成的图，`picture_source_ref` 留空。
- 多图场景**必须拆**：三张人物卡、对比双图、瀑布流图组等，每张图一个独立 sub_point + 独立 `picture-NNN`，不得共用一个 id。
- **每页配图数量分布（软目标，仅为控制搜图开销，不是每页的正确性判据）**：在**需要靠图片表达**的 `page_type=content` 页里，单页配图数（= `picture` 非空的 sub_point 数 + 是否有 `background_picture`）**倾向于**符合以下 deck 级分布——
  - **~60%** 的页：配图数 **1**（含复用既有图也算 1）；
  - **~30%** 的页：配图数 **2~3**；
  - **~10%** 的页：配图数 **3 或更多**，**仅**用于人物组、对比图组、瀑布流图组等天然多图场景。
- `page_type ∈ {title, ending, transition, catalog}` 的页**不**计入此分布（按需配图或纯文字均可）。
- **图表页豁免**：`content` 页若已用 `chart` / `table` 承载视觉，就**不**计入上面的配图分布分母，也**不**需要为了凑分布再补 `picture` / `background_picture`——chart/table 本身即视觉，软下限已认它。配图分布只约束「确实要靠图片表达」的页。
- 小 deck（content 页 ≤ 4）允许偏离严格比例，但仍保持「主流单图、少数多图」的相对结构，不得把全部 content 页都堆成多图页。

## 自检（跑确定性脚本，不要手写校验）

写完 `outline.json` 后**只跑这一个脚本**做自检，**不要**自己用 `execute_code` 手写 Python 逐项校验：

```bash
python scripts/outline_preflight.py --deck <deck_dir>
```

读 stdout 一行 JSON `{"status":"ok"/"fail","errors":[...],"warnings":[...]}`：

- `status=ok`（exit 0）：通过（`warnings` 仅提示，可按需微调，不强制）。完成本阶段。
- `status=fail`（exit 2）：按 `errors` **一次性整体重写 `outline.json`**（覆盖写，**不要**逐处 `edit_file` 反复改），再**跑同一个脚本确认一次**即可。

本阶段子任务内的自检用于尽早修正；子任务返回后，主 agent 仍必须重复运行本脚本，并紧接着运行 `outline_md_preflight.py --against-json`。主 agent 的两次确定性校验是进入阶段 4 的最终准入闸门，不能用子任务口头汇报替代。

脚本已覆盖下列全部硬校验（errors）与软目标（warnings），无需再人工逐条核对；以下清单仅供理解脚本在查什么：

- `len(pages) == total_pages`；`page_number` 覆盖 `1..total_pages` 且唯一。
- 每页 `content` 至少 1 个 sub_point，且 `len(content) ≤ content_budget.max_content_blocks`。
- `page_type=ending` 的页：`len(content) ≤ 2` 且 `content_budget.max_content_blocks ≤ 2`，只承载总结性结论 + 致谢，不放详细信息；详细卖点/数据总结另开 `page_type=summary` 页，不揉进结尾。
- 每个 sub_point 中 `table / chart / picture` 至多一个非空。
- 同一页所有 sub_point 的 `picture / chart / table` 数量一致（结构对称）。
- 含 `chart` / `table` 的 sub_point：字段里写齐了具体数值 / 单元格内容（HTML 阶段唯一数据来源）。
- 所有 `sub_point.picture` 和 `background_picture` 的值都是 `picture-NNN` 格式，**没有**填成 info_pack image_id 或其它格式。
- 所有 `picture-NNN`（含 `sub_point.picture` 与 `background_picture`）全 deck 唯一，无重复 id。
- `picture_source_ref` 非空的 sub_point，其 `picture` 也非空（是个 picture-NNN），且 `picture_source_ref` 的值能在 `info_pack.available_images[].image_id` 中找到。
- `require_image=true` 的页：至少一个 sub_point 的 `picture` 非空，或 `background_picture` 非空。
- `require_chart_or_table=true` 的页：至少一个 sub_point 的 `chart` 或 `table` 非空。
- 软下限：按「视觉策略与配图规划」的章节切分，每个章节 / 每连续 3 个 `content` 页一组内，至少 1 页带非文字视觉元素。
- deck 级硬下限：全 deck 至少存在一处非空 `chart` / `table` / `picture`（或 `background_picture`），不得整份纯文字。
- 配图分布检查（**软目标，仅提示不阻断**）：在「需要靠图片表达」的 `page_type=content` 页里（**已用 chart/table 承载视觉的页不计入分母**），单页配图数为 1 的占 ~60%、2~3 的占 ~30%、≥3 的占 ~10%（±15%）。**仅当**绝大多数图片页都堆成 2~3 张时，才把多图页收紧到 1 张；**不要**为了凑这个比例，给本可纯文字、或已有 chart/table 的页硬加 `background_picture`。
