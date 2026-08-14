# （智能体工作目录）
本目录是智能体执行任务时的工作根目录（WORKSPACE_DIR）。它位于项目根目录（repoRoot）之下，项目根由运行时自动定位，不要假设固定的磁盘绝对路径。

## 目录用途
- mnt/data/upload/  → 用户上传的原始文件存放处（只读）
- mnt/data/result/  → 你的最终产出物输出位置（写入）
- sk_user/          → 与用户交互的问答记录（自动维护）
- odos/             → 任务清单（自动维护）
- skills/           → 跳转提示：技能统一存放在上一级 repoRoot/skills/（见 skills/README.md）

## 路径使用规则
- ✅ 所有读写路径使用相对路径，以 `./` 开头
- ✅ 写入结果：`./mnt/data/result/xxx.md`
- ✅ 读取上传：`./mnt/data/upload/xxx.csv`
- ❌ 不要使用绝对路径（如 D:/、/home/ 等）

## 路径解析规则（以代码为准）
工具入参的**相对路径一律以本工作区（workspace/）为基准**：
- 裸路径：`mnt/data/result/xxx.md` → workspace/mnt/data/result/xxx.md
- `local://` 同理：`local://mnt/data/upload/file.txt` → workspace/mnt/data/upload/file.txt
- 绝对路径：`local://D:/...` 或 `D:/...` 原样使用
- ⚠️ 不要写 `local://workspace/...`（会解析成 workspace/workspace/...）

## 重要提醒
- 所有任务产出物必须写入 `./mnt/data/result/` 目录
- 不要修改 `sk_user/`、`odos/` 目录下的文件
- 路径分隔符统一使用 `/`（即使 Windows 环境）