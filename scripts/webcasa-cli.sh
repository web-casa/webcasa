#!/usr/bin/env bash
# ============================================================================
#  Web.Casa — CLI Management Tool
#  https://web.casa
#
#  Usage:
#    webcasa                         Interactive menu
#    webcasa panel <command>         Panel management
#    webcasa caddy <command>         Caddy management
#    webcasa info                    System information
#    webcasa update                  Update Web.Casa
#    webcasa reset-password          Reset admin password
#    webcasa version                 Show version
#    webcasa help                    Show help
# ============================================================================

set -euo pipefail

# ==================== Configuration ====================
VERSION="0.9.1"
GITHUB_REPO="web-casa/webcasa"
SERVER_BIN="/usr/local/bin/webcasa-server"
CADDY_BIN="/usr/local/bin/caddy"
ENV_FILE="/etc/webcasa/webcasa.env"
DATA_DIR="/var/lib/webcasa"
LOG_DIR="/var/log/webcasa"
SERVICE_NAME="webcasa"
CADDY_ADMIN="http://localhost:2019"

# ==================== Colors ====================
if [[ -t 1 ]]; then
    GREEN='\033[0;32m'
    RED='\033[0;31m'
    YELLOW='\033[1;33m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    DIM='\033[2m'
    NC='\033[0m'
else
    GREEN='' RED='' YELLOW='' CYAN='' BOLD='' DIM='' NC=''
fi

# ==================== Output Helpers ====================
info()    { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }
success() { echo -e "${GREEN}  ✓${NC} $*"; }

# ==================== Environment ====================
load_env() {
    WEBCASA_PORT="${WEBCASA_PORT:-39921}"
    if [[ -f "$ENV_FILE" ]]; then
        # shellcheck source=/dev/null
        set +u
        source "$ENV_FILE" 2>/dev/null || true
        set -u
    fi
    WEBCASA_PORT="${WEBCASA_PORT:-39921}"
}

# ==================== Status Detection ====================
panel_is_running() {
    systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null
}

caddy_is_running() {
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' "${CADDY_ADMIN}/config/" 2>/dev/null || echo "000")
    [[ "$code" == "200" ]]
}

get_panel_pid() {
    systemctl show -p MainPID --value "$SERVICE_NAME" 2>/dev/null || echo "0"
}

get_panel_port() {
    load_env
    echo "$WEBCASA_PORT"
}

get_server_ip() {
    local ip
    ip=$(hostname -I 2>/dev/null | awk '{print $1}')
    if [[ -z "$ip" ]]; then
        ip=$(ip -4 addr show scope global 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' | head -1)
    fi
    echo "${ip:-127.0.0.1}"
}

get_caddy_version() {
    if command -v "$CADDY_BIN" &>/dev/null; then
        "$CADDY_BIN" version 2>/dev/null | awk '{print $1}' | tr -d 'v'
    else
        echo "not installed"
    fi
}

get_webcasa_version() {
    if [[ -x "$SERVER_BIN" ]]; then
        "$SERVER_BIN" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+[0-9.]*' || echo "$VERSION"
    else
        echo "$VERSION"
    fi
}

# ==================== Panel Commands ====================
cmd_panel() {
    local action="${1:-status}"
    case "$action" in
        status)  cmd_panel_status ;;
        start)   cmd_panel_start ;;
        stop)    cmd_panel_stop ;;
        restart) cmd_panel_restart ;;
        logs)    shift; cmd_panel_logs "$@" ;;
        *)       error "Unknown panel command: $action"; echo "Usage: webcasa panel {status|start|stop|restart|logs}"; exit 1 ;;
    esac
}

