# 阶段 4：资产规划

本文件是 asset-planning subagent 的执行说明。收到主 agent 传入的输入包后先读完本文件。

## 输入

- `outline.json`、`style_spec.json`、`task_pack.json`、deck 工作目录
- `prompts/background_image_prompt.md`、`prompts/illustration_image_prompt.md`、`prompts/image_filter_check_prompt.md`
- 工具：搜图 `image_search`、补内容 `web_search`/`fetch_url`、生图 `image_generate`。**图片的下载与检查不由模型逐张做**，统一由脚本 `scripts/image_pipeline.py` 并发完成（下载 + 4 条代码级硬检查 + 调 `image_filter` 视觉检查）。除 `image_pipeline.py` / `image_generate` 外，严禁用 `execute_code` / `fetch_url` / VQA 伪造或拼接图片。

### outline.json 消费说明

- `outline.pages[i].content` 是 dict（key `sub_point1`/`sub_point2`…），按 key 数值序遍历 `content.values()`。
- **图片需求直接在大纲里，没有 needed_pictures**：块级配图 = `sub_point.picture` 非空（一个 `picture-NNN`）；整页底图 = `outline.pages[i].background_picture` 非空（也是 `picture-NNN`）。这两处所有非空 `picture-NNN` = 本页全部图槽。
- `image_asset.slot_id` **直接取该 `picture-NNN`**，一一对应，禁止改格式。`asset_kind`：来自 `sub_point.picture` 记 `illustration`，来自 `background_picture` 记 `background_image`。
- **既有图复用**：`sub_point.picture_source_ref` 非空 = 该 `picture-NNN` 复用 `info_pack.available_images[].image_id` 等于该值的既有图，走「复用分支」不搜不生；空的才进搜图/生图。
- 本阶段不消费 `content_budget`。

## 输出

`asset_map.json`：记录内容补充结论 + 最终可用的本地图片资源，是 HTML 阶段消费的资产索引。**只记真实可读的本地图片**；失败/候选中间结果不写；**无 placeholder 占位**——搜+生都失败的槽位直接不写，HTML 阶段按无图降级。必须写入指定输出地址，不得只在聊天里返回。

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
                          # ⚠ outline 的**每一页**都必须有一条 PageAssetMap——没有图槽的页也要写（image_assets 给空数组）；
                          #   缺页会让切片脚本因 page_number 集合不一致直接崩
    content_supplements: list["ContentSupplement"]   # {query, result_summary, source_local_paths, reason}
    image_assets: list["ImageAsset"]

class ImageAsset:
    asset_id: str
    slot_id: str          # = picture-NNN
    asset_kind: str       # background_image | illustration
    caption: str
    source: ImageAssetSource
    local_path: str       # {deck_dir}/assets/<asset_id>.<ext>，真实可读
    reason: str           # 一句话入选/来源说明，不承载检查记录
