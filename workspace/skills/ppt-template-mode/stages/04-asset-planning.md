# 阶段 4：资产规划

本文件是 asset-planning subagent 的执行说明。asset-planning subagent 收到主 agent 传入的输入包后，必须先读取本文件。

## 输入

- `outline.json`
- `task_pack.json`
- deck 工作目录
- `prompts/background_image_prompt.md`
- `prompts/illustration_image_prompt.md`
- 默认工具名说明：搜索使用 `web_search`，搜图使用 `image_search`，从网址获取内容和文件使用 `fetch_url`，生图使用 `image_generate`。**图片的下载与检查不再由模型逐张做**，而是由本阶段脚本 `scripts/image_pipeline.py` 并发完成（并发下载 + 4 条代码级硬检查 + 调 `image_filter` API 做水印/语义/清晰度视觉检查），详见下文「图片获取统一流程」。实际运行时以当前可用工具为准：某能力不存在时按降级路径处理；除 `image_pipeline.py` 之外，严禁用 `execute_code` / `fetch_url` / VQA 等伪造或拼接图片。

`outline.json` 是用户确认或修改后的大纲。`task_pack.json` 提供 deck 工作目录、主题、页数、使用场合、演讲者身份、听众身份、语言、约束和交付要求。

### outline.json 消费说明

- `outline.pages[i].content` 是 dict，key 命名 `sub_point1` / `sub_point2` …；遍历时按 key 字典序或数值序迭代 `content.values()`。
- **图片需求**直接体现在大纲里，**没有 needed_pictures 数组**：块级配图 = `sub_point.picture` 非空（值是一个 `picture-NNN`）。把所有非空的 `sub_point.picture` 收集起来，就是本页要落地的全部图槽。
- 每个 sub_point 保留 `slot_id` 扩展字段，必须来自对应 `template_map.pages[i].layout_slots`（如 `left_image`）。它是**模板布局槽**，承担「图最终填到模板哪个 DOM 槽」的职责，由 stage 5 直接从 outline 读取——**不要**把它写进 `asset_map`。
- `image_asset.slot_id` **直接取 `sub_point.picture` 上的 `picture-NNN`**（连字符 + 三位递增数字，全 deck 唯一），与该 sub_point 一一对应——**不是**模板布局槽。stage 5 正是靠 `image_asset.slot_id`(`picture-NNN`) → outline `sub_point.picture` → `sub_point.slot_id`(模板槽) 这条链把真图填进模板的 `fallback.png` 占位槽。**禁止**把模板布局槽（如 `left_image`）写进 `image_asset.slot_id`，否则 stage 5 桥接失败、HTML 回落 `fallback.png`。
- `image_asset.asset_kind`：模板图槽的配图统一记 `illustration`（模板背景由模板自带，本阶段不产背景图）。
- **既有图复用**：若某 `sub_point.picture_source_ref` 非空，说明这个 `picture-NNN` 复用 `info_pack.available_images[].image_id` 等于该值的既有图——**不搜不生**，直接走下文「既有图复用分支」。`picture_source_ref` 空的才进搜图/生图。
- 搜索关键词由本阶段从 `sub_point.sub_point_name` + `text` 自行合成（见「搜索关键词合成」），把入选图写入 `image_assets`。

## 输出

- `asset_map.json`

`asset_map.json` 记录内容补充搜索得到的可用结论，以及最终可用的本地图片资源。它是后续 HTML 生成阶段消费的资产索引。
`asset_map.json` 只记录真实可读的本地资源地址；搜索、下载、检查或生图过程中的失败结果和候选中间结果不写入。
必须写入输入包指定的 `asset_map.json` 输出文件地址，不得只在聊天消息中返回。

## 字段定义

```python
from typing import Literal

ImageAssetSource = Literal["user_provided", "image_search", "image_generate"]


class AssetMap:
    schema_version: str
    deck_dir: str
    pages: list["PageAssetMap"]


class PageAssetMap:
    page_id: str
    page_number: int      # 必填整数——下游切片靠它对齐 outline 页，缺了直接报错
    content_supplements: list["ContentSupplement"]
    image_assets: list["ImageAsset"]


class ContentSupplement:
    query: str
    result_summary: str
    source_local_paths: list[str]
    reason: str


class ImageAsset:
    asset_id: str
    slot_id: str
    asset_kind: str
    caption: str
    source: ImageAssetSource
    local_path: str
    reason: str
```

字段含义：

