# 阶段 4：页面渲染

本文件是 `page-render` subagent 的执行说明。每一页一个独立 subtask，subagent 收到主 agent 传入的输入包后，必须先读取本文件。

## 输入

- `<deck_dir>/pages/page_{NNN}.input.json` 绝对路径
- `page_number`（1-based）
- `output_png_path`

## 输出

- `<deck_dir>/pages/page_{NNN}.png`

## 切片 JSON 字段

`page_xxx.input.json` 的固定字段（所有字段均为本页范围）：

- `schema_version`：切片版本号
- `deck_dir`：deck 工作目录绝对路径
- `page_number`：1-based 整数页码
- `page_id`：稳定页面标识（`page_001`）
- `output_png_path`：本页 PNG 的绝对输出路径
- `ppt_id` / `ppt_title`：API payload 字段（`ppt_title` 取自 outline 顶层 `title`）
- `page_style_md`：`style_spec.md` 全文（内联字符串）
- `page_outline`：API 消费的单页 dict（已按 spec §4.3.1 映射）
  - `template_id` 若 outline 未提供则兜底为 `"default_template"`
  - `page_number` 取自 outline 的 `page_number_label`（字符串，如 "第N页，共M页" / "Page N of M"，按 language）
  - `needed_pictures` 已把新 schema `{id, tag, purpose, size_hint}` 映射为 API 旧格式 `{id, caption, tag, size}`
  - 保留顶层 `title` / `page_type` / `layout` / `content` 透传

## 处理要求

1. 渲染脚本会创建图片文件，所以调用渲染脚本之前需要按照`文件操作预播报协议(File Action Protocol)`进行通知。
2. subagent **只允许**用 `execute_shell_command` 调下列脚本；**禁止**用 `execute_code` 自写 HTTP 请求或绕过脚本直接调 API。
3. 从此skill所在目录执行渲染脚本：

   ```bash
   python scripts/creative_page_render.py \
     --input <input_json_path>
   ```

   脚本内置 3 次重试（指数退避 2s/4s/8s）；subagent **不要**自己重试——脚本非零退出即视为失败。
4. 检查 `output_png_path` 存在且文件大小 > 0：

   ```bash
   test -s <output_png_path>
   ```


## 失败处理

- 渲染脚本非零退出：把 stderr 最后一行作为"未解决项"写进返回消息，**不要**自己发起重试、**不要**改 slice / style / outline 文件、**不要**启动新 subtask。
- `output_png_path` 存在但 size=0：视为失败，同上。
- 切片 JSON 缺字段：脚本会自检并打印 `[creative_page_render] slice missing fields ...`，按上面规则汇报即可。

## 严格禁止

- 不得用 `execute_code` 自写 `requests.post`、自写 base64 解码、自写 PNG 落盘逻辑
- 不得读 `style_spec.md` / `outline.json` 全量文件（切片已内联所需信息）
- 不得修改其他页的切片、PNG 或主 agent 产物
- 不得启动新 subtask
- 不得对生成的 PNG 做图片质量核对（v1 只保证产出，不做 vision 校验）

## subagent 返回消息格式

subagent 在最终返回消息中**只汇报**以下四行，禁止粘贴 base64、HTTP 响应体或文件正文：

```
完成状态: 成功/失败
产物路径: <output_png_path>
自检结果: file_exists=true|false size=<bytes>
未解决项: 无 / <stderr 最后一行 / 具体原因>
```
