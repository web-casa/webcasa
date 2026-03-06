#!/usr/bin/env bash
# WebCasa App Store — Upstream Sync Script
#
# 功能:
#   1. 从 Runtipi 上游同步新增/更新的应用
#   2. 检测与 WebCasa 的兼容性
#   3. 生成中文翻译 (调用 AI API)
#   4. 创建 PR
#
# 用法:
#   ./sync-upstream.sh [--dry-run] [--no-translate] [--app <app-id>]
#
# 环境变量:
#   UPSTREAM_REPO   上游仓库 (默认: https://github.com/runtipi/runtipi-appstore)
#   AI_API_URL      AI API 地址 (OpenAI-compatible)
#   AI_API_KEY      AI API Key
#   AI_MODEL        模型名称 (默认: gpt-4o)

set -euo pipefail

# ── 配置 ──
UPSTREAM_REPO="${UPSTREAM_REPO:-https://github.com/runtipi/runtipi-appstore}"
UPSTREAM_BRANCH="master"
AI_API_URL="${AI_API_URL:-https://api.openai.com/v1}"
AI_MODEL="${AI_MODEL:-gpt-4o}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
APPS_DIR="$REPO_ROOT/apps"
COMPAT_DB="$REPO_ROOT/compatibility.json"
SYNC_LOG="$REPO_ROOT/SYNC_LOG.md"

DRY_RUN=false
NO_TRANSLATE=false
SINGLE_APP=""

# ── 参数解析 ──
while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) DRY_RUN=true; shift ;;
        --no-translate) NO_TRANSLATE=true; shift ;;
        --app) SINGLE_APP="$2"; shift 2 ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# ── 工具函数 ──
log() { echo "[$(date '+%H:%M:%S')] $*"; }
warn() { echo "[$(date '+%H:%M:%S')] ⚠ $*" >&2; }
err() { echo "[$(date '+%H:%M:%S')] ✗ $*" >&2; }

# ── Step 1: 添加并拉取上游 ──
log "Step 1: Fetching upstream..."

if ! git remote | grep -q '^upstream$'; then
    git remote add upstream "$UPSTREAM_REPO"
fi

git fetch upstream "$UPSTREAM_BRANCH" --depth=1

# ── Step 2: 找出变更的应用 ──
log "Step 2: Detecting changed apps..."

# 获取上游 apps/ 目录的应用列表
UPSTREAM_APPS=$(git ls-tree --name-only "upstream/$UPSTREAM_BRANCH" -- apps/ 2>/dev/null | sed 's|^apps/||')

CHANGED_APPS=()
NEW_APPS=()
UPDATED_APPS=()

for app_id in $UPSTREAM_APPS; do
    [[ -z "$app_id" ]] && continue
    [[ "$app_id" == "." ]] && continue

    # 如果指定了单个应用，只处理它
    if [[ -n "$SINGLE_APP" && "$app_id" != "$SINGLE_APP" ]]; then
        continue
    fi

    if [[ ! -d "$APPS_DIR/$app_id" ]]; then
        NEW_APPS+=("$app_id")
        CHANGED_APPS+=("$app_id")
    else
        # 比较 config.json 和 docker-compose.yml 的差异
        upstream_config=$(git show "upstream/$UPSTREAM_BRANCH:apps/$app_id/config.json" 2>/dev/null || echo "")
        local_config=$(cat "$APPS_DIR/$app_id/config.json" 2>/dev/null || echo "")

        if [[ "$upstream_config" != "$local_config" ]]; then
            UPDATED_APPS+=("$app_id")
            CHANGED_APPS+=("$app_id")
        fi
    fi
done

log "Found ${#NEW_APPS[@]} new, ${#UPDATED_APPS[@]} updated apps"

