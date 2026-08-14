# ppt_batch - PPT 批量自动化（效果 + 成本测算）

无头批量跑参考测试集 cases，统计每次 PPT 生成的耗时与成本。
详细配置参数（max_tokens / summary 阈值 / timeout 等）见主 README `关键参数（团队须知）`。

## 目录结构
- `cases/`   测试集 manifest（来自参考 cases.zip，34 条，25 条纯文本可跑）
- `scripts/` run_batch.py：无头批量 runner
- `runs/<run>/`  每次运行：costs.csv / summary.json / 日志 / sessions（会话+usage）
- `docs/`    进度与对比分析文档

## 数据落盘
- 批量会话 + usage：`ppt_batch/runs/<run>/sessions/<case>/`（runner 直接 spawn，不经前端，不进 frontend/sessions）
- PPT 产物（deck）：`workspace/result/<run>/<case>/`。runner 传给 Skill 的路径是对应的逻辑生产路径 `/mnt/data/result/<run>/<case>/`，由本地运行时映射到上述目录。
- 成本表：`ppt_batch/runs/<run>/costs.csv`

## 批处理用法

### 运行命令
```
# 批量跑（默认筛选纯文本 case，no-template 模式）
node scripts/python.mjs ppt_batch/scripts/run_batch.py --model clotho-qn-claude-opus-47 --limit 3

# 指定模式 / 模型 / case
node scripts/python.mjs ppt_batch/scripts/run_batch.py --mode no-template --model clotho-claude-opus-47 --cases seq_0206,seq_0134

# 只重算成本表（从已有 run 目录）
node scripts/python.mjs ppt_batch/scripts/run_batch.py --collect-only --out ppt_batch_xxx
```

### ask_user 处理（特殊设置）
| 入口 | ask_user 怎么处理 | 机制 |
|---|---|---|
| 批处理（无头） | **agent 侧自动应答** | runner 传 `-AutoAnswerAskUser` → agent 遇到 ask_user 不阻塞，自动注入答案继续（大纲确认→“大纲确认通过，请按当前大纲继续执行后续步骤。”；参数确认→“参数确认通过，请按上述配置继续执行。”；其它→“确认，请继续执行。”）；不改 skill、不砍 LLM 轮次，成本数据与真实交互一致 |
| UI 手动 | **前端 30s 无应答自动选择** | 弹窗倒计时 30s，无操作自动勾选第一项并推进；用户操作即重置；也可手动选择/跳过 |

注意：
- 批量会话不走前端 → ask_user 的“等待”不产生 real_ms，看 elapsed_s
- 部分场景用户确实需要参与选择（如刀剑神域选题材）：批量自动应答会选默认继续，效果不如手动选方向，这类 case 建议 UI 手动评测

## 成本统计口径（重要）
- `llm_call_count` / `prompt_tokens` / `completion_tokens` = **主循环 + 全部子任务**（create_subtask 派生的独立 agent）汇总；CSV 里 `main_llm_calls` 与 `subtask_llm_calls` 分列
- 子任务通常占 60%+ 的调用和 token（大纲/资产规划/每页渲染）
- `real_ms` 只有前端 UI 流程有值；批量无头为 0，真实耗时看 `elapsed_s`（墙钟）。成本看 token，与 real_ms 无关

## 已知缺口 / 待确认
- `html_to_png` 是本地 Chrome/Chromium/Edge 的 headless 实现；机器未安装可用浏览器时会返回 `unavailable`。
- `image_search`、`image_generate`、`document_parser` 是 HTTP 服务型工具。`pnpm gateway:mock` 只能验证协议与 Agent 生命周期，不能替代真实搜索、绘图或附件解析验收；接真实服务前，PPT 可能降级为无图或只保留 HTML。
- 新版 PPT skill 的内部后端调用读取 `tools.pptTools`（目前配置在 `config.clotho.json`）；本地 Shell、Python 和子任务会继承其端点设置。通用 `config.json` 不包含真实内网端点。
- 定价表（$/M token）、老版本基准口径待确认