cmd_panel_status() {
    load_env
    echo ""
    echo -e "${BOLD}Web.Casa Panel Status${NC}"
    echo "────────────────────────────────"

    if panel_is_running; then
        local pid
        pid=$(get_panel_pid)
        echo -e "  Status:  ${GREEN}● Running${NC} (PID $pid)"
    else
        echo -e "  Status:  ${RED}● Stopped${NC}"
    fi

    echo -e "  Port:    ${WEBCASA_PORT}"
    echo -e "  URL:     http://$(get_server_ip):${WEBCASA_PORT}"
    echo -e "  Data:    ${DATA_DIR}"
    echo -e "  Logs:    journalctl -u ${SERVICE_NAME}"
    echo ""
}

cmd_panel_start() {
    if panel_is_running; then
        warn "Panel is already running"
        return 0
    fi
    info "Starting Web.Casa panel..."
    systemctl start "$SERVICE_NAME"
    sleep 1
    if panel_is_running; then
        success "Panel started successfully"
        load_env
        echo -e "  URL: http://$(get_server_ip):${WEBCASA_PORT}"
    else
        error "Failed to start panel. Check: journalctl -u $SERVICE_NAME"
        exit 1
    fi
}

cmd_panel_stop() {
    if ! panel_is_running; then
        warn "Panel is not running"
        return 0
    fi
    info "Stopping Web.Casa panel..."
    systemctl stop "$SERVICE_NAME"
    success "Panel stopped"
}

cmd_panel_restart() {
    info "Restarting Web.Casa panel..."
    systemctl restart "$SERVICE_NAME"
    sleep 1
    if panel_is_running; then
        success "Panel restarted successfully"
        load_env
        echo -e "  URL: http://$(get_server_ip):${WEBCASA_PORT}"
    else
        error "Failed to restart panel. Check: journalctl -u $SERVICE_NAME"
        exit 1
    fi
}

cmd_panel_logs() {
    local follow=""
    local lines="100"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -f|--follow) follow="-f"; shift ;;
            -n|--lines)  shift; lines="${1:-100}"; shift ;;
            *)           shift ;;
        esac
    done
    journalctl -u "$SERVICE_NAME" --no-pager -n "$lines" ${follow:+"$follow"}
}

# ==================== Caddy Commands ====================
cmd_caddy() {
    local action="${1:-status}"
    case "$action" in
        status)  cmd_caddy_status ;;
        start)   cmd_caddy_start ;;
        stop)    cmd_caddy_stop ;;
        restart) cmd_caddy_restart ;;
        upgrade) shift; cmd_caddy_upgrade "$@" ;;
        logs)    shift; cmd_caddy_logs "$@" ;;
        *)       error "Unknown caddy command: $action"; echo "Usage: webcasa caddy {status|start|stop|restart|upgrade|logs}"; exit 1 ;;
    esac
}

cmd_caddy_status() {
    echo ""
    echo -e "${BOLD}Caddy Status${NC}"
    echo "────────────────────────────────"

    if ! command -v "$CADDY_BIN" &>/dev/null; then
        echo -e "  Status:  ${RED}● Not installed${NC}"
        echo ""
        return
    fi

    local ver
    ver=$(get_caddy_version)

    if caddy_is_running; then
        echo -e "  Status:  ${GREEN}● Running${NC}"
    else
        echo -e "  Status:  ${RED}● Stopped${NC}"
    fi

    echo -e "  Version: ${ver}"
    echo -e "  Binary:  ${CADDY_BIN}"
    echo -e "  Config:  ${DATA_DIR}/Caddyfile"
    echo -e "  Logs:    ${LOG_DIR}/caddy.log"
    echo ""
}

cmd_caddy_start() {
    if caddy_is_running; then
        warn "Caddy is already running"
        return 0
    fi
    info "Starting Caddy..."
    local caddyfile="${DATA_DIR}/Caddyfile"
    XDG_DATA_HOME="${DATA_DIR}/caddy_data" XDG_CONFIG_HOME="${DATA_DIR}/caddy_config" \
        "$CADDY_BIN" start --config "$caddyfile" 2>&1
    sleep 1
    if caddy_is_running; then
        success "Caddy started successfully"
    else
        error "Failed to start Caddy"
        exit 1
    fi
}