- `content_supplements`：为了补足大纲缺失内容而做的简单搜索结论；只有存在可用结论和可读本地来源时才记录。
- `source_local_paths`：内容补充所依据的本地文件地址列表，必须是真实可读的本地路径。
- `image_assets`：每个**成功获取到本地真实可用图片**的槽位记录。搜图和生图都没拿到可用图的槽位**不写入** `image_assets`；`image_assets` 里每一条都必须指向一张真实可读的本地图片。
- `asset_kind`：固定为 `background_image` 或 `illustration`。背景图用于页面背景；示意图用于页面主体内容说明。
- `source`：资产来源，取值为 `user_provided`、`image_search` 或 `image_generate`。没有 `placeholder` 这种取值——如果某槽位搜不到也生不出，**直接不在 `image_assets` 里写该槽位**，不保留空路径条目。
- `local_path`：图片本地文件地址，必须真实可读；必须是 `{deck_dir}/assets/<filename>` 形式的沙盒绝对路径，不能写入网络地址、临时说明、占位文本或 deck 之外的共享下载目录。不允许空字符串或 `null`。
- `reason`：一句话说明该图入选/来源（例如 image_pipeline 的检查结论、生图说明）；不入选的图槽整条不写入，本字段不承载检查记录。

## 处理要求

- 在发起任何图片搜索或生图之前，必须先用 `execute_shell_command` 执行 `mkdir -p {deck_dir}/assets`，确保 deck 资产目录存在；`{deck_dir}` 取自 `task_pack.json`。
- 本阶段产出的所有图片文件（搜索下载、生图产物、用户自备图片）都必须落入 `{deck_dir}/assets` 目录下，不得写入 `/mnt/data/downloads/images` 或任何 deck 之外的共享下载目录。
- **搜图候选的检查统一走脚本**：`image_search` 怎么拿候选随意（`download=true` 下到本地、或 `download=false` 取 URL 都行），但**候选的检查必须交给 `scripts/image_pipeline.py` 并发完成**（并发默认 16，见下文「图片获取统一流程」），不再由模型逐张看图判断。**每槽 `top_k=5`**——`image_pipeline.py` 每槽只检查前 5 张候选（并发够大不会更慢）；给够候选是为了**一轮**就选中、不用补搜。

### 入选图片的命名规则

候选的下载/检查由 `scripts/image_pipeline.py` 在临时子目录里并发完成，**不在 `assets/` 留候选池**；只有入选图才落到 `assets/`。进入 `asset_map.json` 的图片必须用规范文件名，以便 HTML 阶段稳定引用：

- 规范文件名：`<asset_id>.<ext>`，例如 `page_003_ill_02.jpg`、`page_001_bg_01.png`。`asset_id` 本身已含 `page_xxx` 前缀。
- 搜图入选：`image_pipeline.py` 已经把入选图按规范名落到 `{deck_dir}/assets/<asset_id>.<ext>`，`local_path` 直接取脚本返回的 `results[].local_path`，subagent 无需自己 `cp`/`mv`。
- 生图入选：调用 `image_generate` 时直接把输出路径指定为 `{deck_dir}/assets/<asset_id>.<ext>`；若工具不支持指定输出路径，落盘后 `mv`/`cp` 改名到规范路径。生图前先确认可用工具里有生图能力，才调用生图。
- 用户自备图入选：`cp` 进 `assets/` 时就用 `<asset_id>.<ext>` 作为目标文件名。
- 同一 `asset_id` 多次产出时新产物覆盖旧文件，不留 `<asset_id>_v2.jpg` 等变体。
- `asset_map.json` 里每条 `image_asset.local_path` 必须是 `{deck_dir}/assets/<asset_id>.<ext>` 这种严格匹配命名规则的沙盒绝对路径。
- 根据大纲现有内容和 `task_pack.json` 的需求，判断是否需要做简单搜索来补充大纲缺失内容。
- 只有当 `outline.json` 中的信息不足以支撑页面表达、且缺口能通过轻量搜索补足时，才执行内容补充搜索。
- 内容补充搜索默认调用 `web_search` 完成，搜索结果写入 `asset_map.json`，不能只把搜索结论写进聊天消息。
- 当搜索结果需要读取网页内容、下载文件或把外部图片地址转换为本地可读文件时，默认调用 `fetch_url`。
- 内容补充搜索必须记录可用结论和本地来源文件；无法得到本地可读来源时丢弃该条补充结果，不写入 `asset_map.json`。
- 内容补充搜索不得改写 `outline.json`；如果需要修改大纲，只在 `content_supplements` 中记录建议和来源。
- 判断是否需要进行图片处理；只有 `outline.json` 中存在图片槽位、既有图片引用不足，或 `task_pack.json` 明确要求图片资产时，才进入图片获取流程。

