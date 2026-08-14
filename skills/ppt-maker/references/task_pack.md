# task_pack.json 生成要求

`task_pack.json` 是本 Skill（ppt-maker，公共准备层）的产物，固定本次 PPT 的任务边界、用户约束、模式分发、内容缺口判断和后续链路输入。生成前必须已确认工作目录（`deck_dir`，来自 `ppt_config` 或确认工具）、关键参数和补充信息任务判定。本 Skill 不创建 deck、不调 todolist。

## 目标

- 生成 `${deck_dir}/task_pack.json`，把入口参数、用户 query、模式选择、风格意图、内容缺口与补充任务门控收束为一个稳定任务包，供 `info_pack.json` 和模式 Skill 消费。

## 输入

- 用户 query；工作目录 `deck_dir`（来自 `ppt_config` 或确认工具，不自行生成）；入口参数、模式参数、文件说明、用户文件
- 补充信息任务判定结果

## 输出

- `${deck_dir}/task_pack.json`，固定 JSON object；所有任务共用同一 schema，不得临时增删顶层字段；未知或不适用字段写空字符串 / 空数组 / `null` / `false`，不得省略。

## 固定 Schema

```python
from typing import Literal

PPTMode = Literal["no-template", "template", "creative"]
ModeSkill = Literal["ppt-no-template-mode", "ppt-template-mode", "ppt-creative-mode"]
SupplementalInfoTaskType = Literal["file_parsing", "data_analysis", "deepresearch"]
SupplementalInfoStatus = Literal["reuse_existing", "required", "not_required", "blocked"]
ContentDensityProfile = Literal["analysis-heavy", "balanced", "showcase-light"]
VisualStrategy = Literal["chart_dominant", "image_dominant", "mixed"]


class TemplateParams:
    template_name: str | None
    template_tags_path: str | None
    template_html_dir: str | None
    notes: str


class StyleIntent:
    scenario: str
    audience_signal: str
    tone: list[str]
    industry_context: str
    explicit_style_preference: str | None
    page_style: str | None


class ContentGapAssessment:
    has_gap: bool
    summary: str
    blocking_items: list[str]
    non_blocking_items: list[str]
    affected_pages_or_sections: list[str]


class SupplementalInfoTask:
    task_type: SupplementalInfoTaskType
    status: SupplementalInfoStatus
    reason: str
    scope: list[str]
    priority: str
    reuse_existing_report_id: str | None
    unresolved: list[str]


class LanguageDecision:
    source: Literal["explicit_requirement", "query_fallback"]
    matched_signal: str   # source=explicit_requirement 时必填；query_fallback 时必须为空串
    chosen_language: str   # 必须 == 顶层 language
    note: str


class TaskPack:
    schema_version: str
    deck_dir: str
    user_query: str
    topic: str
    language: str
    language_decision: LanguageDecision
    ppt_mode: PPTMode
    mode: ModeSkill
    page_count_desc: str
    speaker_identity: str
    audience_identity: str
    use_case: str
    template_params: TemplateParams
    page_style: str | None
    style_intent: StyleIntent
    content_density_profile: ContentDensityProfile
    visual_strategy: VisualStrategy
    content_gap_assessment: ContentGapAssessment
    supplemental_info_tasks: list[SupplementalInfoTask]
    constraints: list[str]
```

## 字段规则

- `schema_version` 写当前版本，例如 `ppt_maker_task_pack_v3`。
- `deck_dir` 是工作目录，来自 `ppt_config` 或确认工具返回，**不得自行生成或重拼**（本 Skill 不创建 deck、不调初始化脚本）。
- `topic` 缺失时从用户 query 与文件说明中抽取。
- `language` 判定优先级：
  1. 任何显式「输出语言」要求；
  2. 用户 query 的主自然语言。
  ①与②冲突时（如要求全程某语言、query 为另一语言）以①为准。
  - ①的判定口径（通用）：来自用户指令、系统/角色/人格设定中，任何指定「以某语言输出/回复」「无论输入什么语言都用某语言输出」「全程某语」之类的约束，均视为显式输出语言要求 → `language` 取该指定语言，覆盖 query 主语言。判定看「是否指定了输出语言」，不看该指令出现在哪个输入通道。
  - 轨道区分：本字段只决定 **deck 产物语言**（下游大纲/页面对用户可见文本）；与 assistant 对用户说话的**旁白语言**相互独立。`task_pack.language` 一经按本优先级落定，即为下游所有阶段的唯一语言权威，下游不得另行依据 query 或其它信号回落改写。