if [[ ${#CHANGED_APPS[@]} -eq 0 ]]; then
    log "No changes detected. Done."
    exit 0
fi

# ── Step 3: 同步文件 ──
log "Step 3: Syncing app files..."

for app_id in "${CHANGED_APPS[@]}"; do
    log "  Syncing: $app_id"

    if $DRY_RUN; then
        log "  [dry-run] Would sync $app_id"
        continue
    fi

    # 创建目录
    mkdir -p "$APPS_DIR/$app_id/metadata"

    # 从上游检出文件 (保留本地的 i18n 和 zh 文件)
    git show "upstream/$UPSTREAM_BRANCH:apps/$app_id/config.json" > "$APPS_DIR/$app_id/config.json" 2>/dev/null || true
    git show "upstream/$UPSTREAM_BRANCH:apps/$app_id/docker-compose.yml" > "$APPS_DIR/$app_id/docker-compose.yml" 2>/dev/null || true
    git show "upstream/$UPSTREAM_BRANCH:apps/$app_id/metadata/description.md" > "$APPS_DIR/$app_id/metadata/description.md" 2>/dev/null || true

    # 同步 logo (尝试多种格式)
    for ext in jpg png svg webp; do
        if git show "upstream/$UPSTREAM_BRANCH:apps/$app_id/metadata/logo.$ext" > "$APPS_DIR/$app_id/metadata/logo.$ext" 2>/dev/null; then
            break
        else
            rm -f "$APPS_DIR/$app_id/metadata/logo.$ext"
        fi
    done
done

# ── Step 4: 兼容性检测 ──
log "Step 4: Running compatibility checks..."

check_compatibility() {
    local app_id="$1"
    local config="$APPS_DIR/$app_id/config.json"
    local compose="$APPS_DIR/$app_id/docker-compose.yml"
    local issues=()
    local compat="full"

    # 检查 compose 文件
    if [[ -f "$compose" ]]; then
        # 安全警告
        if grep -q 'privileged: true' "$compose"; then
            issues+=('{"type":"security_warning","description":"Uses privileged mode","severity":"warning"}')
            [[ "$compat" == "full" ]] && compat="partial"
        fi
        if grep -q 'cap_add:' "$compose"; then
            issues+=('{"type":"security_warning","description":"Uses cap_add","severity":"warning"}')
            [[ "$compat" == "full" ]] && compat="partial"
        fi
        if grep -q 'docker.sock' "$compose"; then
            issues+=('{"type":"security_warning","description":"Mounts docker.sock","severity":"warning"}')
            [[ "$compat" == "full" ]] && compat="partial"
        fi
        if grep -q 'pid: host' "$compose"; then
            issues+=('{"type":"security_warning","description":"Uses host PID namespace","severity":"warning"}')
            [[ "$compat" == "full" ]] && compat="partial"
        fi

        # 检查是否有未知变量
        unknown_vars=$(grep -oP '\$\{(\w+)\}' "$compose" 2>/dev/null | sort -u | while read -r var; do
            var_name="${var#\$\{}"
            var_name="${var_name%\}}"
            # 检查是否是已知变量
            case "$var_name" in
                APP_ID|APP_PORT|APP_DATA_DIR|APP_DOMAIN|APP_HOST|LOCAL_DOMAIN|APP_EXPOSED|APP_PROTOCOL|ROOT_FOLDER_HOST|TZ|NETWORK_INTERFACE|DNS_IP|INTERNAL_IP|COMPOSE_PROJECT_NAME)
                    ;;
                *)
                    # 检查是否在 form_fields 中定义
                    if ! jq -r '.form_fields[]?.env_variable' "$config" 2>/dev/null | grep -q "^${var_name}$"; then
                        echo "$var_name"
                    fi
                    ;;
            esac
        done)

        if [[ -n "$unknown_vars" ]]; then
            for uv in $unknown_vars; do
                issues+=("{\"type\":\"missing_var\",\"description\":\"Unknown variable: \${$uv}\",\"severity\":\"warning\"}")
            done
            [[ "$compat" == "full" ]] && compat="partial"
        fi

        # 检查非 HTTP 端口直接暴露
        if grep -E '^\s+- "[0-9]+:[0-9]+/(tcp|udp)"' "$compose" >/dev/null 2>&1; then
            issues+=('{"type":"firewall_needed","description":"Exposes non-HTTP ports directly","severity":"info"}')
        fi
    fi

    # 输出结果
    local issues_json="["
    local first=true
    for issue in "${issues[@]}"; do
        if $first; then first=false; else issues_json+=","; fi
        issues_json+="$issue"
    done
    issues_json+="]"

    echo "{\"app_id\":\"$app_id\",\"compatibility\":\"$compat\",\"issues\":$issues_json}"
}

# 初始化或加载兼容性数据库
if [[ ! -f "$COMPAT_DB" ]]; then
    echo '{}' > "$COMPAT_DB"
fi

COMPAT_RESULTS=()
for app_id in "${CHANGED_APPS[@]}"; do
    result=$(check_compatibility "$app_id")
    COMPAT_RESULTS+=("$result")

    compat=$(echo "$result" | jq -r '.compatibility')
    issues_count=$(echo "$result" | jq '.issues | length')

    if [[ "$compat" == "full" ]]; then
        log "  ✅ $app_id: fully compatible"
    elif [[ "$compat" == "partial" ]]; then
        warn "  ⚠ $app_id: partially compatible ($issues_count issues)"
    else
        err "  ✗ $app_id: incompatible"
    fi
done

# 更新兼容性数据库
if ! $DRY_RUN; then
    for result in "${COMPAT_RESULTS[@]}"; do
        app_id=$(echo "$result" | jq -r '.app_id')
        # Merge into compatibility.json
        tmp=$(mktemp)
        jq --argjson entry "$result" --arg id "$app_id" '.[$id] = $entry' "$COMPAT_DB" > "$tmp" && mv "$tmp" "$COMPAT_DB"
    done
fi

# ── Step 5: AI 翻译 ──
if ! $NO_TRANSLATE && [[ -n "${AI_API_KEY:-}" ]]; then
    log "Step 5: AI translation..."

    translate_app() {
        local app_id="$1"
        local config="$APPS_DIR/$app_id/config.json"
        local desc_file="$APPS_DIR/$app_id/metadata/description.md"
        local i18n_dir="$APPS_DIR/$app_id/metadata/i18n"
        local zh_json="$i18n_dir/zh.json"
        local zh_desc="$APPS_DIR/$app_id/metadata/description.zh.md"

        # 跳过已有翻译的 (除非是更新)
        if [[ -f "$zh_json" ]] && ! printf '%s\n' "${UPDATED_APPS[@]}" | grep -q "^${app_id}$"; then
            log "  Skip translation for $app_id (already exists)"
            return
        fi

        # 读取源数据
        local name=$(jq -r '.name // ""' "$config")
        local short_desc=$(jq -r '.short_desc // ""' "$config")
        local form_fields=$(jq -c '.form_fields // []' "$config")
        local desc_content=""
        [[ -f "$desc_file" ]] && desc_content=$(cat "$desc_file")

        # 构建 AI 请求
        local prompt="请将以下应用信息翻译为简体中文。

应用名称: $name
简短描述: $short_desc
表单字段: $form_fields

翻译规则:
- 应用名保留英文，后面可加中文说明 (如 \"Nextcloud 私有云\")
- 简短描述控制在 80 字以内
- form_fields 只翻译 label 和 hint
- 技术术语保持一致

请直接返回 JSON 格式:
{
  \"name\": \"中文名\",
  \"short_desc\": \"中文简述\",
  \"form_fields\": {
    \"ENV_VAR_NAME\": {\"label\": \"中文标签\", \"hint\": \"中文提示\"}
  }
}"

        # 调用 AI API
        local response=$(curl -sf "$AI_API_URL/chat/completions" \
            -H "Authorization: Bearer $AI_API_KEY" \
            -H "Content-Type: application/json" \
            -d "$(jq -n --arg model "$AI_MODEL" --arg prompt "$prompt" '{
                model: $model,
                messages: [{"role": "user", "content": $prompt}],
                temperature: 0.3,
                response_format: {"type": "json_object"}
            }')" 2>/dev/null)

        if [[ -z "$response" ]]; then
            warn "  AI translation failed for $app_id"
            return
        fi

        # 提取翻译结果
        local translated=$(echo "$response" | jq -r '.choices[0].message.content')

        if [[ -n "$translated" && "$translated" != "null" ]]; then
            mkdir -p "$i18n_dir"
            echo "$translated" | jq '.' > "$zh_json" 2>/dev/null || echo "$translated" > "$zh_json"
            log "  Translated: $app_id"
        fi

        # 翻译长描述 (如果存在且超过 50 字符)
        if [[ -n "$desc_content" && ${#desc_content} -gt 50 ]]; then
            local desc_prompt="将以下 Markdown 应用描述翻译为简体中文。保留 Markdown 格式，保留英文专有名词、URL 和代码块。\n\n$desc_content"

            local desc_response=$(curl -sf "$AI_API_URL/chat/completions" \
                -H "Authorization: Bearer $AI_API_KEY" \
                -H "Content-Type: application/json" \
                -d "$(jq -n --arg model "$AI_MODEL" --arg prompt "$desc_prompt" '{
                    model: $model,
                    messages: [{"role": "user", "content": $prompt}],
                    temperature: 0.3
                }')" 2>/dev/null)

            if [[ -n "$desc_response" ]]; then
                local zh_content=$(echo "$desc_response" | jq -r '.choices[0].message.content')
                if [[ -n "$zh_content" && "$zh_content" != "null" ]]; then
                    echo "$zh_content" > "$zh_desc"
                    log "  Translated description: $app_id"
                fi
            fi
        fi
    }

    for app_id in "${CHANGED_APPS[@]}"; do
        translate_app "$app_id"
        sleep 1  # Rate limiting
    done
else
    if [[ -z "${AI_API_KEY:-}" ]]; then
        log "Step 5: Skipped (no AI_API_KEY set)"
    else
        log "Step 5: Skipped (--no-translate)"
    fi
fi

# ── Step 6: 生成同步日志 ──
log "Step 6: Updating sync log..."

if ! $DRY_RUN; then
    SYNC_DATE=$(date '+%Y-%m-%d %H:%M')
    {
        echo ""
        echo "## Sync: $SYNC_DATE"
        echo ""
        if [[ ${#NEW_APPS[@]} -gt 0 ]]; then
            echo "### New Apps (${#NEW_APPS[@]})"
            for app_id in "${NEW_APPS[@]}"; do
                compat=$(jq -r --arg id "$app_id" '.[$id].compatibility // "unknown"' "$COMPAT_DB")
                echo "- \`$app_id\` — $compat"
            done
            echo ""
        fi
        if [[ ${#UPDATED_APPS[@]} -gt 0 ]]; then
            echo "### Updated Apps (${#UPDATED_APPS[@]})"
            for app_id in "${UPDATED_APPS[@]}"; do
                echo "- \`$app_id\`"
            done
            echo ""
        fi
    } >> "$SYNC_LOG"
fi

# ── 完成 ──
log "Done! ${#NEW_APPS[@]} new, ${#UPDATED_APPS[@]} updated apps synced."

if $DRY_RUN; then
    log "[dry-run] No files were modified."
else
    log "Next steps:"
    log "  1. Review changes: git diff"
    log "  2. Commit: git add -A && git commit -m 'sync: upstream $(date +%Y%m%d)'"
    log "  3. Push and create PR"
fi
