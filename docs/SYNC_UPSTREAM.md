# 同步上游与单提交合入指南（scripts/sync-upstream.sh）

本文档介绍如何使用 `scripts/sync-upstream.sh` 同步上游仓库代码，或将上游的“某一个提交”安全地合入当前分支。

- 脚本路径：`scripts/sync-upstream.sh`
- 默认上游：`https://github.com/NoFxAiOS/nofx.git`（远端名：`upstream`）
- 适用场景：
  - 日常将 `upstream/main` 或 `upstream/dev` 同步到本地分支
  - 精确引入上游某次修复（指定 commit SHA），避免整批合并带来风险

---

## 基本用法

- 不带参数（与现有流程一致）：同步上游分支
  - 根据当前分支自动选择目标：
    - 当前是 `dev` → 同步 `upstream/dev`
    - 当前是 `main` → 同步 `upstream/main`
    - 其他分支 → 交互选择或默认 `upstream/dev`
  - 交互选择 `merge`/`rebase`，并展示预览与冲突处理建议

```bash
bash scripts/sync-upstream.sh
```

- 指定“上游某个提交”合入（默认 cherry-pick）

```bash
bash scripts/sync-upstream.sh --commit 2cd4760066cf114818ba8ca4a4d61c2b4ee01b2d
```

> 适合热修复：线性历史、冲突范围小；脚本会先尝试 fetch 该 SHA。

- 指定“合并（merge）”方式合入单提交（保留合并关系）

```bash
bash scripts/sync-upstream.sh --commit 2cd4760 --mode merge
```

---

## 进阶参数

- `--commit, -c <sha>`：指定要合入的上游提交 SHA（默认执行 cherry-pick）
- `--mode, -m <cherry|merge>`：单提交合入方式（默认 `cherry`）
- `--remote, -r <name>`：上游远端名（默认 `upstream`）
- `--branch, -b <name>`：参考分支名（用于 fetch 与预览，默认按当前分支推断）
- `--no-confirm`：非交互、跳过确认提示
- `--force`：忽略未提交更改直接继续（谨慎）
- `--dry-run`：仅展示将执行的步骤，不改动任何历史
- `--help, -h`：显示帮助

示例：自定义远端与分支（并静默执行）

```bash
bash scripts/sync-upstream.sh -c 2cd4760 -r upstream -b main --no-confirm
```

---

## 常见场景示例

- 同步 `upstream/dev` 到当前功能分支（默认 merge）：
```bash
bash scripts/sync-upstream.sh
```

- 精确引入一次主干修复（推荐 cherry-pick）：
```bash
bash scripts/sync-upstream.sh --commit 2cd4760066cf114818ba8ca4a4d61c2b4ee01b2d
```

- 预览而不实际执行：
```bash
bash scripts/sync-upstream.sh --commit 2cd4760 --dry-run
```

- 有未提交修改、仍要继续（慎用）：
```bash
bash scripts/sync-upstream.sh --commit 2cd4760 --force --no-confirm
```

---

## 冲突处理

- `cherry-pick` 发生冲突：
  - 解决冲突后：`git add . && git cherry-pick --continue`
  - 放弃：`git cherry-pick --abort`
- `merge` 发生冲突：
  - 解决冲突后：`git add . && git commit`
  - 放弃：`git merge --abort`

> 建议在干净工作区执行；脚本会提示未提交更改并提供确认。

---

## 回退与自救

- 单提交合入回退：
  - cherry-pick 回退：`git revert <pick-commit>` 或 `git reset --hard <之前的SHA>`
  - merge 回退：`git revert -m 1 <merge-commit>`（保留历史）
- 最稳妥的做法：先创建救援分支备份当前现场
```bash
git switch -c rescue/$(date +%Y%m%d_%H%M%S)
```

---

## 注意事项与建议

- 默认远端名为 `upstream`，URL 默认为官方仓库；脚本会自动添加/校正。
- `merge` 会产生合并提交，历史分叉更清晰；`cherry-pick` 线性历史、更利于热修复。
- 指定多个提交的需求可以多次执行 `--commit`，或后续扩展脚本支持范围语法；当前版本专注于“单提交”与“分支同步”。

如需扩展：支持多个 `-c`、或 A..B 范围拣选、以及自动检测与跳过已存在的提交，欢迎提 Issue。

