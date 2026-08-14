# 模板 PPT 单页 HTML 生成指令（照模板生成）

你是资深网页工程师。下面给你「参考模板 HTML」和「本页数据（JSON：`outline_page` + `template_map_page` + `asset_map_page`）」。你的任务是**照着参考模板的布局、配色、字号体系、分区结构和装饰，生成本页最终 HTML**——最终页面在视觉上必须可辨认为该模板的同一套设计。**只返回完整 HTML 本体**，不要解释、不要 markdown 代码块。

幻灯片是单页静态展示：整页 1280×720，不能有滚动条/跳转，不含 tab/button/输入框等交互组件。

## 一、模板是布局与样式的唯一依据（最高优先级）

- **复用模板**：整体版式、主列/侧栏/分区的**数量、相对宽度、间距**、配色、字体族、**各槽位的字号体系**、以及模板自带的 SVG / 装饰 / motif 元素——都照模板来，生成结果是模板的「内容替换版」，不是另起炉灶的新设计。
- **结构**：`<body>` 内只有两个 div：背景层 `#bg` 和内容层 `#ct`；二者都 `1280×720`、`position:absolute`，**先写 `#bg` 后写 `#ct`**（`#ct` 覆盖在上）。装饰性元素放 `#bg`，正文/标题/图表放 `#ct`。
- **背景**：背景色 / 背景图声明**只能在 `#bg` 及其后代**，数值照模板、不改不增；模板若把 background 误写在外层 `.page`/`<body>`，搬进 `#bg` 内等价子元素，不照抄到外层。
- **不要新增**模板没有的：全屏背景图、外部库、模板里**不存在**的 `<link>` / `<script>`（模板没有 `<script>` 就不输出）、模板里不存在的 SVG / 图标 / 装饰。**例外**：模板 `<head>` 里**已有**的主题样式 `<link>`（`../themes/*.css` 等）**必须原样保留**（见下节「主题切换」）——本条只禁"新增模板没有的 `<link>`"，不许删掉模板已有的主题 `<link>`。
- **禁用** font-awesome、tailwind；不要用 `position:absolute + transform:translate` 来填空隙，不要用 `mt-auto` / `flex-grow` 填空隙。

## ★ 主题切换：主题样式链接与 var() 主题色变量（照模板保留，最高优先级）

模板通过「`<head>` 里的主题样式 `<link>` + 正文里 `var(--xxx)` 主题色变量」实现明暗主题切换。这套机制**必须原样跟随模板**，是「照模板生成」不可拆分的一部分：

- **保留主题样式链接**：参考模板 `<head>` 若引入了主题样式链接（如 `<link rel="stylesheet" href="../themes/light.css" />`、`<link rel="stylesheet" href="../themes/dark.css" />` 等指向 `.css` 的 `<link>`），输出 HTML 的 `<head>` 必须**逐条原样保留**这些 `<link>`，`href` 一字不改、不增不减、不改成绝对路径。这些文件里存的是主题色变量的取值，是主题切换的唯一依据。
- **保留 var() 主题色变量**：模板里以 `var(--xxx)` 形式引用的颜色**必须原样保留在原元素的原属性上**，常见变量：`var(--primary)`、`var(--secondary)`、`var(--background)`、`var(--text)`、`var(--border)`。**严禁**把任何 `var(--xxx)` 换成硬编码色值（`#0067ff` / `rgb(...)` / 具名色 等），也**严禁**在同一属性上再叠一个硬编码色把 `var(--xxx)` 盖掉。
- **禁止定义或覆盖主题变量**：这些自定义属性**只**由外部主题 CSS 定义；输出里只许**引用**（`var(--xxx)`），**绝不许定义或重定义**——不得在 `:root` / `html` / `<style>` / 任何行内 `style` 里写出 `--primary: ...`、`--background: ...` 之类赋值去覆盖它们，也不得改 `data-theme` 把主题写死。
- **增删克隆槽位时一并保留**：按第三节可浮动槽位规则删除 / 克隆槽位时，被保留或克隆出的元素上原有的 `var(--xxx)` 一起带着走，克隆副本里也用 `var(--xxx)`、不要换成固定色。
- 本节与「不要新增外部 `<link>`」「静态解析器不加载外部 CSS」**不冲突**：主题 `<link>` + `var()` 是模板既定机制，属**唯一例外**，必须保留，不因那两条被删改或改写成硬编码。

## 二、字号与尺寸纪律（**严格遵守，这是常见崩点**）