```

- `content_supplements`：补足大纲缺失内容的简单搜索结论，只有存在可用结论 + 可读本地来源才记。
- `image_assets`：每个槽位**最终成功获取**的本地图记录；搜+生都失败的槽位整条不写（静默丢弃，不留占位）。

## 图片获取统一流程

**🚫 搜图硬红线（最高优先级）**：搜图候选的**检查必须经 `scripts/image_pipeline.py`**——它是唯一图片检查入口。**怎么拿候选随你**：`image_search` 用 `download=true`（下到本地取路径）或 `download=false`（取 URL）都行，脚本两种都吃。**每槽 `top_k=5`**（脚本每槽也只检查前 5 张，并发够大不会更慢；给够候选是为了让**一轮**就选中、不用补搜）。**严禁**用 `execute_code`/凭文件名/凭肉眼**手动挑图**后直接写 `asset_map.json`，**严禁**不经 `image_pipeline.py` 就把搜图来源的图写进 asset_map。搜图槽位 > 0 时执行轨迹里**必须**出现真实的 `python scripts/image_pipeline.py --spec ...` 调用，否则视为未执行本阶段。（生图、既有图复用的 `cp` 是另两条独立路径，不受此红线约束。）

进入前先按当前可用工具记两个布尔值：`has_search`（有 `image_search`）、`has_generate`（有 `image_generate`）。每个图槽按下面路由，不得跳步、不得一次批量规划后直接产 asset_map：

**0. 既有图复用分支（最先判定）**：`sub_point.picture_source_ref` 非空时——在 `info_pack.available_images` 找 `image_id==该值` 的图取 `local_path`，`cp` 成 `{deck_dir}/assets/<asset_id>.<ext>`，写 asset_map（`source="user_provided"`、`asset_kind` 按来源、`slot_id`=该 `picture-NNN`），结束。若找不到或不可读，当作 `picture_source_ref` 为空降级搜图/生图。

**1. 优先级**（仅 `picture_source_ref` 为空的槽位）：除「结束页背景图」外所有图槽优先搜图；结束页（`page_type=ending`）背景图优先生图。

**2. 搜图分支（默认，批量并发）** —— `has_search=true` 时：
1. **取候选**：对每个搜图槽位调一次 `image_search`（download 随意），query 按下文「搜索关键词合成」从 outline 文本合成，多图同页 query 要显著不同；该槽候选 = 返回的本地路径或 URL 列表。
2. **写极简候选映射 + 脚本拼 spec（不要手写整个 spec）**：把每个搜图槽的候选汇成一份**极简** `{deck_dir}/assets/_candidates.json` = `{"picture-NNN": [候选 URL 或本地路径, ...], ...}`（**只写「槽 → 候选列表」，别的什么都不写**）。然后跑：
   ```bash
   python scripts/build_pipeline_spec.py --outline <outline.json 绝对路径> --candidates {deck_dir}/assets/_candidates.json --check-prompt prompts/image_filter_check_prompt.md --deck {deck_dir} --out {deck_dir}/assets/_pipeline_spec.json --concurrency 16
   ```
   脚本会**自动**从 outline 枚举图槽、命名 `asset_id`、推导 `slot_desc`、用模板生成每槽 `check_prompt`、组装出完整 spec（顶层 `{deck_dir, concurrency, slots}`）。**严禁**自己手写那份「90-URL + 18 段长 check_prompt」的大 spec——那是本阶段最慢的纯输出，必须交给脚本。stdout 返回 `{"status":"ok","slot_count":N,"slots":[...],"skipped_no_candidates":[...]}`。
3. **跑一次** `python scripts/image_pipeline.py --spec {deck_dir}/assets/_pipeline_spec.json --concurrency 16`：脚本并发下载 + 4 条代码级硬检查（大小≥8KB / 分辨率≥200×200 / 像素多样性≥8 色桶 / 跨槽 sha256 去重）+ `image_filter` 视觉检查（水印 / 大段印刷字 / 清晰度 / 语义；**调用失败≠不通过**——语义拒绝的候选必死，调用失败的候选仅在该槽没有视觉确认通过的候选时按序兜底入选，reason 记 `vision_unavailable_default_pass`，不打空槽、不触发补搜），每槽选第一张全过的图落规范名，stdout 一行 JSON `{"status":"ok","results":[{asset_id,slot_id,...,local_path: 路径|null, reason}]}`。
4. **读 results 写 asset_map**：`local_path` 非空 → 写入 `image_assets`（`source="image_search"`，`local_path` 取脚本返回值）；`local_path=null` → 进生图分支。
- **🚫 单次闭环（硬规则，本阶段最大的提速点）**：每槽只调一次 `image_search`、**整批只跑一次** `image_pipeline.py`。脚本返回后，`local_path=null` 的失败槽位**直接转生图分支或放弃**（见下文 3/4），**严禁**为了"把每个槽都填满"而补搜失败槽、重拼 spec、再跑一遍 pipeline——失败槽放弃是合法终态，不是问题。
  - **红旗念头**：冒出"还有 N 个槽没填上，我再搜几个补一轮""候选被刷掉太多，换关键词再来一次"——**立即停**，这就是违规;失败槽转生图或放弃即可。给够了 `top_k=5` + 并发 16，一轮就该够用。
  - 唯一例外：第一轮**某槽返回 0 候选**（image_search 没结果，不是被检查刷掉），允许在**首次**拼 spec 前对该槽换一次近义关键词重取候选——但仍**只跑一次** pipeline，不在 pipeline 之后再补。
- `has_search=false` 时所有槽位直接进生图分支。

**3. 生图分支**：`has_generate=true` → 按 `asset_kind` 选 prompt 模板（`background_image`→`prompts/background_image_prompt.md`，`illustration`→`prompts/illustration_image_prompt.md`）调 `image_generate`，输出落 `{deck_dir}/assets/<asset_id>.<ext>`，写 asset_map（`source="image_generate"`）。`has_generate=false` → 该槽**放弃**，不写任何条目（HTML 阶段按无图处理）。

**4. 结束页背景图例外**：`has_generate=true`→生图；否则 `has_search=true`→降级搜图；都没有→放弃。

**「放弃」是合法终态**：`image_assets` 允许为空或比 outline 声明少几条，不是违规。

## 搜索关键词合成

每个**需要新搜图**的槽位（`picture_source_ref` 为空）在 `image_search` 前自己从 outline 文本合成 query：

- **主词源**：块级配图用 `sub_point.sub_point_name`；整页底图用 `outline.pages[i].title` / `outline.title`。**消歧上下文**：页标题 / deck 标题 / `sub_point.text`（含数据时只作语境，不直接当词源）。必要时参考 `style_spec.json` 的整体调性。
- 形态：短、通用，中文 5-15 字 / 英文 3-6 词；**不含**具体数字/年份/统计/人名/缩写/黑话/隐喻/晦涩概念；是主流图库能命中的自然语言。
- 类型：整页底图偏概念氛围（"城市夜景 鸟瞰"）；块级配图偏具体实体场景（"团队会议 协作"），需与 sub_point 语义对上。

## 命名 / 落盘 / 写回自检

- **命名**：入 asset_map 的图文件名一律 `<asset_id>.<ext>`（ext 小写 jpg/jpeg/png/webp）。搜图入选图由 `image_pipeline.py` 自动落规范名，`local_path` 直接取脚本返回值、**无需自己 `mv`**；生图直接把输出路径设为规范名；用户自备图 `cp` 时用规范名。同一 `asset_id` 重试时覆盖旧文件，不留 `_v2`/`_tmp` 残留。
- **`local_path` 非空硬约束**：严禁空串/`null`/占位值。搜图取脚本返回值；生图/复用由 subagent 落盘后取规范路径。搜+生都失败的槽整条不写。写回前 gate：`assert all(a["local_path"] and os.path.exists(a["local_path"]) for a in all_assets)`，失败禁止以"成功"返回。
- **`assets/` 规范化**：**不再做任何清理 / `rm` 操作**。`_pipeline_spec.json`、`_candidates.json` 等中间产物原样保留供排查，也不再校验 assets/ 是否只剩规范图片。只需保证入 asset_map 的图都按 `<asset_id>.<ext>` 规范命名（见上条）。
- **写回前自检**：每条 asset 的 `source` 合法且 `local_path` 真实存在；`has_search=true` 时每个搜图槽都进过那一次批量流程（取过候选并进了 spec，或因 0 候选/全未过检明确落 `local_path=null`）；存在"outline 有槽但既没取候选也没进脚本"即视为未执行，回去补。
- 用户自备图走 `user_provided`：先 `cp` 进 `{deck_dir}/assets/<asset_id>.<ext>` 才算可用。某页失败不阻塞其它页。本阶段只产 `asset_map.json`，不改 HTML。

## 内容补充搜索（可选）

只有 outline 信息不足以支撑表达、且缺口能轻量搜索补足时才做：默认 `web_search`，需要读网页/下文件时用 `fetch_url`，结论 + 本地来源写入 `content_supplements`（拿不到本地来源就丢弃）。不得改写 `outline.json`，建议只记进 `content_supplements`。

完成 `asset_map.json` 写入后中止，等待主 agent 进入后续阶段。
