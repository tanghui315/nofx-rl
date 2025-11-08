#!/bin/bash

# 🔄 Sync Upstream Script
# 同步上游仓库 (https://github.com/NoFxAiOS/nofx.git) 的最新代码到你的 fork
# 这个脚本会自动添加 upstream 远程仓库（如果不存在），并同步最新代码

set -e

# Args (defaults)
COMMIT_SHA=""
MODE="cherry"      # cherry|merge
REMOTE="upstream"
BRANCH=""          # optional; when empty, infer from current branch
NO_CONFIRM=0
FORCE=0
DRY_RUN=0

usage() {
  cat <<EOF
Usage:
  $(basename "$0")                    # 同步指定分支(默认和当前分支匹配)
  $(basename "$0") --commit <sha>     # 合入上游的某个提交(默认 cherry-pick)

Options:
  --commit, -c   <sha>   上游提交ID
  --mode,   -m   <cherry|merge>  方式(默认 cherry)
  --remote, -r   <name>  上游远程(默认 upstream)
  --branch, -b   <name>  参考分支(用于 fetch 与预览)
  --no-confirm          跳过交互确认
  --force               忽略未提交更改
  --dry-run             仅展示将执行的步骤
  --help, -h            显示帮助
EOF
}

# Parse args
while [[ $# -gt 0 ]]; do
  case "$1" in
    --commit|-c) COMMIT_SHA="$2"; shift 2;;
    --mode|-m)   MODE="$2"; shift 2;;
    --remote|-r) REMOTE="$2"; shift 2;;
    --branch|-b) BRANCH="$2"; shift 2;;
    --no-confirm) NO_CONFIRM=1; shift;;
    --force)       FORCE=1; shift;;
    --dry-run)     DRY_RUN=1; shift;;
    --help|-h) usage; exit 0;;
    *) log_warning "未知参数: $1"; usage; exit 1;;
  esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Upstream repository URL
UPSTREAM_URL="https://github.com/NoFxAiOS/nofx.git"

# Helper functions
log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

log_section() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════${NC}"
}

confirm() {
    read -p "$(echo -e ${YELLOW}"$1 (y/N): "${NC})" -n 1 -r
    echo
    [[ $REPLY =~ ^[Yy]$ ]]
}

# Welcome message
echo ""
echo "╔═══════════════════════════════════════════╗"
echo "║  NOFX 上游代码同步工具                    ║"
echo "║  Sync upstream repository                 ║"
echo "╚═══════════════════════════════════════════╝"
echo ""

# Check if we're in a git repo
if ! git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
    log_error "不在 Git 仓库中。请在 NOFX 项目目录下运行此脚本。"
    exit 1
fi

# Get current branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
log_info "当前分支: $CURRENT_BRANCH"

# Check if there are uncommitted changes
log_section "检查工作区状态"
if ! git diff --quiet || ! git diff --cached --quiet; then
    log_warning "检测到未提交的更改"
    echo ""
    echo "未提交的文件:"; git status --short
    echo ""
    if [[ $FORCE -eq 0 ]]; then
      if [[ $NO_CONFIRM -eq 1 ]] || confirm "继续可能影响未提交的更改，是否继续?"; then :; else
        log_info "已取消同步"; exit 0; fi
    fi
else
    log_success "工作区干净，可以安全同步"
fi

# Check and add remote
log_section "配置上游远程仓库"
if git remote | grep -q "^${REMOTE}$"; then
    CURRENT_UPSTREAM=$(git remote get-url "$REMOTE")
    log_info "已存在 $REMOTE 远程仓库: $CURRENT_UPSTREAM"
    
    if [ "$CURRENT_UPSTREAM" != "$UPSTREAM_URL" ] && [ "$REMOTE" = "upstream" ]; then
        log_warning "upstream URL 不匹配"
        echo "  当前: $CURRENT_UPSTREAM"
        echo "  期望: $UPSTREAM_URL"
        
        if [[ $NO_CONFIRM -eq 1 ]] || confirm "是否更新 upstream URL?"; then
            git remote set-url "$REMOTE" "$UPSTREAM_URL"
            log_success "已更新 upstream URL"
        fi
    fi
