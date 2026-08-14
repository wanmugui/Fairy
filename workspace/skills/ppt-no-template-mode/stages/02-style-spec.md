# 阶段 2：设计信息生成

本文件是 style-outline subagent 的阶段 2 执行说明。style-outline subagent 收到主 agent 传入的输入包后，必须先读取本文件。

## 输入

- `task_pack.json`
- `info_pack.json`
- 用户确认或编辑后的最新 `outline.md`
- **风格库** `references/design-styles.md`（74 设计风格 / 22 色调 / 29 主色，选型必读）
- 用户图片本地地址列表（如有）
- deck 工作目录

## 输出

- `style_spec.json`

必须写入输入包指定的 `style_spec.json` 输出文件地址，不得只在聊天消息中返回。

生成前必须读取最新 `outline.md`。用其中实际存在的页型、页面主题、要点结构和信息密度设计整套视觉体系及页面变体，确保每类页面都有可承载的设计方案。`outline.md` 只约束结构和内容形态，不提供正文事实；任务背景、受众、场景和视觉策略仍取 `task_pack.json`，内容类型和可用素材仍取 `info_pack.json`，具体风格、色调和主色仍必须从线上风格库中选择。禁止按初始页数描述或修改前的大纲设计风格。

## 字段定义

```python
class StyleSpec:
    schema_version: str
    deck_dir: str
    source_files: StyleSpecSources
    design_concept: DesignConcept
    color_palette: list["ColorRole"]
    typography: Typography
    decorative_elements: DecorativeElements
    ui_components: UIComponents
    page_type_variants: list["PageTypeVariant"]
    density_rules: list[str]
    diversity_rules: list[str]
    anti_patterns: list[str]
    fallback_archetype: str
    unresolved: list[str]


class StyleSpecSources:
    task_pack: str
    info_pack: str
    user_images: list[str]


class DesignConcept:
    core_theme: str                  # 一句话定义整体风格
    texture_and_material: TextureAndMaterial
    shape_language: str              # 核心图形语言（例如：几何、手绘、扁平、拟物）


class TextureAndMaterial:
    description: str                 # 材质与肌理的文字描述
    css_rules: str                   # 对应的 CSS 代码片段


class ColorRole:
    role: str                        # "background" | "primary" | "secondary" | "accent" | "border" | "shadow" | "text"
    hex: str                         # "#RRGGBB"
    alpha: float                     # 0.0-1.0
    usage: str                       # 用途说明
    is_global: bool                  # 是否全局（≥40% 页面出现）


class Typography:
    title_strategy: FontStrategy     # 标题字体策略，font_family_css 必须从 Library B 选
    body_strategy: FontStrategy      # 正文字体策略，font_family_css 必须从 Library A 选


class FontStrategy:
    source_library: str              # "Library A (body)" | "Library B (title)"
    font_family_css: str             # 完整 CSS font-family 值
    suggested_weight: str            # 字重建议，如 "400" / "700"
    base_line_height: float          # 行高倍数
    letter_spacing: str              # 字距，如 "0.02em"


class DecorativeElements:
    global_background_layer: list["DecorationItem"]  # 全局背景装饰
    local_decorations: list["LocalDecoration"]       # 按 page_type 分配的局部装饰


class DecorationItem:
    data_motif_key: str              # 稳定装饰标识
    description: str                 # 装饰语义描述
    html_template: str               # 可直接嵌入 #bg 或 #ct 的 HTML/CSS 代码片段
    palette_binding: str             # 绑定 color_palette 中哪个 role


class LocalDecoration(DecorationItem):
    target_page_types: list[str]     # 适用的 page_type，如 ["content", "summary"]


class UIComponents:
    charts: ChartSpec
    lists: ListSpec
    dividers: DividerSpec


class ChartSpec:
    color_sequence: list[str]        # ECharts 系列配色序列（hex）
    echarts_options: dict            # 默认 ECharts option 片段（如 textStyle、axis、legend）


class ListSpec:
    marker_style: str                # 列表标记 CSS，如 "disc"、"custom SVG"、"数字圆牌"


class DividerSpec:
    css_border: str                  # 分割线 CSS，如 "1px solid rgba(255,255,255,.2)"


class PageTypeVariant:
    variant_id: str                  # 稳定 ID，outline.OutlinePage.page_type_variant_id 引用
    purpose: str                     # 页面类型用途，如 "封面"、"正文内容页"、"过渡页"
    layout_intent: str               # 布局意图
    content_density: str             # "low" | "medium" | "high"
    background_motif_recipe: MotifRecipe
    foreground_motif_recipe: MotifRecipe


class MotifRecipe:
    motif_keys: list[str]            # 引用 DecorativeElements 中的 data_motif_key
    placement: str                   # "full-screen" | "edge-band" | "corner-cluster" | "side-panel" | "top-ribbon" | "sparse"
    density: str                     # "high" | "medium" | "low"
    opacity_hint: float              # 0.0-1.0
```

