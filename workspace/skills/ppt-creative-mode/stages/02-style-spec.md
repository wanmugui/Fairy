# 阶段 2：风格生成

本文件是 `style-outline` subagent 的阶段 2 执行说明。subagent 收到主 agent 传入的输入包后，必须先读取本文件。

## 输入

- `task_pack.json` 绝对路径
- `info_pack.json` 绝对路径
- 用户确认或编辑后的最新 `outline.md` 绝对路径
- `user_images`：用户上传图片本地路径列表（可空）
- `deck_dir`
- `style_spec.md` 输出路径（`<deck_dir>/style_spec.md`）
- `prompts/query2style.md` 绝对路径
- `prompts/image2style.md` 绝对路径

## 输出

- `style_spec.md`（markdown 格式，非 JSON）

必须写入输入包指定的 `style_spec.md` 输出路径，不得只在聊天消息中返回。

生成前必须读取最新 `outline.md`。根据其中实际存在的页型、页面主题、要点结构和信息密度规划创意方向、版式语言与页面节奏，确保风格能够覆盖用户调整后的完整结构。`outline.md` 不替代内容来源：任务背景、受众、场景和视觉策略仍取 `task_pack.json`，内容类型、既有素材和信息缺口仍取 `info_pack.json`。禁止按初始页数描述或修改前的大纲生成风格。

## 处理要求

1. 判断 `user_images` 是否非空：
   - **非空** → 读 `prompts/image2style.md`，将 `${reference_images}` 占位替换为 reference 图片的视觉描述 + `task_pack.json` 摘要。subagent 可用视觉模型能力读图；如视觉能力不可用，仅凭 `task_pack.json` 描述也必须给出可执行风格。
   - **空** → 读 `prompts/query2style.md`，将 `{content_summary}` 替换为 `task_pack.json` + `info_pack.json` 核心内容摘要，将 `{font_info}` 替换为下文列出的固定字体列表。
2. 按 prompt 要求生成 markdown 文档。第一行固定为标题 `# 视觉风格与美术指导 (Visual Style & Art Direction)`。必须完整覆盖 prompt 中列的 10 个基础部分：插画/图形风格、构图形式、线条风格、几何化/图形处理、空间与透视、装饰元素、配色方案、背景、字体排版、整体氛围。然后追加增强章节：`Theme Profile`、`Visual Axes`、`Global Visual System`、`Page-Type Adaptation`、`Sparse-Content Expansion Rules`、`Do / Don't`。
3. 写入 `style_spec.md` 绝对路径。
4. 完成阶段 2 后**不中止**，继续读 `stages/03-outline-generation.md` 进入阶段 3。

## 关键约束

- `style_spec.md` 是 markdown 文档。**禁止**输出为 JSON 结构、YAML 或任何非 markdown 形态。
- 有图必用 `image2style.md`，无图必用 `query2style.md`；**不混用**两份 prompt。
- 所选字体必须来自下文列出的字体 Library，且必须写入离线兜底（系统字体作为 font-family 最后一项）。
- 内容必须和 `task_pack.json` 中的使用场合、受众、演讲者身份、页面风格、交付要求一致。

## 字体 Library（query2style.md 中 {font_info} 占位的填充内容）

**离线优先原则**：渲染环境通常无法访问 Google Fonts。字体列表里第一项可为任意字体名（含 Google Fonts），但最后一项必须是系统字体兜底（例如 `sans-serif` / `serif`）。

**Library A（正文字体，body）**：

- `'PingFang SC', sans-serif` — Mac 风、优雅（系统字体，离线可用）
- `'Microsoft YaHei', sans-serif` — 经典、兼容性好（Windows 系统字体，离线可用）
- `'Noto Sans SC', sans-serif` — 现代、通用（Google Fonts，需配 PingFang/YaHei 回退）
- `'Noto Serif SC', serif` — 优雅、传统（Google Fonts，需配系统 serif 回退）
- `'Arial', sans-serif` — 兜底（系统字体，离线可用）

**Library B（标题字体，title）**：

- `'PingFang SC', sans-serif` — 标题走现代简洁路线（离线可用）
- `'Microsoft YaHei', sans-serif` — 标题走厚重现代风（离线可用）
- `'ZCOOL QingKe HuangYou', sans-serif` — 圆润海报风（Google Fonts）
- `'ZCOOL XiaoWei', serif` — 柔和艺术衬线（Google Fonts，需配 serif 回退）
- `'Ma Shan Zheng', cursive` — 正楷毛笔、传统（Google Fonts，需配 STKaiti / serif 回退）
- `'Zhi Mang Xing', cursive` — 有力毛笔（Google Fonts）

**推荐离线回退示例**：

- 正文商务通用：`'PingFang SC', 'Microsoft YaHei', 'Source Han Sans SC', sans-serif`
- 正文国际化：`'Inter', 'PingFang SC', 'Microsoft YaHei', sans-serif`
- 标题现代厚重：`'Microsoft YaHei', 'PingFang SC', sans-serif`
- 标题毛笔中式：`'Ma Shan Zheng', 'STKaiti', 'KaiTi', serif`
