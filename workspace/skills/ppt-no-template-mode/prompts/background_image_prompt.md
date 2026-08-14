# 背景图生成 Prompt

用于阶段 4 资产规划时通过 `image_generate` 生成背景图。生成时用当前页、任务包和设计说明中的实际内容替换变量。

变量映射：

- `${title}`：来自 `task_pack.json.topic` 或 `outline.json.title`。
- `${speaker_identity}`：来自 `task_pack.json.speaker_identity`。
- `${use_case}`：来自 `task_pack.json.use_case`。
- `${audience_identity}`：来自 `task_pack.json.audience_identity`。
- `${design_theme}`：来自 `style_spec.json.design_theme`。
- `${visual_archetype}`：来自 `style_spec.json.visual_archetype`。
- `${page_title}`：来自 `outline.json.pages[].title`。
- `${outline}`：来自当前页 `outline.json.pages[]` 的页面目标和内容块摘要。
- `${caption}`：来自 `asset_map.json` 当前图片资产的 `caption` 或 `purpose`。
- `${size}`：来自资产槽位或生图工具要求的目标尺寸。

```text
你是一位专家级 UI/UX 演示设计师，请生成一张高保真的 PPT 背景图。

PPT 主题：${title}
演讲者身份：${speaker_identity}
使用场景：${use_case}
听众身份：${audience_identity}
设计主题：${design_theme}
视觉原型：${visual_archetype}
当前页标题：${page_title}
当前页大纲内容：${outline}
背景图内容描述：${caption}
图片尺寸：${size}

要求：
1. 背景图必须服务当前页内容和整套 PPT 主题。
2. 图片中不要出现任何文字、字母、数字、标题、标志或水印。
3. 背景不要出现过高饱和度元素，避免影响前景文字和图表可读性。
4. 画面适合作为 PPT 背景，留出足够的前景内容承载空间。
5. 风格必须符合 style_spec.json 中的设计主题和视觉原型。
6. **提示词必须显式写入 deck 调色板**：把 style_spec 的主色 hex、色调与情绪翻成英文关键词写进生图提示词（例如 "deep navy and muted gold palette, low-saturation, editorial mood"），让出图天然贴合整套配色，不要出一张五颜六色的图破坏全套。
6. 输出一张适合放在该页 PPT 中的背景图。

渲染质量要求：高质感、细节充分、构图稳定、专业演示设计风格。
```
