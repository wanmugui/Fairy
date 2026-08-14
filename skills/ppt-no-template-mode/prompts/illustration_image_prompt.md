# 示意图生成 Prompt

用于阶段 4 资产规划时通过 `image_generate` 生成页面主体示意图。生成时用当前页、任务包和设计说明中的实际内容替换变量。

变量映射：

- `${title}`：来自 `task_pack.json.topic` 或 `outline.json.title`。
- `${use_case}`：来自 `task_pack.json.use_case`。
- `${audience_identity}`：来自 `task_pack.json.audience_identity`。
- `${speaker_identity}`：来自 `task_pack.json.speaker_identity`。
- `${design_theme}`：来自 `style_spec.json.design_theme`。
- `${visual_archetype}`：来自 `style_spec.json.visual_archetype`。
- `${page_number}`：来自 `outline.json.pages[].page_number`。
- `${page_title}`：来自 `outline.json.pages[].title`。
- `${page_content}`：来自当前页 `outline.json.pages[]` 的内容块摘要。
- `${caption}`：来自 `asset_map.json` 当前图片资产的 `caption` 或 `purpose`。
- `${size}`：来自资产槽位或生图工具要求的目标尺寸。

```text
你是一位审美稳定的 PPT 视觉设计师，请为生图工具生成一段详细中文提示词，用于生成 PPT 页面中的示意图。

PPT 主题：${title}
使用场景：${use_case}
听众身份：${audience_identity}
演讲者身份：${speaker_identity}
设计主题：${design_theme}
视觉原型：${visual_archetype}
当前页页码：${page_number}
当前页标题：${page_title}
当前页文本内容：${page_content}
示意图内容描述：${caption}
图片尺寸：${size}

核心要求：
1. 示意图必须表达当前页内容，不要偏离整套 PPT 主题。
2. 风格现代、简约、专业，具有职场演示美感，并符合 style_spec.json。**提示词必须显式写入 deck 调色板**：把 style_spec 的主色 hex、色调与情绪翻成英文关键词（如 "deep navy and muted gold palette, low-saturation"），让出图贴合整套配色。
3. 图片中严禁出现任何文字、字母、数字、标题、标志或水印。
4. 通过人物动作、环境氛围、抽象几何、物品、场景或流程关系表达含义。
5. 详细描述光影、材质、视角和构图。
6. 直接输出生图提示词，不要输出解释，不要包含 Markdown 代码块标签。
```