### 固定背景页禁止规划背景图资产

若某页 `template_map.pages[i].template_constraints.has_background_image == false`，该页 `image_assets` 中**禁止**出现 `asset_kind == "background_image"` 的条目，无论 `visual_requirements` 里是否写了 `"background_image"`、无论 `usage` / `layout.custom_description` 怎么表述。

典型页：`template_raw_constraints.usage` 或 `template_raw_constraints.layout.custom_description` 含「固定背景图」「不需要额外添加」字样的封面、ending 页。这类页面的 `image_assets` 应为空数组（除非有非背景图的 `illustration` / `icon` 槽位存在）。


### 搜索关键词合成

大纲不再提供 purpose 主题标签。每个**需要新搜图**的槽位（`picture_source_ref` 为空的那些）在调用 `image_search` 前，必须自己从 outline 内文本合成一组实际搜索关键词。

合成输入（按权重）：

- **主词源**：所属 `sub_point.sub_point_name`。
- **消歧上下文**：`outline.pages[i].title` / `outline.title` / `sub_point.text`。`text` 含具体数据时仅作语境理解，不直接作为关键词词源。

形态约束（合成后的搜索查询应满足）：

- 短、通用，整体长度控制在 5-15 字（中文）或 3-6 个英文词。
- **不要**含具体数字 / 年份 / 统计数据 / 人物姓名 / 内部缩写 / 技术黑话 / 抽象隐喻 / 晦涩概念。
- 必须是 Google / Bing / 主流图库索引能命中的自然语言。

类型差异：

- 模板图槽的配图（`asset_kind=illustration`）：偏具体实体或场景（例如"团队会议 协作"、"数据中心 服务器"），与正文 sub_point 共用版面、需要语义对得上。

多图同页要求：本页多个待搜图槽（多个非空 `sub_point.picture`）必须合成出**显著不同**的搜索查询，避免搜图引擎返回同一张图。

第一轮关键词命中失败（按"图片质量检查标准"被全部毙掉，或返回 0 条）时，允许换一次关键词重搜——换的方向是调整词序、补语境、或换近义短语，而不是把主题整段换成另一个题材。

### 图片获取统一流程

**🚫 搜图硬红线（本阶段最高优先级，先读）**：搜图候选的**检查**必须经 `scripts/image_pipeline.py`——这是唯一的图片检查入口。**怎么拿候选随你**（这步不卡）：`image_search` 可以 `download=true`（连 `download_dir` 一起把候选下到本地，例如 `{deck_dir}/cand_images/`），也可以 `download=false` 只取 URL——**两种都行**，因为 `image_pipeline.py` 的候选既吃本地文件路径、也吃 URL。

固定两步：

1. 对每个搜图槽位调 `image_search` 拿到候选（本地文件路径列表 或 `results[].image_url` URL 列表，二选一）。
2. 写一份**极简** `{deck_dir}/assets/_candidates.json`（`{"picture-NNN": [候选 URL 或本地路径, ...]}`，只写槽→候选）→ 跑 `python scripts/build_pipeline_spec.py --outline <outline.json> --candidates {deck_dir}/assets/_candidates.json --check-prompt prompts/image_filter_check_prompt.md --deck {deck_dir} --out {deck_dir}/assets/_pipeline_spec.json --concurrency 16` 自动拼 spec（**别手写整个 spec**）→ 跑 `python scripts/image_pipeline.py --spec {deck_dir}/assets/_pipeline_spec.json --concurrency 16`，脚本并发跑 4 条代码级硬检查 + `image_filter` 视觉检查（调用失败≠不通过：语义拒绝必死；调用失败的候选仅在该槽无视觉确认通过候选时按序兜底，不打空槽不补搜）+ 挑图落规范名，读它的 `results` 写 asset_map。

**严禁**（出现任一即视为本阶段未执行、违反本 Skill）：

- **严禁**用 `execute_code` / 凭文件名 / 凭肉眼**手动挑图**后直接写进 `asset_map.json`——挑图必须由 `image_pipeline.py` 根据检查结果做。
- **严禁**不经过 `image_pipeline.py` 就把任何**搜图来源**的图写进 `asset_map.json`。

