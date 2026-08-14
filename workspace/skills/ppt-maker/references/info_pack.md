# info_pack.json 生成要求

`info_pack.json` 用于汇总本次 PPT 可用的全部内容依据和信息缺口。后续模式 Skill 的大纲生成、资产规划和页面 HTML 生成都以 `info_pack.json` 为内容来源。

与 `task_pack.json` 的职责边界：`task_pack.json` 固定任务边界和模式选择，`info_pack.json` 汇总可用内容和缺口。两者不重叠。

## 目标

- 生成 `${deck_dir}/info_pack.json`，汇总用户输入、文件解析结果、数据分析结果、deepresearch 结果和已有半成品报告，标注来源角色，并记录无法覆盖的信息缺口。

## 输入

- `task_pack.json`（已生成）；用户 query 与文件说明；用户文件
- 文件解析 / 数据分析 / deepresearch 结果、已有半成品报告（各项均可选）

## 输出

- `${deck_dir}/info_pack.json`，固定 JSON object；所有任务共用同一 schema，不得临时增删顶层字段；未知或不适用字段写空字符串 / 空数组 / `null` / `false`，不得省略。

## 固定 Schema

```python
from typing import Literal

SourceRole = Literal["content_source", "style_reference", "template", "asset", "background"]
ContentKind = Literal["text", "table", "chart_data", "image", "structured_data", "mixed"]


class InfoPack:
    schema_version: str
    deck_dir: str
    source_files: InfoPackSources
    user_input_summary: UserInputSummary
    content_blocks: list["ContentBlock"]
    available_images: list["AvailableImage"]
    source_roles: list["SourceRoleEntry"]
    evidence_references: list["EvidenceReference"]
    info_gaps: list["InfoGap"]
    unresolved: list[str]


class InfoPackSources:
    task_pack: str


class UserInputSummary:
    original_query: str
    key_points: list[str]
    file_descriptions: list[str]


class ContentBlock:
    block_id: str
    source_type: str
    source_path: str | None
    content_kind: ContentKind
    title: str
    body: str
    key_findings: list[str]
    data_tables: list[str]
    chart_data: list[str]
    applicable_pages: list[str]
    coverage_notes: str


class AvailableImage:
    image_id: str
    source_type: str
    local_path: str
    description: str
    applicable_pages: list[str]
    usage_hint: str


class SourceRoleEntry:
    source_id: str
    file_path: str | None
    role: SourceRole
    description: str


class EvidenceReference:
    claim: str
    source_id: str
    source_path: str | None
    confidence: str


class InfoGap:
    gap_id: str
    description: str
    affected_pages_or_sections: list[str]
    severity: str
    fillable_by: str
```

## 字段规则

- `schema_version` 写当前版本，例如 `ppt_maker_info_pack_v1`。
- `deck_dir` 必须来自 `task_pack.json`，不得重新生成。
- `user_input_summary` 保留用户原始 query 和关键要点，不改写。
- `content_blocks` 是可用内容的逐块记录；每个 block 对应**一个**信息来源（文件解析 / 数据分析 / deepresearch / 半成品报告 / 用户直接输入），不得混入多源；`source_type` 取值 ∈ {`file_parsing` / `data_analysis` / `deepresearch` / `semi_finished_report` / `user_input`}；`applicable_pages` 适用于全局时写 `["all"]`。
- `available_images` 记录**本就存在**的可用本地图片资源——**仅限**用户上传的图片、半成品报告里内嵌的既有图；**禁止**把入口阶段自己 `image_search` 搜来的图写进 `available_images`（入口为配图主动搜图本身就是禁止的，见 SKILL「入口图片硬红线」）。`source_roles` 记录每个来源文件的角色判断；`evidence_references` 记录关键事实出处。
  - `available_images[].image_id` 是既有图的身份标识，**仅供大纲阶段的 `sub_point.picture_source_ref` 引用**（表示"这个图槽复用这张既有图"）；它**不是** `picture-NNN`，**不得**直接写进大纲的 `sub_point.picture` 或最终 HTML——下游图槽 id 一律由大纲铸造的 `picture-NNN` 承载。
- `info_gaps.severity` 取 `blocking` / `non_blocking`；`fillable_by` 取 `light_search` / `user_input` / `deepresearch` / `not_fillable` 等。

## 内容汇总规则

- 补充信息任务的执行决定已在制作流程第 3/4 阶段完成；本阶段只汇总结果，不重新决定是否执行。
- 文件解析 / 数据分析 / deepresearch 若已执行完成，必须逐项写入 `content_blocks`；`task_pack.json.supplemental_info_tasks` 判定为 `reuse_existing` 的半成品报告也写入并标注来源。
- 按 `task_pack.visual_strategy` 决定原料汇总侧重（不改变是否执行的门控，只决定怎么落字段）：
  - `chart_dominant`：来源中可量化的数字 / 时间序列 / 占比 / 对比尽量落实到 `content_blocks[].chart_data`（与 `data_tables` 区分：能画统计图的进 `chart_data`），让大纲有据可画；确实没有数据支撑时把缺口写进 `info_gaps`（`fillable_by` 标 `data_analysis` 或 `not_fillable`）。
  - `image_dominant`：**只有**用户自备图 / 半成品报告内既有图落 `available_images`（**严禁**把入口现搜的图写进来）；预计需要而当前没有的配图，把缺口写进 `info_gaps`（`affected_pages_or_sections` 标明），供下游 stage 4 搜图/生图**并过滤**。入口不得为配图主动 `image_search` / `image_generate`。
- 信息不足写入 `info_gaps`，不能虚构。

## 禁止事项

- 不得虚构未提供的事实 / 数据 / 图表结论。
- 不得重新决定补充信息任务是否执行；不得临时增删顶层字段。
- 不得把产物写到 `deck_dir` 之外。