cmd_caddy_stop() {
    if ! caddy_is_running; then
        warn "Caddy is not running"
        return 0
    fi
    info "Stopping Caddy..."
    "$CADDY_BIN" stop 2>&1
    success "Caddy stopped"
}

cmd_caddy_restart() {
    info "Restarting Caddy..."
    if caddy_is_running; then
        "$CADDY_BIN" stop 2>/dev/null || true
        sleep 1
    fi
    local caddyfile="${DATA_DIR}/Caddyfile"
    XDG_DATA_HOME="${DATA_DIR}/caddy_data" XDG_CONFIG_HOME="${DATA_DIR}/caddy_config" \
        "$CADDY_BIN" start --config "$caddyfile" 2>&1
    sleep 1
    if caddy_is_running; then
        success "Caddy restarted successfully"
    else
        error "Failed to restart Caddy"
        exit 1
    fi
}

cmd_caddy_upgrade() {
    local target_ver="${1:-}"
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)  arch="amd64" ;;
        aarch64) arch="arm64" ;;
        *)       error "Unsupported architecture: $arch"; exit 1 ;;
    esac

    # Get recommended version if not specified
    if [[ -z "$target_ver" ]]; then
        info "Fetching recommended Caddy version..."
        target_ver=$(curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/refs/heads/main/VERSIONS" 2>/dev/null \
            | grep '^CADDY=' | cut -d= -f2)
        if [[ -z "$target_ver" ]]; then
            error "Failed to fetch recommended version"
            exit 1
        fi
    fi

    local current_ver
    current_ver=$(get_caddy_version)
    if [[ "$current_ver" == "$target_ver" ]]; then
        info "Caddy is already at v${target_ver}"
        return 0
    fi

    info "Upgrading Caddy: v${current_ver} → v${target_ver}"
    local url="https://github.com/caddyserver/caddy/releases/download/v${target_ver}/caddy_${target_ver}_linux_${arch}.tar.gz"

    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    info "Downloading Caddy v${target_ver}..."
    if ! curl -fsSL "$url" -o "${tmpdir}/caddy.tar.gz"; then
        error "Download failed"
        exit 1
    fi

    tar -xzf "${tmpdir}/caddy.tar.gz" -C "$tmpdir" caddy

    # Stop Caddy
    local was_running=false
    if caddy_is_running; then
        was_running=true
        info "Stopping Caddy..."
        "$CADDY_BIN" stop 2>/dev/null || true
        sleep 1
    fi

    # Backup current binary
    cp -f "$CADDY_BIN" "${CADDY_BIN}.bak"

    # Install new binary
    cp -f "${tmpdir}/caddy" "$CADDY_BIN"
    chmod 755 "$CADDY_BIN"
    setcap 'cap_net_bind_service=+ep' "$CADDY_BIN" 2>/dev/null || true

    # Restart if was running
    if $was_running; then
        info "Starting Caddy..."
        local caddyfile="${DATA_DIR}/Caddyfile"
        if XDG_DATA_HOME="${DATA_DIR}/caddy_data" XDG_CONFIG_HOME="${DATA_DIR}/caddy_config" \
            "$CADDY_BIN" start --config "$caddyfile" 2>&1; then
            sleep 1
            if caddy_is_running; then
                rm -f "${CADDY_BIN}.bak"
                success "Caddy upgraded to v${target_ver}"
                return 0
            fi
        fi

        # Rollback on failure
        warn "New Caddy failed to start, rolling back..."
        "$CADDY_BIN" stop 2>/dev/null || true
        mv -f "${CADDY_BIN}.bak" "$CADDY_BIN"
        XDG_DATA_HOME="${DATA_DIR}/caddy_data" XDG_CONFIG_HOME="${DATA_DIR}/caddy_config" \
            "$CADDY_BIN" start --config "$caddyfile" 2>/dev/null || true
        error "Upgrade failed, rolled back to v${current_ver}"
        exit 1
    fi

    rm -f "${CADDY_BIN}.bak"
    success "Caddy upgraded to v${target_ver}"
}