## 风格选型（三维点名，最先做）

**写 `style_spec.json` 之前，必须先读 `references/design-styles.md`，从三个表里各点名选一项**——选型是"从库里挑"，不是凭感觉现编：

1. **设计风格**：74 种里选一个贴合 `task_pack`（topic / use_case / 受众 / style_intent）的，写名字（如「暗黑极简」「国潮」「高端奢侈」）。
2. **色调**：22 种里选一个，**写名字 + ID**（如「深色/暗色系(ID 1)」「黑金(ID 8)」「冷色调(ID 4)」）。**这一项直接决定 `color_palette` 的 background**。
3. **主色**：29 种里选一个，**写名字 + 确切 hex**（如「克莱因蓝 #002FA7」「墨绿 #1B5E20」）；`color_palette` 中 `role="accent"`（或 primary）的 hex **直接用这个值**，不得另调。

把选定的「风格 × 色调(ID) × 主色(hex)」三元组写进 `design_concept.core_theme`（例：`"暗黑极简 × 深色系(ID1) × 克莱因蓝 #002FA7 —— 沉稳科技蓝调"`），整套 palette / 装饰 / variant 都从这个三元组推导。用户明确给了风格/品牌色时优先用户的，再从库里找最接近的一档对齐。

## 背景纪律（防 AI 默认味）

- **⚠️ 奶油 / 米黄 / 暖白底是被严重滥用的"安全牌"，默认不用。** 动笔前先问：这个主题最该是什么底？背景跟着**选定的色调**走：
  - 商务 / 科技 / 数据 / 金融 / 政务 / 奢侈 / 赛博 → 优先**深底**（深蓝黑 / 近黑 / 深墨绿）；
  - 医疗 / 科研 / 法律 / 冷感主题 → 冷白蓝 / 冷灰；
  - 高对比 / 包豪斯 / 杂志感 → 纯白或纯黑；
  - 奶油 / 暖白**只留给真正偏暖的**：生活 / 文教 / 手作 / 婚庆 / 母婴 / 餐饮；其它主题用暖白必须有明确理由。
- **全套只用一个背景模式**：要么亮底、要么暗底；封面 / 过渡页可用**同一色系更深 / 更浓的变体**做戏剧感，**绝不**亮底暗底来回跳（暗底封面 + 亮底正文 = 两个 deck 的割裂感）。
- **调色板封闭**：deck 用到的**所有**颜色都必须在 `color_palette` 一次定全——**包括状态色**（涨跌 / 正负 / 警示 / 提示，role 写 `up` / `down` / `warn` / `note`），且状态色必须从主色 / 辅助色**派生**（同色系深浅或冷暖偏移），**严禁**为"负数 / 风险"硬塞调色板外的大红大绿大粉。

## 字段含义

- `design_concept.core_theme`：一句话定义整体风格，例如"沉稳商务蓝 + 几何装饰"。
- `design_concept.texture_and_material`：材质肌理描述 + 对应 CSS 代码。
- `color_palette`：每种颜色必须绑定明确的 role，hex 与 alpha 清晰，`is_global=true` 的颜色至少覆盖 40% 页面。
- `typography`：标题字体必须从 Library B 中选一款，正文字体必须从 Library A 中选一款，全 deck 统一，不得混用。
- `decorative_elements`：每个装饰条目必须有 `html_template`（一段可直接嵌入 `#bg` 或 `#ct` 的 HTML/CSS）。
- `page_type_variants`：至少覆盖封面、目录/过渡、正文、总结、结尾等常见类型；每种 variant 必须定义 `background_motif_recipe` 和 `foreground_motif_recipe`。
- `diversity_rules`：防止全 deck 页面视觉单调的规则。
- `anti_patterns` **必须包含 AI 味黑名单**（在此基础上按主题增补）：靛蓝 `#6366f1` 套紫白的烂大街配色（除非主色就选了它）、什么都居中、一排排一模一样的卡片、无意义毛玻璃、奶油底默认、ECharts 默认调色板。
- `fallback_archetype`：用户信号不足时退回哪种风格。

