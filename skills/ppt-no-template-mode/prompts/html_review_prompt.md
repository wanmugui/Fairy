# PPT 单页截图评审指令（html_page_review）

给你的是一页 1280×720 PPT 的渲染截图。请以"专业 PPT 是否可直接交付"为标准评审这一页，**只返回一段合法 JSON**，不要任何解释、不要 markdown 代码块。

重点看截图里**肉眼可见**的版式问题（这是截图评审的价值所在）：

- `OUT_OF_PAGE`：元素超出页面边界、被画布裁掉。
- `OVERLAP`：文字/组件互相重叠、压字。
- `CLIPPED`：内容被容器裁断、显示不全。
- `EMPTY_BLOCK`：大片空白、明显空框/占位、该有图的地方是空的。
- `FAKE_IMAGE`：用色块/emoji/纯文字假冒图片。
- 视觉层级：标题不够突出、正文字号过小/过大、信息密度过高过挤或过空、对齐混乱。
- 配色/字体：明显刺眼配色、字体与整体不协调（仅记明显的）。

判定口径（务实，不吹毛求疵）：
- 结构硬伤（`OUT_OF_PAGE`/`OVERLAP`/`CLIPPED`/`EMPTY_BLOCK`/`FAKE_IMAGE`）任一出现 → `is_ok=false`、`needs_rewrite=true`。
- 只是"可以更好看"的轻微瑕疵（间距略紧、某处对齐稍偏）→ 记进 issues 但 `is_ok` 可为 true、`needs_rewrite=false`。
- 页面整体专业、无硬伤 → `is_ok=true`、`needs_rewrite=false`、`issues=[]`。

返回 JSON 结构（字段名固定）：
```
{
  "is_ok": true 或 false,
  "score": 0-100 的整数,
  "needs_rewrite": true 或 false,
  "issues": ["[OVERLAP] 标题和右上角图标压在一起", "[CLIPPED] 底部第三行被裁断", ...],
  "suggestion": "一段话，说明最该修的是什么、怎么修"
}
```

`issues` 每条以 `[CODE]` 开头，CODE 从上面列出的英文码里选；无问题写 `[]`。只返回这段 JSON。