- `language_decision` 是 language 判定的**强制留痕**，必须与 `language` 同时产出，作为"判定确实执行过"的证据，不得省略或留空（缺失会被前置校验脚本拦下）：
  - `source`：`explicit_requirement`（命中显式输出语言要求）或 `query_fallback`（全通道扫描后确无任何显式输出语言要求，回落 query 主语言）二选一。
  - `matched_signal`：`source=explicit_requirement` 时必填，引用命中的原话片段并标明出现通道（用户指令 / 系统 / 角色 / 人格设定）；`source=query_fallback` 时必须为空字符串。
  - `chosen_language`：必须与顶层 `language` 完全一致。
  - `note`：一句话判定理由。
  - 填 `query_fallback` 等于主动断言"已扫遍所有输入通道、确无任何显式输出语言要求"——该断言会被生成 agent 在同上下文内的对抗式自查（见 SKILL.md 阶段 4）复核，不得用来掩盖"未做判定就走默认"。
- **已知限制（语言支持范围）**：当前下游大纲与 HTML 渲染只对 `zh` / `en` 及拉丁语系给了明确格式范例与版式适配；**LTR 布局假设**和无模板模式的 `data-display-id` 字符数阈值（按中文/英文字符数估算）对 **RTL 语言（阿拉伯语 `ar` / 希伯来语 `he` 等）和部分 CJK-other 脚本未做适配**，传入这类 `language` 可能出现版式或截断问题。需要支持 RTL / 其它脚本时，应先在渲染链路补 LTR/RTL 与字符阈值适配，再开放对应 `language`。
- `ppt_mode` 取值限 `no-template` / `template` / `creative`；`mode` 由 `ppt_mode` 映射到对应模式 Skill。
- `page_count_desc` 保留自然语言描述（例如"6 页左右"）；不确定时也写描述，不另设整数字段。
- `constraints` 记用户明确给出的硬约束（必须包含/排除的内容、风格/品牌要求等）。

## 关键参数写入映射

| 入口参数 | 写入字段 |
|---|---|
| 任何显式「输出语言」要求（用户指令 / 系统 / 角色 / 人格设定） | `language`（命中时覆盖 query 主语言；口径见「字段规则」language 优先级） |
| `role` | `speaker_identity` |
| `scene` | `use_case` |
| `audience` | `audience_identity` |
| `page_count_desc` | `page_count_desc` |
| `ppt_mode` | `ppt_mode`，同时把 `mode` 映射为 `ppt-no-template-mode` / `ppt-template-mode` / `ppt-creative-mode` |
| `template_name` | `template_params.template_name` |
| `template_html_dir` | `template_params.template_html_dir`（`ppt_mode=template` 时必填） |
| `template_tags_path` | `template_params.template_tags_path`（`ppt_mode=template` 时必填） |
| `page_style` | 顶层 `page_style`，并同步写入 `style_intent.page_style` |

- 区分：`role`→`speaker_identity` 是「演讲者身份」，不是语言信号；不得把输出语言要求误并入 speaker_identity，也不得因 ppt_config 未写语言就判定无语言信号。

## 模板参数规则

- `template_params.template_name` 未明确时写 `null`；非模板模式下 `template_params` 的字段保留存在，不适用写 `null` 或空字符串。
- `template_params.template_html_dir` 为绝对路径，目录内文件名必须是 `<template_id>.html`。
- `template_params.template_tags_path` 为模板配置文件绝对路径；模板模式前必须补齐。

## 风格意图规则

- `style_intent` 至少覆盖 `scenario`、`audience_signal`、`tone`、`industry_context`、`explicit_style_preference`、`page_style`，在本 Skill 一次抽取，不得推迟到后续页面生成时猜测。
- 用户明确给出的风格偏好 / 品牌语气 / 参考图 / 模板方向必须写入，后续阶段不得覆盖；`page_style` 为空时由后续阶段补充，入口已写入后只能消费不能改写。