## 字体库

`typography.title_strategy.font_family_css` 必须从 **Library B** 中选一款，`typography.body_strategy.font_family_css` 必须从 **Library A** 中选一款。全 deck 标题字体统一、正文字体统一，不得混用库外字体。

**离线优先原则**：渲染环境通常无法访问 Google Fonts。为保证 HTML 离线可看，`font_family_css` **必须以系统字体兜底**——CSS font-family 列表里，第一项可以是任意字体名（含 Google Fonts），但**最后一项必须是系统字体兜底**（例如 `sans-serif` / `serif`），且如果 Library 中标注"需 Google Fonts"的字体被选中，该字体**必须**和它匹配的系统字体一起列出，格式形如 `'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', sans-serif`。

**Library A（正文字体，body）**：

- `'PingFang SC', sans-serif` — Mac 风、优雅 *（系统字体，离线可用）*
- `'Microsoft YaHei', sans-serif` — 经典、兼容性好 *（Windows 系统字体，离线可用）*
- `'Noto Sans SC', sans-serif` — 现代、通用 *（Google Fonts，需配 PingFang/YaHei 回退）*
- `'Noto Serif SC', serif` — 优雅、传统 *（Google Fonts，需配系统 serif 回退）*
- `'Arial', sans-serif` — 兜底 *（系统字体，离线可用）*

**Library B（标题字体，title）**：

- `'PingFang SC', sans-serif` — 标题走现代简洁路线时可直接用系统字体 *（离线可用）*
- `'Microsoft YaHei', sans-serif` — 标题走厚重现代风时的系统字体兜底 *（离线可用）*
- `'ZCOOL QingKe HuangYou', sans-serif` — 圆润海报风 *（Google Fonts，需配 'Microsoft YaHei' / sans-serif 回退）*
- `'ZCOOL XiaoWei', serif` — 柔和艺术衬线 *（Google Fonts，需配 serif 回退）*
- `'ZCOOL KuaiLe', cursive` — 圆润、俏皮 *（Google Fonts，需配 sans-serif 回退）*
- `'Ma Shan Zheng', cursive` — 正楷毛笔、传统 *（Google Fonts，需配 'KaiTi' / 'STKaiti' / serif 回退）*
- `'Zhi Mang Xing', cursive` — 有力毛笔 *（Google Fonts，需配 'KaiTi' / serif 回退）*

**离线回退示例（推荐直接使用）**：

- 正文（商务/通用）：`'PingFang SC', 'Microsoft YaHei', sans-serif`
- 正文（国际化）：`'PingFang SC', 'Microsoft YaHei', sans-serif`
- 标题（现代厚重）：`'Microsoft YaHei', 'PingFang SC', sans-serif`
- 标题（毛笔中式）：`'Ma Shan Zheng', 'STKaiti', 'KaiTi', serif`

subagent 写 `font_family_css` 时直接选一条离线回退示例或按同样格式组合，确保即使首选字体加载失败，系统仍能渲染成视觉上接近的替代字体。

## 装饰元素规则

**骨架组件库（按需选配，不强制全上）**：以下三个骨架件是编辑级页面框架的备选组件。**按选定的风格与题材决定配哪几件**（编辑感/商务/科技/杂志类通常三件都配；卡通、手绘、极简留白、仪式感满幅大图等风格可只配 eyebrow、或一件不配——风格说了算，不为配而配）。**选用了哪件，就把哪件的 key 写进相应 `page_type_variants[].foreground_motif_recipe.motif_keys`**（lint 按 motif_keys 强制兑现；没选用的不写、不强制）：

- `deco_meta_bar`：顶部 meta 细条（≤60px 高）——deck 短标题 / 场合短语（宽字距小字）；不放页码（页码由 `#footer` 独占）。
- `deco_eyebrow`：主标题上方的章节标签——强调色细条 / 斜切块 + 短标签（文案由生成阶段按该页 `page_goal` 填）；这是强调色的标准用武之地，三件里最通用的一件。
- `deco_footbar`：底部信息条——章节名 / 一句数据摘要（宽字距小字、弱化颜色）；同样不放页码。

