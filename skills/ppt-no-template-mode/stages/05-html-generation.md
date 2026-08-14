# 阶段 5：无模板 html 生成（批量并发调 API）

本文件是阶段 5「页面生成」subagent 的执行说明。**本阶段不再每页一个子任务、也不再由模型亲手写 HTML**；改成一个生成子任务跑一个批量脚本，脚本内部并发（默认 4）对所有页：组装 prompt → 调 `html_page_generate` API → 落盘 → lint → 失败重出一次。subagent 收到输入包后先读完本文件。

## 输入

- deck 工作目录绝对路径
- 主 agent 已在阶段 5 前跑过 `scripts/slice_stage5_inputs.py`，每页切片 `htmls/page_xxx.input.json` 已就绪（含 `style_spec` / `outline_page` / `asset_map_page` / `output_html_path`）
- prompt 模板：`prompts/html_gen_no_template.md`（页面格式硬约束都在里面，脚本会把它 + 本页数据拼成 prompt 发给 API）

## 输出

- 每页 `htmls/page_xxx.html`（脚本写盘，三位页码）

## 处理要求

1. **只跑这一个脚本**，从本 skill 所在目录执行：

   ```bash
   python scripts/html_page_generate_batch.py \
     --deck <deck_dir> \
     --mode no_template \
     --prompt prompts/html_gen_no_template.md \
     --concurrency 4
   ```

   命令以上面为准；脚本对参数有容错：`--mode` 省略时按 deck 切片**自动判定** no_template/template，`--gen-prompt` 是 `--prompt` 的兼容别名——但仍**优先照上面原样传**，不要自行改写。

   脚本会：加载所有 `htmls/page_*.input.json`；线程池并发每页 = 组装 prompt（`html_gen_no_template.md` 全文 + 本页 `outline_page`/`style_spec`/`asset_map_page` 的 JSON）→ POST `html_page_generate` 拿 `{html}` → 去掉 markdown 包裹 → 写 `output_html_path` → 跑 `scripts/lint_pages.py` 自检；lint 不过则把问题追加进 prompt **重出一次**，再不过则标记该页失败（HTML 仍保留）。

2. **读 stdout 的一行 JSON**：`{"status":"ok","page_count":N,"ok_pages":[...],"failed_pages":[...],"details":[...]}`。据 `ok_pages` / `failed_pages` 汇报，不要把 `details` 全文或 HTML 贴进返回。

3. 端点与重试由脚本内 `_tool_call.py` 管（env `PPT_TOOL_API_URL` / `PPT_TOOL_API_BASE` 可覆盖，默认 code-stage、内置 host pin；内置 3 次退避）。subagent **不要**自己重试、不要自己调 API。

## 严格禁止

- 不得用 `execute_code` / 文件写入工具**亲手写 HTML**，不得自写 `requests.post` 调 `html_page_generate`，不得绕过脚本。
- 不得读 `style_spec.json` / `outline.json` / `asset_map.json` 全量文件（脚本只吃切片）。
- 不得修改切片、不得改其它产物、不得启动新 subtask。
- 不得对生成结果再发起 review/rewrite（那是阶段 6/7 的事，由主 agent 另行编排）。

## subagent 返回消息格式

最终返回**只**四行，总字符数不超过 200，禁止贴 HTML/JSON/脚本输出：

```
完成状态: 成功/失败
产物路径: <deck_dir>/htmls/
自检结果: ok_pages=N/总页数 failed=<失败页码或无>
未解决项: 无 / <失败页码 + 简要原因>
```

**画布开关**：默认 1280×720（前端契约，命令不带参即可）。仅当任务约束 / 用户明确要求对比 1600×900 时，生成与复核命令**都**加 `--canvas 1600x900`（prompt 注入与 lint 校验自动跟随）。