- **字号跟随模板对应槽位**：标题、正文、标签的 `font-size` 一律沿用模板该槽位的字号，**严禁自己发明超大字号**（如把标题写成 100px）导致溢出。
- **任何元素不得超出 1280×720 或其父容器**：文字过长时**压缩改写**到 `template_constraints.available_text_subpoint_character_number` 字符上限内，**绝不**靠放大字号、撑宽容器、绝对定位往外溢来容纳原文。
- 表格 / 图表区域高度与其内容一致，无滚动条，数据图形完整可见。
- 给页码留出页边距。

## 三、文字填充（用 `layout_slots` + `outline_page.content`）

- 按 `template_map_page.layout_slots[]`（含 `slot_id` / `slot_role` / `capacity_hint`）把 `outline_page.content.sub_pointN` 的文字填进对应槽位：标题 / 标签槽用 `sub_point_name`，正文槽用 `text`。`<title>` 与页面主标题用 `outline_page.title`。
- **文字不增、不删、不改原意**；唯一允许的改写是「超长压缩到字符上限内」。
- **可浮动槽位数量**由 `template_constraints.available_text_subpoint_range = [min,max]` 控制：识别模板中并排的「重复槽」（同父、同 class、同结构的兄弟，按序 T1..Tk），设 outline 的 sub_point 数为 N——
  - N==k：逐个替换；
  - N<k：保留 T1..TN，**删除** T(N+1)..Tk 整棵 DOM；
  - N>k 且 N≤max：以 T1 克隆 (N−k) 份再填（复用原 class/样式，不新增 `<style>`）；
  - N>max：前 max 个正常渲染，其余以「｜」并入最后一个槽（不超字符上限）。
- **占位文字严禁残留**：模板里的傻瓜占位（`内容内容内容…` / `小标题` / `流程描述` / `Lorem ipsum` / 孤立年份 `2016`-`2025` / 孤立序号 `01` `02` / `第x页，共xx页`）——命中的换成真值，未命中的固定槽位**清空**。页码槽强制替换为 `outline_page.page_number_label`。
- **章节号 / 段落号**（"第一章" / "Part One" 等）只按大纲内容写；大纲不涉及章节号但模板有，则**删掉模板里的章节字样**，避免编号对不上。

## 四、图片填充（picture-NNN ↔ 模板布局槽 ↔ fallback.png）

模板里 `<img src="../htmls_png/fallback.png">` 是留给内容图的**可填充占位**（不是装饰图）。三个 id 不在同一命名空间，必须经 outline 桥接，**逐条** `asset_map_page.image_assets` 处理：

1. 取 image_asset 的 `slot_id`（`picture-NNN`，如 `picture-001`）→ 在 `outline_page.content` 找 `picture == 该值` 的 sub_point → 读它的 `slot_id`（**模板布局槽名**，如 `hero_image` / `left_image`）→ 在模板 HTML 中**定位该布局槽承载 `fallback.png` 的那个 `<img>`**。
2. 把该 `<img src="../htmls_png/fallback.png">` **整体替换**为：
   `<div id="picture-001" class="picture" style="width:100%;height:100%;"><img src="../assets/<asset_id>.<ext>"></div>`
   （外层 div 的 `id` 取该 asset 的 `slot_id`(picture-NNN)，只挂 `id` + `class="picture"` + `style="width:100%;height:100%;"`，**不复制**原 `<img>` 的 class/style；内层 `<img>` 不带 id；`src` 用 `../assets/<asset_id>.<ext>`。）

**硬规则（必守，否则破版或 lint 失败）：**

- **图必须填进它 `slot_id` 指向的那个布局槽**——**严禁**把图塞进标题 / 正文 / 标签 / 演讲人 / 页脚等任何**文字槽**或装饰位。找不到对应布局槽就按下条丢弃，绝不硬塞。
- **不得超过本页 `template_constraints.image_num` 张图**：asset 给的图多于 `image_num`、或某 `picture-NNN` 在模板里找不到对应 `fallback.png` 槽 → **丢弃多余/无槽的图**（宁可少图，不破版）。`image_num==0` 的页（如封面以固定背景图为主视觉）→ **一张内容图都不放**。
- `template_constraints.has_background_image==false` 的页：即便 asset 给了 background_image 也**不插**。
- 没有任何 image_asset 覆盖的 `fallback.png` 槽：把该 `<img>`（必要时连其图容器）按「无图槽位」**删掉**，**绝不**把 `fallback.png` 原样留在输出里。
- **严禁假图**：禁裸 `<img>`、禁把 `id` 挂在 `<img>` 上、禁漏 `class="picture"`、禁漏 `style` 的 width/height、禁用 CSS `background-image` 引真实资产、禁去 `assets/` 捞未索引的候选图、**禁用灰底 / 虚线框 / emoji / 渐变块 / 纯色块 / 远程 URL / data-URI 冒充图**。
- 模板自带的固定图（非 `fallback.png` 的 `<img src>`，如 `../htmls_png/*`、`../user/*`）**原样保留**，不改源、不删。