## 内容承载 profile 规则

- `content_density_profile` 是正文页内容承载策略，不是纯视觉风格：
  - `analysis-heavy`：分析 / 汇报 / 评估类，允许更高的论点、证据和结构承载。
  - `balanced`：普通汇报 / 培训 / 项目介绍，信息量与留白均衡。
  - `showcase-light`：品牌 / 展示 / 活动 / 发布类，强调主视觉和克制文字承载。
- 用户明确要求"更满 / 更克制"时可覆盖默认 profile。
- **默认值**：用户 query 没有明示克制 / 简洁 / 极简偏好时，默认写 `analysis-heavy`（按"默认富填"原则，每页内容靠上限承载，不主动留白）；只有用户明示降档才写 `balanced` 或 `showcase-light`。

## 视觉策略规则（visual_strategy）

- `visual_strategy` 是**视觉素材主轴**，决定本份 deck 以「数据图表」还是「图片」为主要视觉支撑，与 `content_density_profile`（正文文字密度）**正交**：density 管文字多少，visual_strategy 管图表 vs 图片。两者组合，不得互相替代。
- 取值与默认推导（按 topic / use_case / style_intent 判断，用户无明确要求时取默认）：
  - `chart_dominant`：数据 / 财务 / 分析 / 评估 / 复盘 / 研究类，结论依赖统计图表支撑——优先安排 chart / table。
  - `image_dominant`：品牌 / 活动 / 发布 / 展示 / 培训科普 / 文旅类，靠主视觉和示意图撑场——优先安排背景图 / 示意图。
  - `mixed`：上述特征不明显、或图表与图片都需要时的默认折中。
- **判定口径**：用户没有明确视觉要求时，判断该 query 是否需要详细数据图表支撑——需要就 `chart_dominant`，不需要就 `image_dominant`，两可时 `mixed`。
- **硬下限**：**禁止** deck 整体只有正文、既无图表也无图片。`visual_strategy` 不存在 `text_only` 取值；纯文字承载只能通过 `content_density_profile=analysis-heavy` 表达「文字密度高」，不得用 visual_strategy 关掉视觉。
- **默认值**：用户没有明示"以图表为主"或"以图为主"时，默认写 `mixed`，让大纲层在每页按内容性质择一选图表或图片，确保两类视觉都出现在 deck 里。
- 用户明确要求"多放图表 / 多放图 / 少配图"时按用户覆盖默认值。
- visual_strategy 在本 Skill 一次定下，后续模式 Skill 只消费不改写：它驱动本 Skill 第 3 步补充信息任务判定（见 SKILL.md），并供无模板大纲阶段编排图表/图片配比与「每章节至少一处视觉」的软下限使用。

## 内容缺口与补充任务门控

- `content_gap_assessment` 记内容缺口判断（为什么影响内容决策、可能阻塞哪些页）。
- `supplemental_info_tasks` 是文件解析 / 数据分析 / deepresearch 是否执行的门控，必须在本 Skill 分发前的补充信息任务判定阶段定下，不得在 `info_pack.json` 阶段重新决定。
- `supplemental_info_tasks[].status` 取值：
  - `reuse_existing`：半成品报告足以支撑，填 `reuse_existing_report_id`。
  - `required`：半成品不足以支撑，说明缺口 / 范围 / 优先级。
  - `not_required`：不需要补充。
  - `blocked`：确实需要但无法执行，阻塞原因写入该任务的 `unresolved`。

## Deck 目录规则

- `deck_dir` 是本轮任务 canonical 输出根目录；后续所有 Skill 必须复用，不得改写、缩写、翻译或重拼。

## 用户回显

- 开始生成时说明正在整理任务包（固定 deck 目录、页数假设、模式和缺口判断）。
- 完成后回显主题、受众、页数、模式、deck 目录、补充任务判定和关键假设；有依赖假设的边界条件必须显式写出，不静默带过。

## 禁止事项

- 不得省略 `content_gap_assessment`；不得临时增删顶层字段。
- 不得把补充任务的判断推迟到 `info_pack.json` 阶段。
- 不得虚构未提供的事实 / 数据 / 图表结论；不得把产物写到 `deck_dir` 之外。
