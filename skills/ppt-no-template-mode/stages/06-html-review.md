# 阶段 6+7：html review + rewrite（批量并发调 API）

本文件是阶段 6/7「页面复核」subagent 的执行说明。**本阶段不再每页一个子任务、也不再由模型自己截图或代码级判读**；改成一个复核子任务跑一个批量脚本，脚本内部并发（默认 4）对所有页：读 HTML → 把 `../assets/` 本地图内联成 `data:` URL → `html_to_png`（**stateless 模式**，传 HTML 字符串）拿 `png_base64` → `html_page_review` 视觉评审 → 落 `page_xxx.review.md`；对 `needs_rewrite` 的页把「当前 HTML + 评审 issues」塞进 `html_page_generate` 重出修正版 → 覆盖 → 重跑 lint。subagent 收到输入包后先读完本文件。

## 输入

- deck 工作目录绝对路径
- 阶段 5 已产出 `htmls/page_xxx.html` 和 `htmls/page_xxx.input.json`
- 评审 prompt：`prompts/html_review_prompt.md`（给 `html_page_review` 的评审指令，要求返回可解析 JSON）
- 生成 prompt：`prompts/html_gen_no_template.md`（rewrite 重出修正版时复用的格式约束）

## 输出

- 每页 `htmls/page_xxx.review.md`（评审报告，唯一评估产物，markdown）
- 对 `needs_rewrite` 的页：原地覆盖的 `htmls/page_xxx.html`
- 截图走 `html_to_png` 的 **stateless 模式**，直接返回 `png_base64` 给 `html_page_review`，**不落任何 png 文件**；**不写** `review.json` / `*_render.html` / `tmp_shot_*.py` / `*.bak` 等副产物

## 处理要求

1. **只跑这一个脚本**，从本 skill 所在目录执行：

   ```bash
   python scripts/html_page_review_batch.py \
     --deck <deck_dir> \
     --review-prompt prompts/html_review_prompt.md \
     --gen-prompt prompts/html_gen_no_template.md \
     --concurrency 4
   ```

   脚本会：加载所有 `htmls/page_*.input.json`；**并发**每页 = 读 `page_xxx.html` → 把 `../assets/` 本地图内联成 `data:` URL（stateless html_to_png 不读沙盒，必须自带图）→ `html_to_png(mode=stateless, html_file_content=内联后的HTML)` 拿 `png_base64`（过大自动缩到 ≤1600px）→ `html_page_review(评审 prompt + 截图)` 拿评审 → 解析成 `page_xxx.review.md`（`mode=screenshot` / `is_ok` / `score` / `needs_rewrite` / `issues` / `suggestion`）。随后对 `needs_rewrite=true` 且有 `issues` 的页，把「当前 HTML 全文 + issues」追加进生成 prompt 调 `html_page_generate` 重出整页修正版 → 覆盖 `output_html_path` → 跑 `lint_pages.py` 兜结构。

2. **读 stdout 的一行 JSON**：`{"status":"ok","reviewed":[...],"needs_rewrite":[...],"rewritten":[...],"rewrite_failed":[...],...}`。据此汇报，不要把 `details` 全文、review 正文或 HTML 贴进返回。

3. 端点/重试/host-pin 由脚本内 `_tool_call.py` 管。subagent **不要**自己调 API、不要自己截图、不要自己改 HTML。

## 🚫 行为红线

- **只跑一遍**：review + rewrite 一次过，不对同一页发起二次评审或二次 rewrite。
- **禁止自写截图/渲染脚本**：截图统一由 `html_to_png` API 出，不得用 `execute_code` 写 playwright/puppeteer/pyppeteer/selenium/html2image/Pillow 拼图等。
- **禁止绕过脚本**：不得用文件写入工具手改 HTML、不得自写 `requests.post` 调三个 API。
- **收敛产物**：本阶段只新增/覆盖 `page_xxx.review.md` / `page_xxx.png` / `page_xxx.html`，不写其它文件。

## subagent 返回消息格式

最终返回**只**四行，总字符数不超过 200，禁止贴 review/HTML/JSON 正文：

```
完成状态: 成功/失败
产物路径: <deck_dir>/htmls/
自检结果: reviewed=N/总页数 rewritten=<页码或无> rewrite_failed=<页码或无>
未解决项: 无 / <rewrite 失败页码 + 简要原因>
```

**画布开关**：若生成阶段用了 `--canvas 1600x900`，本阶段命令也必须带同样的 `--canvas`（rewrite 的 prompt 注入与 lint 校验需一致）。注意：`html_to_png` 暂无视口参数，1600 模式下评审截图可能按默认视口缩放，评审结论仅供参考。