else
    log_info "添加 $REMOTE 远程仓库..."
    git remote add "$REMOTE" "$UPSTREAM_URL"
    log_success "已添加 $REMOTE 远程仓库: $UPSTREAM_URL"
fi

# Fetch upstream
log_section "获取上游最新代码"
log_info "正在从 $REMOTE 获取最新代码..."
if git fetch "$REMOTE"; then
    log_success "成功获取上游代码"
else
    log_error "获取上游代码失败"
    exit 1
fi

# If specifying a commit, handle single-commit path
if [[ -n "$COMMIT_SHA" ]]; then
  log_section "单提交同步"
  [[ -z "$BRANCH" ]] && BRANCH="main"
  log_info "引用分支: $REMOTE/$BRANCH"
  git fetch "$REMOTE" "$BRANCH" || true
  if ! git cat-file -e "$COMMIT_SHA"^{commit} 2>/dev/null; then
    log_warning "本地不存在提交 $COMMIT_SHA，尝试从 $REMOTE 抓取"
    git fetch "$REMOTE" "$COMMIT_SHA" || true
  fi
  if ! git cat-file -e "$COMMIT_SHA"^{commit} 2>/dev/null; then
    log_error "仍未找到提交 $COMMIT_SHA，请确认 SHA 与远端"
    exit 1
  fi

  log_info "将要引入的提交:"
  git --no-pager show --no-patch --oneline "$COMMIT_SHA" | sed 's/^/  /'
  [[ $DRY_RUN -eq 1 ]] && { log_info "dry-run: 不执行实际合入"; exit 0; }

  if [[ "$MODE" = "merge" ]]; then
    log_info "合并模式：git merge --no-ff $COMMIT_SHA"
    if git merge --no-ff "$COMMIT_SHA" -m "Merge upstream commit $COMMIT_SHA"; then
      log_success "单提交合并完成"
    else
      log_error "合并冲突，请解决后执行: git merge --continue 或 git merge --abort"
      exit 1
    fi
  else
    log_info "拣选模式：git cherry-pick -x $COMMIT_SHA"
    if git cherry-pick -x "$COMMIT_SHA"; then
      log_success "单提交拣选完成"
    else
      log_error "拣选冲突，请解决后执行: git cherry-pick --continue 或 git cherry-pick --abort"
      exit 1
    fi
  fi

  log_section "完成"
  log_success "已合入 $COMMIT_SHA"
  exit 0
fi

# ===== 分支同步路径（原有逻辑） =====

log_section "选择同步策略"
echo ""; log_info "当前分支: $CURRENT_BRANCH"; echo ""
echo "建议:"; echo "  - dev 分支同步 upstream/dev"; echo "  - main 分支同步 upstream/main"; echo "  - 其他分支默认 upstream/dev"; echo ""

# Determine target branch
if [[ -n "$BRANCH" ]]; then
  TARGET_BRANCH="$REMOTE/$BRANCH"
elif [ "$CURRENT_BRANCH" = "dev" ]; then
  TARGET_BRANCH="$REMOTE/dev"
elif [ "$CURRENT_BRANCH" = "main" ]; then
  TARGET_BRANCH="$REMOTE/main"
else
  echo "可用分支:"; git branch -r | grep "$REMOTE/" | sed 's/^/  /'
  echo ""; read -p "请输入要同步的上游分支 (默认: $REMOTE/dev): " USER_BRANCH
  TARGET_BRANCH=${USER_BRANCH:-$REMOTE/dev}
fi

log_info "目标分支: $TARGET_BRANCH"

