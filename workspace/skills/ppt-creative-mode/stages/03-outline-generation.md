# 阶段 3：大纲生成

## outline.md 骨架契约（最高优先级）

生成前必须重新读取用户确认后的 `outline.md`。页数、页序、`page_type`、页标题和一二级要点层级必须严格一致，页标题禁止润色；用户调整后的内容优先于初始页数描述。本阶段只能补写正文、创意渲染字段、图片需求和机械字段。写完运行 `outline_md_preflight.py --against-json`，失败时重写 JSON，不得改 Markdown。

本文件是 `style-outline` subagent 的阶段 3 执行说明。完成阶段 2 后必须读取本文件。

## 输入

- 用户确认或编辑后的最新 `outline.md`
- `style_spec.md`（阶段 2 已写入）、`info_pack.json`、`task_pack.json`、`deck_dir`
- `outline.json` 输出路径（`<deck_dir>/outline.json`）

## 输出

- `outline.json`：必须写入输入包指定路径。

## 字段定义（单一权威 schema）

以下 class 定义是本阶段输出的**唯一权威 schema**。**凡未在 class 中声明的 key 一律视为非法**（顶层、页级、`content` 内、sub_point 内同此约束）。本文件其它段落只补充无法由类型表达的语义/跨字段约束，不重复定义字段。

```python
class Outline:
    schema_version: Literal["ppt_creative_outline_v1"]
    deck_dir: str
    ppt_id: str                            # = Path(deck_dir).name
    title: str                             # 整份 deck 标题
    language: str                          # 从 task_pack.language 取（如 "zh" / "en" / "es" 等）；缺失默认 "zh"
    total_pages: int
    pages: list["OutlinePage"]
    unresolved: list[str]


class OutlinePage:
    page_id: str                           # 必须形如 page_{NNN}（三位数，page_001 起）；禁止 slug 如 "cover"/"closing"；插页删页保持不变
    page_number: int                       # 1 基；连续无跳号；全 deck 唯一
    page_number_label: str                 # 必须含总页数；language=zh 形如 "第N页，共M页"，language=en 形如 "Page N of M"
    page_type: Literal["title", "content", "catalog", "transition", "summary", "ending"]
    title: str                             # 页面标题/副标题都收在这里
    layout: Layout                         # 必须是对象，不得写成字符串
    content: dict[str, "SubPoint"]         # key 必须形如 sub_point{N}（无前导零：sub_point1, sub_point2, ...）；除 sub_point{N} 外不得有任何其它 key
    needed_pictures: list["NeededPicture"]
    unresolved: list[str]


class Layout:
    category: str                          # 自由描述（非枚举），如 "single_column" / "two_column" / "grid" / "centered" / "custom"
    custom_description: str                # 本页版式的自然语言简述；所有视觉/版式描述统一收在这里，不另开字段


class SubPoint:
    icon: str                              # 语义图标标识，如 "fa-lightbulb"
    sub_point_name: str
    text: str                              # language=zh 时用中文引号「」包裹，形如 "「...」"；其它语言用该语言惯用标点（如 en 用引号或不加），不强制「」
    table: str                             # 语义摘要；无则空字符串
    chart: str                             # 语义摘要；无则空字符串
    picture: str                           # 引用 needed_pictures[].id 或 info_pack 既有图 id；无则空字符串
                                           # ↑ table / chart / picture 三者至多一个非空
    sub_sub_points: dict[str, "SubSubPoint"]  # key 形如 sub_sub_point{N}；无则空 dict {}


class SubSubPoint:
    icon: str
    sub_sub_point_name: str
    text: str


class NeededPicture:
    id: str                                # 形如 picture_{NNN}，全 deck 唯一
    tag: Literal["背景图", "示意图"]
    purpose: str                           # 5-15 字通用、可搜索短语；供后续阶段生成搜索词
    size_hint: str                         # 如 "1280x720" / "half_page" / "quarter_page"
```

## 处理要求