cmd_caddy_logs() {
    local follow=""
    local lines="100"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -f|--follow) follow="-f"; shift ;;
            -n|--lines)  shift; lines="${1:-100}"; shift ;;
            *)           shift ;;
        esac
    done

    local logfile="${LOG_DIR}/caddy.log"
    if [[ ! -f "$logfile" ]]; then
        warn "Caddy log file not found: $logfile"
        return 1
    fi

    if [[ -n "$follow" ]]; then
        tail -f -n "$lines" "$logfile"
    else
        tail -n "$lines" "$logfile"
    fi
}

# ==================== Info Command ====================
cmd_info() {
    load_env
    echo ""
    echo -e "${BOLD}═══════════════════════════════════════════${NC}"
    echo -e "${BOLD}  Web.Casa System Information${NC}"
    echo -e "${BOLD}═══════════════════════════════════════════${NC}"

    # Versions
    echo ""
    echo -e "${CYAN}  Versions${NC}"
    echo "  ─────────────────────────────────"
    echo -e "  Web.Casa:   $(get_webcasa_version)"
    echo -e "  Caddy:      $(get_caddy_version)"
    if command -v docker &>/dev/null; then
        echo -e "  Docker:     $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'not available')"
    fi

    # Services
    echo ""
    echo -e "${CYAN}  Services${NC}"
    echo "  ─────────────────────────────────"
    if panel_is_running; then
        echo -e "  Panel:      ${GREEN}● Running${NC} (PID $(get_panel_pid))"
    else
        echo -e "  Panel:      ${RED}● Stopped${NC}"
    fi
    if caddy_is_running; then
        echo -e "  Caddy:      ${GREEN}● Running${NC}"
    else
        echo -e "  Caddy:      ${RED}● Stopped${NC}"
    fi

    # Network
    echo ""
    echo -e "${CYAN}  Network${NC}"
    echo "  ─────────────────────────────────"
    echo -e "  Panel Port: ${WEBCASA_PORT}"
    echo -e "  Panel URL:  http://$(get_server_ip):${WEBCASA_PORT}"

    # Paths
    echo ""
    echo -e "${CYAN}  Paths${NC}"
    echo "  ─────────────────────────────────"
    echo -e "  Binary:     ${SERVER_BIN}"
    echo -e "  Data:       ${DATA_DIR}"
    echo -e "  Config:     ${ENV_FILE}"
    echo -e "  Logs:       ${LOG_DIR}"
    echo -e "  Caddyfile:  ${DATA_DIR}/Caddyfile"

    # System
    echo ""
    echo -e "${CYAN}  System${NC}"
    echo "  ─────────────────────────────────"
    if [[ -f /etc/os-release ]]; then
        echo -e "  OS:         $(. /etc/os-release && echo "$PRETTY_NAME")"
    fi
    echo -e "  Kernel:     $(uname -r)"
    echo -e "  Arch:       $(uname -m)"
    echo -e "  CPU:        $(nproc) cores"
    echo -e "  Memory:     $(free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo 'N/A') total"
    echo -e "  Disk:       $(df -h "${DATA_DIR}" 2>/dev/null | awk 'NR==2{print $4}' || echo 'N/A') available"
    echo ""
}