# Verify target branch exists
if ! git rev-parse --verify "$TARGET_BRANCH" > /dev/null 2>&1; then
  log_error "分支 '$TARGET_BRANCH' 不存在"
  exit 1
fi

# Show what will be synced
log_section "同步预览"
COMMITS_AHEAD=$(git rev-list --count HEAD.."$TARGET_BRANCH" 2>/dev/null || echo "0")
COMMITS_BEHIND=$(git rev-list --count "$TARGET_BRANCH"..HEAD 2>/dev/null || echo "0")

echo ""
log_info "同步统计:"
echo "  $TARGET_BRANCH 领先当前分支: $COMMITS_AHEAD 个提交"
echo "  当前分支领先 $TARGET_BRANCH: $COMMITS_BEHIND 个提交"
echo ""

if [ "$COMMITS_AHEAD" -eq 0 ]; then
    log_success "已经是最新的，无需同步！"
    exit 0
fi

# Show latest commits
if [ "$COMMITS_AHEAD" -gt 0 ]; then
    log_info "上游最新提交:"
    git log "$CURRENT_BRANCH".."$TARGET_BRANCH" --oneline | head -10 | sed 's/^/  /'
    
    if [ "$COMMITS_AHEAD" -gt 10 ]; then
        echo "  ... 还有 $((COMMITS_AHEAD - 10)) 个提交"
    fi
    echo ""
fi

# Choose sync method
log_section "选择同步方式"
echo ""
echo "1. Merge (合并) - 保留完整的提交历史，创建一个合并提交"
echo "2. Rebase (变基) - 将你的提交重新应用到最新代码上，保持线性历史"
echo ""
read -p "请选择同步方式 (1=merge, 2=rebase, 默认=1): " SYNC_METHOD
SYNC_METHOD=${SYNC_METHOD:-1}

# Perform sync
log_section "执行同步"
if [ "$SYNC_METHOD" = "2" ]; then
    log_info "使用 rebase 方式同步..."
    
    if ! confirm "确定要 rebase 吗? (会重写提交历史)"; then
        log_info "已取消同步"
        exit 0
    fi
    
    if git rebase "$TARGET_BRANCH"; then
        log_success "Rebase 成功完成！"
    else
        log_error "Rebase 过程中出现冲突"
        echo ""
        log_info "解决冲突后，运行以下命令继续:"
        echo "  git rebase --continue"
        echo ""
        log_info "或者取消 rebase:"
        echo "  git rebase --abort"
        exit 1
    fi
else
    log_info "使用 merge 方式同步..."
    
    if git merge "$TARGET_BRANCH" --no-edit; then
        log_success "Merge 成功完成！"
    else
        log_error "Merge 过程中出现冲突"
        echo ""
        log_info "解决冲突后，运行以下命令:"
        echo "  git add ."
        echo "  git commit"
        echo ""
        log_info "或者取消 merge:"
        echo "  git merge --abort"
        exit 1
    fi
fi

# Summary
log_section "同步完成"
echo ""
log_success "代码同步成功！"
echo ""
log_info "当前状态:"
git status --short | head -10 || true
echo ""

if [ "$COMMITS_BEHIND" -gt 0 ]; then
    log_info "你的本地提交:"
    git log "$TARGET_BRANCH"..HEAD --oneline | head -5 | sed 's/^/  /'
    echo ""
fi

log_info "下一步操作:"
echo "  1. 检查代码是否有冲突需要解决"
echo "  2. 测试代码是否正常工作"
echo "  3. 如果一切正常，推送到你的 fork:"
echo "     git push origin $CURRENT_BRANCH"
echo ""

if [ "$SYNC_METHOD" = "2" ] && [ "$COMMITS_BEHIND" -gt 0 ]; then
    log_warning "由于使用了 rebase，推送时可能需要强制推送:"
    echo "     git push -f origin $CURRENT_BRANCH"
    echo ""
fi

log_success "感谢使用 NOFX！🚀"
echo ""
