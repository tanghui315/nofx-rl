#!/bin/bash

# 🔄 Sync Upstream Script
# 同步上游仓库 (https://github.com/NoFxAiOS/nofx.git) 的最新代码到你的 fork
# 这个脚本会自动添加 upstream 远程仓库（如果不存在），并同步最新代码

set -e

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
    echo "未提交的文件:"
    git status --short
    
    echo ""
    if ! confirm "继续同步可能会影响未提交的更改。是否继续?"; then
        log_info "已取消同步"
        exit 0
    fi
else
    log_success "工作区干净，可以安全同步"
fi

# Check and add upstream remote
log_section "配置上游远程仓库"
if git remote | grep -q "^upstream$"; then
    CURRENT_UPSTREAM=$(git remote get-url upstream)
    log_info "已存在 upstream 远程仓库: $CURRENT_UPSTREAM"
    
    if [ "$CURRENT_UPSTREAM" != "$UPSTREAM_URL" ]; then
        log_warning "upstream URL 不匹配"
        echo "  当前: $CURRENT_UPSTREAM"
        echo "  期望: $UPSTREAM_URL"
        
        if confirm "是否更新 upstream URL?"; then
            git remote set-url upstream "$UPSTREAM_URL"
            log_success "已更新 upstream URL"
        fi
    fi
else
    log_info "添加 upstream 远程仓库..."
    git remote add upstream "$UPSTREAM_URL"
    log_success "已添加 upstream 远程仓库: $UPSTREAM_URL"
fi

# Fetch upstream
log_section "获取上游最新代码"
log_info "正在从 upstream 获取最新代码..."
if git fetch upstream; then
    log_success "成功获取上游代码"
else
    log_error "获取上游代码失败"
    exit 1
fi

# Check which branch to sync
log_section "选择同步策略"
echo ""
log_info "当前分支: $CURRENT_BRANCH"
echo ""
echo "建议:"
echo "  - 如果在 'dev' 分支，通常同步 'upstream/dev'"
echo "  - 如果在 'main' 分支，通常同步 'upstream/main'"
echo "  - 如果在自己的功能分支，同步 'upstream/dev'"
echo ""

# Determine target branch
if [ "$CURRENT_BRANCH" = "dev" ]; then
    TARGET_BRANCH="upstream/dev"
elif [ "$CURRENT_BRANCH" = "main" ]; then
    TARGET_BRANCH="upstream/main"
else
    # Ask user for target branch
    echo "可用分支:"
    git branch -r | grep "upstream/" | sed 's/^/  /'
    echo ""
    read -p "请输入要同步的上游分支 (默认: upstream/dev): " USER_BRANCH
    TARGET_BRANCH=${USER_BRANCH:-upstream/dev}
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