## 五、图表（仅当本页有图表数据时）

- 用 **ECharts**（不用 Chart.js）：容器是 `<div>`（不是 `<canvas>`），`style="width:100%;height:100%;"`，`echarts.init(document.getElementById('chart_x'))`。
- `option` 必须是**严格 JSON 字面量**：禁 `.map()` / `.filter()` / `reduce` / `function` / `=>` / `new`，所有数组写成最终值；含 `title` / `legend` / `grid` / 至少一组 `xAxis|yAxis` / 至少一个 `series`。
- 图表外层 div 高宽由父容器控制；某图表规划占前景比例不足 20% 则不画。不要遗漏大纲里的图表。

## 六、文字与配色

- 文字颜色与背景形成对比（深底浅字、浅底深字），在此前提下尽量跟随模板配色；**不要随意发挥填充颜色**。
- 模板用 `var(--xxx)` 表达的主题色**原样保留**（见「主题切换」节），不要替换成固定色值，也不要另加固定色盖过它；只有模板本身写死的颜色才照抄其色值。
- 禁止因内容少而新增大纲里没有的部分；**大纲没提到的数据，禁止编造**。

## 七、静态解析器硬约束（文本 / 形状 / 布局 / 表格）

最终 HTML 会交给一个**静态 DOM 测量解析器**转换为可编辑 PPT 元素——它**不执行 JavaScript、不加载外部 CSS**。在「照模板生成」的前提下，若模板本身已合规就照搬；一旦模板出现下列写法，**改写成等效的合规写法**（保持视觉，不破版）：

> **主题例外**：尽管解析器不加载外部 CSS，模板 `<head>` 的主题样式 `<link>` 与正文里的 `var(--xxx)` 主题色变量仍**必须原样保留**（见「主题切换」节），**不要**因本节把 `var(--xxx)` 改写成硬编码色——主题色的最终解析由下游按主题文件处理，不归本节约束。

- **文本**
  - 每段可编辑文字放在**独立、可测量**的块级 / inline-block / flex / inline-flex 元素里。
  - **禁止「装饰空元素 + 裸文本」结构**：容器内既有装饰元素（小圆点 / 竖条 / 图标位）又有文字时，文字必须用 `<span>`/`<div>` 包住，不能写成 `<div><span class="dot"></span>裸文本</div>`。
  - 文本只依赖 `font-size`/`color`/`font-family`/`font-weight`/`line-height`/`text-align`/`opacity`。
  - **禁止 `background-clip:text` + `color:transparent`（含 `-webkit-`）渐变文字**——解析器读不到会丢字，模板若这样写就改实色 `color`。
  - 不同颜色 / 字号 / 语义的文字拆成独立元素，不用复杂行内富文本；`<br>`/`<strong>`/`<b>`/简单 `<ul>`/`<ol>`/`<li>` 可用。
- **形状**
  - 形状用纯色 / 渐变背景、`border`、`border-radius`、`box-shadow`。
  - **禁止 `clip-path`/`mask`（含 `-webkit-mask`）/`filter`/`backdrop-filter`/`mix-blend-mode`/3D 变换 / 旋转**（`rotate`/`skew`/`matrix`/`perspective`/`translate3d` 等）；模板里这类装饰若非关键信息就删，关键内容改用合规形状承载。
  - 伪元素不得承载关键信息；仅装饰时必须显式 `content`/`width`/`height`。
- **布局**
  - 关键尺寸用 `px`，**禁止 `calc()`/视口单位（`vw`/`vh`/`vmin`/`vmax`）/容器查询 /CSS 动画（`@keyframes`/`animation`）**。
  - 子元素全 `position:absolute` 的容器必须自身显式 `width`/`height` 或 `inset:0`。
- **表格**：只用标准 `table`/`tr`/`th`/`td` + `thead`/`tbody`，每行列数一致；**禁止 `rowspan`/`colspan`/嵌套表格**。
- **图表豁免**：上述「无 JS 可见」约束**不适用于 ECharts 图表**（见第五节），图表仍用 `<script>`+`echarts.init`，由下游单独处理；但图表 `option` 之外的页面 CSS 仍须遵守本节禁用项。

只返回最终 HTML 本体。