- 在 `style_spec.md` 约束下生成页面级大纲。
- 页数必须符合 `task_pack.json` 中的页数描述。
- 内容必须来自 `info_pack.json` 和 `task_pack.json`；**不得**补编来源不明的事实或数据；信息不足写入 `unresolved`。
- `ppt_id` 必须等于 `Path(deck_dir).name`；`language` 从 `task_pack.language` 取，缺失默认 `"zh"`；**所有对用户可见文本**（`title` / `text` / `sub_point_name` / `sub_sub_point` 文本 / `page_number_label` 等）必须使用该语言撰写，不得跟 query 语种回落成另一种语言。
- `layout.category` 和 `layout.custom_description` 由 subagent 按页面设计需要自由填自然语言简述，**非枚举**。
- 每个 sub_point 保留 `icon`，使用稳定语义标识（如 `fa-chart-line`、`fa-lightbulb`）；只作内容提示，后续渲染阶段决定具体 icon 实现。
- `needed_pictures` 仅做**图片需求声明**：本阶段不生成搜索关键词、不调用 image_search、不评估图片质量、不写本地路径——这些全部是资产规划阶段的职责。
- 完成 `outline.json` 写入后中止，等待主 agent 进入后续阶段。

## page_id / page_number 规则

- `page_id` 命名：`page_{NNN}`（三位数，从 `page_001` 起）；作为稳定标识，插页删页时保持不变，后续 asset_map / HTML 生成阶段通过它对齐。
- **禁止**用语义 slug 作为 `page_id`（如 `"cover"` / `"overview"` / `"closing"` 等都是非法的；这类语义信息只能放在 `title` 或 `page_type` 中）。
- `page_number` 从 1 起，连续无跳号；是当前大纲中的展示序号，可能因手动改稿而变化，但本阶段输出时必须覆盖 `1..total_pages` 且唯一。
- `page_number_label` 必须显式包含总页数；`language=zh` 格式如 `"第1页，共8页"`，`language=en` 格式如 `"Page 1 of 8"`。

## 页面规划规则

- **1-4 页 deck**：不强制封面/结尾页；允许压缩信息密度；不要凑页数填空洞结构。
- **5+ 页 deck**：必须有 `page_type=title` 封面和 `page_type=ending` 结尾页；目录/过渡页最多 1 页；封面日期精确到年月（若 task_pack 提供时间线索）。

## content 承载原则

class 已限定 `content` 的 key 形态和 value 结构。本节只说**信息归属**：

- 页面级语义（标题/副标题）走 `title`；版式描述走 `layout.custom_description`。**不**进 content。
- 所有具体内容（要点正文、列表项、卡片文字）**必须**全部承载在 `sub_pointN.text`（及其 `sub_sub_points`）里——**一个卡片 / 一个分项 = 一个 sub_point**。
- 每页 sub_point 数 + picture 数 + chart 数 + table 数总和 ≤ 6（各算 1 块）；6 块页面数 ≤ 总页数 40%。
- 每页 chart + picture 总数 ≤ 4。
- 同一页所有 sub_point 的 `picture / chart / table` 数量一致（结构对称）。

## needed_pictures `purpose` 撰写规则

- 5-15 字的通用、可搜索短语，与本页及整份 PPT 主题相关。
- 背景图：概念性总结（如 "故宫文物介绍"、"城市夜景"）。
- 示意图：具体实体（如 "团队会议"、"数据中心"）。
- **禁止**含具体数字 / 年份 / 统计数据 / 抽象隐喻 / 晦涩概念 / 特定人物姓名。
- 必须是 Google / Bing 可搜索的语言；每张图差异化。

## 自检项（返回前自查）

只列 class 定义无法表达的跨字段一致性：

- `style_spec.md` 存在且 `len > 0`。
- `len(pages) == total_pages`；`page_number` 覆盖 `1..total_pages` 且唯一。
- 每页 `content` 至少 1 个 sub_point。
- 每个 sub_point 中 `table / chart / picture` 至多一个非空。
- 同一页所有 sub_point 的 `picture / chart / table` 数量一致（结构对称）。
- 每页 `len(content) + #picture + #chart + #table ≤ 6`；`#chart + #picture ≤ 4`。
- `sub_point.picture` 引用的 id 出现在本页 `needed_pictures` 或 `info_pack` 既有图中。
- `needed_pictures[].id` 全 deck 唯一。