# ==================== Update Command ====================
cmd_update() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)  arch="amd64" ;;
        aarch64) arch="arm64" ;;
        *)       error "Unsupported architecture: $arch"; exit 1 ;;
    esac

    info "Checking for updates..."
    local latest_ver
    latest_ver=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>/dev/null \
        | sed -n 's/.*"tag_name":\s*"v\?\([^"]*\)".*/\1/p' || true)

    if [[ -z "$latest_ver" ]]; then
        error "Failed to check for updates"
        exit 1
    fi

    local current_ver
    current_ver=$(get_webcasa_version)

    if [[ "$current_ver" == "$latest_ver" ]]; then
        info "Web.Casa is already at the latest version (v${latest_ver})"
        return 0
    fi

    echo ""
    info "Update available: v${current_ver} → v${latest_ver}"
    echo ""
    read -rp "Proceed with update? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        info "Update cancelled"
        return 0
    fi

    local url="https://github.com/${GITHUB_REPO}/releases/download/v${latest_ver}/webcasa_${latest_ver}_linux_${arch}.tar.gz"

    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    info "Downloading Web.Casa v${latest_ver}..."
    if ! curl -fsSL "$url" -o "${tmpdir}/webcasa.tar.gz"; then
        error "Download failed"
        exit 1
    fi

    tar -xzf "${tmpdir}/webcasa.tar.gz" -C "$tmpdir"

    # Find the binary in the extracted files
    local new_bin="${tmpdir}/webcasa"
    if [[ ! -f "$new_bin" ]]; then
        new_bin="${tmpdir}/webcasa-server"
    fi
    if [[ ! -f "$new_bin" ]]; then
        error "Binary not found in downloaded archive"
        exit 1
    fi

    # Backup current binary
    cp -f "$SERVER_BIN" "${SERVER_BIN}.bak"

    # Install new binary
    cp -f "$new_bin" "$SERVER_BIN"
    chmod 755 "$SERVER_BIN"

    # Restart panel
    info "Restarting Web.Casa panel..."
    systemctl restart "$SERVICE_NAME"
    sleep 2

    if panel_is_running; then
        rm -f "${SERVER_BIN}.bak"
        success "Web.Casa updated to v${latest_ver}"
    else
        warn "Panel failed to start, rolling back..."
        mv -f "${SERVER_BIN}.bak" "$SERVER_BIN"
        systemctl start "$SERVICE_NAME"
        error "Update failed, rolled back to v${current_ver}"
        exit 1
    fi
}

# ==================== Other Commands ====================
cmd_reset_password() {
    if [[ ! -x "$SERVER_BIN" ]]; then
        error "WebCasa server binary not found: $SERVER_BIN"
        exit 1
    fi
    # Must stop panel first to avoid DB lock
    local was_running=false
    if panel_is_running; then
        was_running=true
        info "Stopping panel for password reset..."
        systemctl stop "$SERVICE_NAME"
        sleep 1
    fi

    cd "$DATA_DIR" && "$SERVER_BIN" --reset-password

    if $was_running; then
        info "Restarting panel..."
        systemctl start "$SERVICE_NAME"
    fi
}

cmd_version() {
    echo "Web.Casa CLI v${VERSION}"
    if [[ -x "$SERVER_BIN" ]]; then
        echo "Web.Casa Server $($SERVER_BIN --version 2>/dev/null || echo 'v?')"
    fi
    if command -v "$CADDY_BIN" &>/dev/null; then
        echo "Caddy v$(get_caddy_version)"
    fi
}

cmd_help() {
    cat <<'HELP'
Web.Casa — Server Management CLI

Usage:
  webcasa                           Interactive menu
  webcasa <command> [options]        Direct command

Panel Management:
  webcasa panel status              Show panel status
  webcasa panel start               Start the panel
  webcasa panel stop                Stop the panel
  webcasa panel restart             Restart the panel
  webcasa panel logs [-f]           View panel logs (-f to follow)

Caddy Management:
  webcasa caddy status              Show Caddy status
  webcasa caddy start               Start Caddy
  webcasa caddy stop                Stop Caddy
  webcasa caddy restart             Restart Caddy
  webcasa caddy upgrade [version]   Upgrade Caddy (latest if no version)
  webcasa caddy logs [-f]           View Caddy logs (-f to follow)

Other:
  webcasa info                      Show system information
  webcasa update                    Update Web.Casa to latest version
  webcasa reset-password            Reset admin password
  webcasa version                   Show version information
  webcasa help                      Show this help message
HELP
}