搜图槽位数 > 0 时，subagent 的执行轨迹里**必须**出现一次真实的 `python scripts/image_pipeline.py --spec ...` 调用；没有这条调用就是没做本阶段的活，必须回去补。（生图 `image_generate`、既有图复用的 `cp` 是另两条独立路径，不受此红线约束。）

一次性检测工具可用性。subagent 在进入任何图片槽位处理前，先根据当前会话实际可用的工具列表记录两个布尔值：

- `has_search`：可用工具里是否存在搜图能力（`image_search`）。
- `has_generate`：可用工具里是否存在生图能力（`image_generate`）。

每一个图片槽位都按下面的路由处理，不得自行跳步、不得一次批量规划后直接产出 `asset_map.json`：

0. **既有图复用分支（最先判定）**：若该槽位对应的 `sub_point.picture_source_ref` 非空，说明它复用 `info_pack.available_images` 里 `image_id` 等于该值的既有图——**不搜不生**：
   - 在 `info_pack.available_images` 中按 `image_id == picture_source_ref` 找到那张图，取其 `local_path`。
   - 用 `execute_shell_command` 的 `cp` 把该文件按"入选图片的命名规则"复制成 `{deck_dir}/assets/<asset_id>.<ext>`。
   - 写入 `asset_map.json`：`slot_id` = 该 sub_point 的 `picture-NNN`，`source="user_provided"`，`local_path` = 复制后的规范路径，`asset_kind="illustration"`。该槽位处理结束，不进下面的搜图/生图。
   - 若 `image_id` 在 `available_images` 里找不到、或 `local_path` 不可读：当作 `picture_source_ref` 为空，降级走下面的搜图/生图。
1. **优先级判定**（仅对 `picture_source_ref` 为空的槽位）：除"收尾页的背景图"这一个例外之外，**所有**图片槽位（封面/正文背景图、正文示意图、双栏配图、卡片图等）都优先走搜图。
   - 例外：结束页（`page_role=ending`）的背景图优先走生图。
2. **搜图分支（批量并发，默认走这条，除例外）**：搜图不再逐槽顺序下载+逐张看图，而是「先取候选 URL，再一次性并发下载检查」：
   - 若 `has_search=true`：
     1. **取候选**：对每个走搜图的槽位调用一次 `image_search`（`download=true` 把候选下到 `{deck_dir}/cand_images/` 后取本地文件路径，或 `download=false` 取 `results[].image_url`——都行）。query 按「搜索关键词合成」从 outline 文本合成，多图同页要合成显著不同的 query。该槽候选 = 这些本地路径或 URL。
     2. **写极简候选 + 脚本拼 spec（别手写整个 spec）**：把每个搜图槽的候选汇成 `{deck_dir}/assets/_candidates.json`（`{"picture-NNN": [候选 URL 或本地路径, ...]}`，只写槽→候选）；然后跑 `python scripts/build_pipeline_spec.py --outline <outline.json> --candidates {deck_dir}/assets/_candidates.json --check-prompt prompts/image_filter_check_prompt.md --deck {deck_dir} --out {deck_dir}/assets/_pipeline_spec.json --concurrency 16`，脚本自动从 outline 枚举图槽、命名 `asset_id`、推导 `slot_desc`、生成每槽 `check_prompt`、组装完整 spec。
     3. **一次并发跑检查**：`python scripts/image_pipeline.py --spec {deck_dir}/assets/_pipeline_spec.json --concurrency 16`，脚本并发下载+4 条代码级硬检查+`image_filter` 视觉检查，给每槽选出第一张全过的图落到 `{deck_dir}/assets/<asset_id>.<ext>`（跨槽 sha 去重），stdout 一行 JSON `{"status":"ok","results":[{asset_id,slot_id,...,local_path: 路径|null, reason}]}`。
     4. **读结果写 asset_map**：`local_path` 非空的写入 `image_assets`、`source="image_search"`、`local_path` 取脚本路径；`local_path=null` 的槽位进入"生图分支"。
   - 若 `has_search=false`：所有槽位直接进入"生图分支"。
3. **生图分支**：
   - 若 `has_generate=true`：按 `asset_kind` 选 prompt 模板（`background_image` 用 `prompts/background_image_prompt.md`，`illustration` 用 `prompts/illustration_image_prompt.md`），调用 `image_generate`，输出文件落到 `{deck_dir}/assets`，写入 `asset_map.json`，`source="image_generate"`，该槽位处理结束。生图失败或工具不可用时进入"跳过分支"。
   - 若 `has_generate=false`：直接进入"跳过分支"。