**骨架文案的语言跟随 deck 语言与风格**：中文主题默认用中文短语；英文 caption 只在风格本身带国际化/编辑感（杂志、发布会、体育播报等）时使用，不得为了"洋气"硬塞英文。**meta_bar 与 footbar 同时配置时，两者文案不得相同**（一个放主题短语、一个放章节/数据摘要）——同句重复会触发 DUP_TEXT_PHRASE 检查。骨架件的 `html_template` 遵守下方技术安全约束（色值绑 palette role、无 data URI、无复杂 SVG）；类名用 `deck-meta` / `deck-eyebrow` / `deck-footbar` 这类专名（**别**用 `.card`/`.panel` 等内容容器类名）。


- `decorative_elements` 分为 `global_background_layer`（全局背景装饰）和 `local_decorations`（按 page_type 分配的局部装饰）。
- 每个装饰条目必须含 `data_motif_key` 和 `html_template`（一段可直接嵌入 `#bg` 或 `#ct` 的 HTML/CSS 代码片段）。
- 全局风格只能抽象元素（色彩、字体、阴影、圆角、边框、透明度、无纹理背景）；出现在 ≥40% 页面的元素才算全局。
- 具体形状（印章、插画、吉祥物、复杂 SVG、照片）禁止进入 `global_background_layer`，必须放入 `local_decorations`，即使每页都出现。
- 判定方法：移除该元素若破坏结构即为全局；仅是装饰即为局部。
- 非极简 variant 的 `background_motif_recipe` 与 `foreground_motif_recipe` 不能同时为空；至少一侧要提供可感知的装饰配方。
- 正文 / 内容页 variant 不能把 `background_motif_recipe` 留空。
- 正文 / 内容页的背景 recipe 至少要有一个大面积或跨边缘的 motif 配方，例如 `full-screen`、`edge-band`、`corner-cluster`、`side-panel`、`top-ribbon`。
- 正文 / 内容页不要只给一个角落里的小图标就算完成。
- 如果页面包含真实图片或主视觉照片，真实图片也不能替代背景装饰配方；背景与前景 recipe 仍然要成立。

## 技术安全约束

- **禁止**在 `html_template` 中使用 data URI（`data:image/svg+xml` 或 base64 字符串）。
- 纹理 / 噪声只能用纯色或 CSS gradient 实现，禁止用代码生成的噪点图。
- 几何图形优先用 `<div>` + `border-radius` / `transform` / `box-shadow` / `linear-gradient` 实现，避免复杂 SVG；确需 SVG 时必须内联书写、结构尽量简单。
- `html_template` 中的色值必须绑定 `color_palette` 中已声明的 role，不要写死未声明的 hex 值。

## 处理要求

- 生成无模板模式的整体设计说明，约束后续大纲和 HTML 页面生成。
- `style_spec.json` 至少定义 design_concept、color_palette、typography、decorative_elements、ui_components、page_type_variants、density_rules、diversity_rules、anti_patterns、fallback_archetype 和 unresolved。
- 用户图片只能作为风格参考或资产线索，不得当成模板文件。
- 用户图片不足时也必须生成可执行的基础设计说明，不能输出空泛默认风格，也不能退回普通白卡片风格。
- `page_type_variants` 必须能覆盖封面、目录或过渡页、正文页、总结页等常见页面类型；不确定时写入 placeholder，后续由用户或下游阶段细化。
- 设计方向必须和 `task_pack.json` 中的使用场合、受众、演讲者身份、页面风格和交付要求一致。
- 视觉系统必须支撑 `task_pack.visual_strategy`：`chart_dominant` 时 `ui_components.charts`（配色序列、echarts_options）要给实，让图表页有统一图表样式；`image_dominant` 时正文/内容页 `page_type_variants` 的 `layout_intent` 要为示意图/前景图预留版面位置（不要排成满屏文字版式），使下游大纲声明的配图有处可放。
- `color_palette` 中每个颜色必须有明确的 `role`、`hex`、`alpha`、`usage`、`is_global` 字段。
- `typography` 的标题字体必须从 Library B 选择，正文字体必须从 Library A 选择，全 deck 统一。
- `decorative_elements` 中每个装饰条目必须有可执行的 `html_template`，不得只写文字描述。
- 对未明确的设计字段写入待补充 placeholder，后续由用户或下游阶段细化。
- 不得读取、引用或生成模板编排结果。
- 完成阶段 2 后继续读取 `stages/03-outline-generation.md`。
