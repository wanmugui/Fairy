# 阶段 5：参考模板生成 html（批量并发调 API）

本文件是阶段 5「页面生成」subagent 的执行说明。**本阶段不再每页一个子任务、也不再由模型亲手改写模板 HTML**；改成一个生成子任务跑一个批量脚本，脚本内部并发（默认 4）对所有页：组装 prompt（含整份参考模板 HTML + 照模板生成约束）→ 调 `html_page_generate` API → 落盘 → lint → 失败重出一次。subagent 收到输入包后先读完本文件。

## 输入

- deck 工作目录绝对路径
- 主 agent 已在阶段 5 前跑过 `scripts/slice_stage5_inputs.py`，每页切片 `htmls/page_xxx.input.json` 已就绪（含 `template_map_page` / `outline_page` / `asset_map_page` / `template_html_path` / `output_html_path`）
- prompt 模板：`prompts/html_gen_template.md`（「照模板生成：遵循模板布局·配色·字号体系 + 文字/槽位/fallback.png 填充 + 字号尺寸纪律 + 防假图」的全部硬约束都在里面）

## 输出

- 每页 `htmls/page_xxx.html`（脚本写盘，三位页码）

## 处理要求

1. **只跑这一个脚本**，从本 skill 所在目录执行：

   ```bash
   python scripts/html_page_generate_batch.py \
     --deck <deck_dir> \
     --mode template \
     --prompt prompts/html_gen_template.md \
     --concurrency 4
   ```

   脚本会：加载所有 `htmls/page_*.input.json`；线程池并发每页 = 读 `template_html_path` 的整份模板 HTML，组装 prompt（`html_gen_template.md` 全文 + 本页 `outline_page`/`template_map_page`/`asset_map_page` 的 JSON + 参考模板 HTML 全文）→ POST `html_page_generate` 拿 `{html}` → 去掉 markdown 包裹 → 写 `output_html_path` → 跑 `scripts/lint_page_html.py --output ... --template ...` 自检；lint 不过则把问题追加进 prompt **重出一次**，再不过标记该页失败（HTML 仍保留）。

2. **读 stdout 的一行 JSON**：`{"status":"ok","page_count":N,"ok_pages":[...],"failed_pages":[...],"details":[...]}`。据 `ok_pages` / `failed_pages` 汇报，不要把 `details` 全文或 HTML 贴进返回。

3. 端点与重试由脚本内 `_tool_call.py` 管（env `PPT_TOOL_API_URL` / `PPT_TOOL_API_BASE` 可覆盖，默认 code-stage、内置 host pin；内置 3 次退避）。subagent **不要**自己重试、不要自己调 API。

## 模板保真兜底

模板模式靠「整份模板 HTML 进 prompt + 照模板生成约束 + lint gate + 重出一次」保真。lint（`lint_page_html.py`）会查：固定 `img`（`../htmls_png/*`/`../user/*`）原样保留、`#bg`/`#ct` 未被破坏、模板没 `<script>` 时输出也不得新增、背景声明位置、模板占位文字残留、`fallback.png` 已被填充或删除、**主题模板的主题样式 `<link>`（`../themes/*.css` 等）原样保留、`var(--xxx)` 主题色变量未被硬编码替换、也未被重新定义覆盖**。某页两次都过不了 lint 就进失败名单，不要在本阶段反复重试。

## 严格禁止

- 不得用 `execute_code` / 文件写入工具**亲手改写模板 HTML**，不得自写 `requests.post` 调 `html_page_generate`，不得绕过脚本。
- 不得读 `template_map.json` / `outline.json` / `asset_map.json` 全量文件、不得扫描模板目录（脚本只吃切片 + 切片里 `template_html_path` 指的那一个模板文件）。
- 不得修改切片、不得改其它产物、不得启动新 subtask。

## subagent 返回消息格式

最终返回**只**四行，总字符数不超过 200，禁止贴 HTML/JSON/脚本输出：

```
完成状态: 成功/失败
产物路径: <deck_dir>/htmls/
自检结果: ok_pages=N/总页数 failed=<失败页码或无>
未解决项: 无 / <失败页码 + 简要原因>
```
