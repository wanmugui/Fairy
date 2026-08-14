# 阶段 7：html rewrite（已并入阶段 6 的批量脚本）

**本阶段不再单独发起。** rewrite 已经由阶段 6 的批量脚本 `scripts/html_page_review_batch.py` 一并完成：脚本评审后，对 `needs_rewrite=true` 且有 `issues` 的页，自动把「当前 HTML 全文 + 评审 issues」塞进 `html_page_generate` 重出整页修正版、覆盖 `page_xxx.html`、再跑一遍 `lint_pages.py` 兜结构。

因此复核子任务**只跑一次** `html_page_review_batch.py` 就同时拿到 review 报告和（按需）修正后的 HTML，无需在本阶段再做任何动作。

## rewrite 行为边界（脚本已遵守，列此备查）

- **只修一次**：每页最多重出一次修正版，不循环 review→rewrite。
- **整页重出而非逐处补丁**：rewrite 走和生成同一套 `html_page_generate` API，把当前 HTML + issues 作为输入让模型返回修好的整页；修正版仍必须过 `lint_pages.py` 的结构硬检查（图片渲染单元、display_id、motif、背景层、页面尺寸等），不过则该页计入 `rewrite_failed`。
- **不扩散**：只覆盖目标 `page_xxx.html`，不动 `outline.json` / `asset_map.json` / `style_spec.json` / 切片 / 其它页。
- **失败即记账**：rewrite 后仍过不了 lint 的页进入 `rewrite_failed`，由主 agent 在收口通知里据此判定该页是否失败，不在本阶段反复重试。