4. **跳过分支（合法终态，不是失败）**：
   - 该槽位**直接跳过**——不在 `asset_map.json.image_assets` 里写任何条目。
   - **禁止**写 `local_path=""` / `url=""` 的空条目、`"图片待补"`字样、占位图，也禁止伪造图片。
   - 本页其他槽位和其他页的规划**继续正常进行**；该槽位跳过只是该槽位的局部决策。
5. **"结束页背景图"的例外分支**：
   - 若 `has_generate=true`：直接走生图（不走搜图）。
   - 若 `has_generate=false` 且 `has_search=true`：降级到搜图，按"搜图分支"处理。
   - 若两者都没有：进入"跳过分支"。

### 每槽位尝试次数的硬上限

搜图候选的下载和检查已经收敛到「每槽位一次 `image_search(download=false)` + 整批一次 `image_pipeline.py`」，不存在反复换关键词把 context 耗尽的风险。硬上限：

- **`image_search` 每槽位最多 2 次调用**（首轮 0 命中时允许换 1 次近义关键词重取 URL），但整批**只跑一次** `image_pipeline.py`。
- **`image_generate` 每槽位最多 2 次调用**（允许一次失败后换 prompt 再生一次）。

达到上限后该槽位立即进入跳过分支。视觉检查由脚本调 `image_filter` 完成，subagent **不再**自己调 `image_vqa` 逐张看图。

### `asset_map.json` 必须写出（即使所有页全跳过）

`asset_map.json` 是**强制产物**，即使全部槽位都进了跳过分支，subagent 也**必须**写出一份合法 `asset_map.json`——这种情况下每页 `image_assets` 为空数组即可，整份文件仍然合法。asset_map 可以是"空的但完整"的，这是合法终态，**不是**失败状态。

判断阶段 4 完成的唯一标准是 `asset_map.json` 已写盘，而不是"找齐了所有图"。

subagent **禁止**在"图片不够好"时拒绝写 `asset_map.json`、也禁止因此把最终返回写成"未完成"——写一份全空的 `asset_map.json` 并返回"完成状态: 成功 / 未解决项: 某页图片均进入跳过分支"，是正确做法。

### 图片质量检查标准

图片检查统一由 `scripts/image_pipeline.py` 并发完成，**务实口径**（筛掉明显不可用的图，不是挑完美图）写在 `prompts/image_filter_check_prompt.md` 里、随 `check_prompt` 送给 `image_filter`：硬性只认明显商业水印（图精灵/摄图网/zcool/veer 等大面积覆盖字）、明显大段印刷文字（书法字/印章/对联少字不算）、分辨率过低导致渲染模糊、语义完全跑偏；构图/风格/细节等软性瑕疵不阻断入选。代码级 4 条硬检查（≥8KB / ≥200×200 / 像素多样性 / 跨槽 sha 去重）也在脚本里。subagent **不再**自己调 `image_vqa` 看图，也不得绕过脚本自己判"通过"。

其他硬约束：

- 只有 `image_pipeline.py` 返回 `local_path` 非空的图才写入 `asset_map.json`；脚本判 null 的槽位不得强行写入。
- 除 `image_pipeline.py`（下载检查）和 `image_generate`（生图）外，严禁用 `execute_code` / `fetch_url` / VQA 等伪造或拼接图片，也不得把远程 URL / 占位图 / 中间步骤写进 `local_path`。
- **assets/ 规范化不再做任何清理 / `rm` 操作**：`_pipeline_spec.json`、`_candidates.json` 等中间产物原样保留供排查，也不再校验 assets/ 是否只剩规范图片。只需保证入 asset_map 的图按 `<asset_id>.<ext>` 规范命名。
- 某页资产失败不阻塞其他页；失败槽位直接不写入 `image_assets`，不写空路径条目。
- 本阶段输出 `asset_map.json`，不直接改 HTML；完成写入后中止，等待主 agent 进入后续阶段。
- **context 压力兜底**：批量流程把检查都收敛进脚本后 context 压力已大幅下降；万一仍接近上限，立即用已拿到的图（可能为空）写出一份合法 `asset_map.json`，未解决项里标明跳过的槽位，正常 finalize。**绝不**因 context 紧张就不写 `asset_map.json` 或把未完成信息堆进聊天消息。