# ==================== Interactive Menu ====================
interactive_menu() {
    load_env

    while true; do
        clear 2>/dev/null || true
        echo ""
        echo -e "${BOLD}══════════════════════════════════════════════${NC}"
        echo -e "${BOLD}  ${GREEN}Web.Casa${NC}${BOLD} v$(get_webcasa_version)${NC}"
        echo -e "${BOLD}══════════════════════════════════════════════${NC}"
        echo ""

        # Status
        if panel_is_running; then
            local pid
            pid=$(get_panel_pid)
            echo -e "  Panel:      ${GREEN}● Running${NC} ${DIM}(PID ${pid})${NC}"
        else
            echo -e "  Panel:      ${RED}● Stopped${NC}"
        fi

        echo -e "  Port:       ${WEBCASA_PORT}"

        if caddy_is_running; then
            echo -e "  Caddy:      ${GREEN}● Running${NC} ${DIM}(v$(get_caddy_version))${NC}"
        else
            echo -e "  Caddy:      ${RED}● Stopped${NC}"
        fi

        echo -e "  URL:        http://$(get_server_ip):${WEBCASA_PORT}"

        echo ""
        echo "──────────────────────────────────────────────"
        echo -e "  ${BOLD}1)${NC} Start Panel            ${BOLD}6)${NC} Upgrade Caddy"
        echo -e "  ${BOLD}2)${NC} Stop Panel             ${BOLD}7)${NC} Update Web.Casa"
        echo -e "  ${BOLD}3)${NC} Restart Panel          ${BOLD}8)${NC} Reset Admin Password"
        echo -e "  ${BOLD}4)${NC} Panel Logs             ${BOLD}9)${NC} System Info"
        echo -e "  ${BOLD}5)${NC} Restart Caddy"
        echo ""
        echo -e "  ${BOLD}0)${NC} Exit"
        echo "──────────────────────────────────────────────"
        echo ""
        read -rp "  Select [0-9]: " choice
        echo ""

        case "$choice" in
            1)
                cmd_panel_start
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            2)
                cmd_panel_stop
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            3)
                cmd_panel_restart
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            4)
                cmd_panel_logs -n 50
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            5)
                cmd_caddy_restart
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            6)
                cmd_caddy_upgrade
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            7)
                cmd_update
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            8)
                cmd_reset_password
                echo ""
                read -rp "  Press Enter to continue..."
                ;;
            9)
                cmd_info
                read -rp "  Press Enter to continue..."
                ;;
            0|q|Q)
                echo -e "  ${DIM}Bye!${NC}"
                echo ""
                exit 0
                ;;
            *)
                warn "Invalid option: $choice"
                sleep 1
                ;;
        esac
    done
}

# ==================== Root Check ====================
check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "This command requires root privileges. Use: sudo webcasa $*"
        exit 1
    fi
}

# ==================== Main Entry ====================
main() {
    case "${1:-}" in
        panel)
            check_root "${@}"
            shift
            cmd_panel "$@"
            ;;
        caddy)
            check_root "${@}"
            shift
            cmd_caddy "$@"
            ;;
        info)
            cmd_info
            ;;
        update)
            check_root "${@}"
            cmd_update
            ;;
        reset-password)
            check_root "${@}"
            cmd_reset_password
            ;;
        version|--version|-v)
            cmd_version
            ;;
        help|--help|-h)
            cmd_help
            ;;
        "")
            check_root ""
            interactive_menu
            ;;
        *)
            error "Unknown command: $1"
            echo ""
            cmd_help
            exit 1
            ;;
    esac
}

main "$@"
