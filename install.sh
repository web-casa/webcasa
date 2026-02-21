#!/usr/bin/env bash
# ============================================================================
#  CaddyPanel — One-Click Install Script
#  Supports: Ubuntu 20+, Debian 11+, CentOS Stream 8+, AlmaLinux 8+, Fedora 38+
#           openAnolis, Alibaba Cloud Linux, openEuler, openCloudOS, Kylin (银河麒麟)
#
#  Usage:
#    curl -fsSL https://raw.githubusercontent.com/caddypanel/caddypanel/main/install.sh | bash
#    or:
#    bash install.sh
#
#  Options:
#    --uninstall     Remove CaddyPanel (keeps data by default)
#    --purge         Remove CaddyPanel and all data
#    --no-caddy      Skip Caddy installation
#    --port PORT     Set panel port (default: 8080)
# ============================================================================

set -euo pipefail

# ==================== Configuration ====================
CADDYPANEL_VERSION="0.1.0"
GO_VERSION="1.26.4"
NODE_VERSION="24"
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/var/lib/caddypanel"
LOG_DIR="/var/log/caddypanel"
CONFIG_DIR="/etc/caddypanel"
SERVICE_USER="caddypanel"
PANEL_PORT="39921"
SKIP_CADDY=false
UNINSTALL=false
PURGE=false
NON_INTERACTIVE=false

# ==================== Colors ====================
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ==================== Helpers ====================
info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC}   $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERR]${NC}  $*" >&2; }
fatal()   { error "$*"; exit 1; }
step()    { echo -e "\n${CYAN}${BOLD}▶ $*${NC}"; }

check_root() {
    if [[ $EUID -ne 0 ]]; then
        fatal "This script must be run as root. Try: sudo bash install.sh"
    fi
}

# ==================== OS Detection ====================
detect_os() {
    if [[ ! -f /etc/os-release ]]; then
        fatal "Cannot detect OS: /etc/os-release not found"
    fi

    . /etc/os-release

    OS_ID="${ID,,}"
    OS_VERSION_ID="${VERSION_ID:-}"
    OS_NAME="${PRETTY_NAME:-$ID}"

    # Normalize OS family
    case "$OS_ID" in
        ubuntu)                 OS_FAMILY="debian" ;;
        debian)                 OS_FAMILY="debian" ;;
        centos)                 OS_FAMILY="rhel" ;;
        almalinux|alma)         OS_FAMILY="rhel" ;;
        rocky|rockylinux)       OS_FAMILY="rhel" ;;
        fedora)                 OS_FAMILY="rhel" ;;
        rhel|redhat)            OS_FAMILY="rhel" ;;
        # Chinese domestic distributions
        anolis)                 OS_FAMILY="rhel" ;;  # openAnolis (Alibaba)
        alinux)                 OS_FAMILY="rhel" ;;  # Alibaba Cloud Linux
        openeuler|openEuler)    OS_FAMILY="rhel" ;;  # openEuler (Huawei)
        opencloudos)            OS_FAMILY="rhel" ;;  # openCloudOS (Tencent)
        kylin)
            # Kylin Desktop is Ubuntu-based (apt), Kylin Server is CentOS-based (dnf/yum)
            if command -v apt-get &>/dev/null && ! command -v dnf &>/dev/null; then
                OS_FAMILY="debian"
            else
                OS_FAMILY="rhel"
            fi
            ;;  # Kylin 银河麒麟
        *)
            # Last-resort auto-detection by package manager
            if command -v apt-get &>/dev/null; then
                warn "Unknown OS '$OS_ID', detected apt — treating as Debian-family"
                OS_FAMILY="debian"
            elif command -v dnf &>/dev/null || command -v yum &>/dev/null; then
                warn "Unknown OS '$OS_ID', detected dnf/yum — treating as RHEL-family"
                OS_FAMILY="rhel"
            else
                fatal "Unsupported OS: $OS_NAME ($OS_ID). Supported: Ubuntu, Debian, CentOS Stream, AlmaLinux, Rocky Linux, Fedora, openAnolis, Alibaba Cloud Linux, openEuler, openCloudOS, Kylin"
            fi
            ;;
    esac

    # Detect architecture
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)     GO_ARCH="amd64";   CADDY_ARCH="amd64" ;;
        aarch64)    GO_ARCH="arm64";   CADDY_ARCH="arm64" ;;
        armv7l)     GO_ARCH="armv6l";  CADDY_ARCH="armv7"  ;;
        *)          fatal "Unsupported architecture: $ARCH" ;;
    esac

    info "Detected OS: ${BOLD}$OS_NAME${NC} ($OS_FAMILY/$ARCH)"
}

# ==================== Parse Arguments ====================
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --uninstall) UNINSTALL=true; shift ;;
            --purge)     PURGE=true; UNINSTALL=true; shift ;;
            --no-caddy)  SKIP_CADDY=true; shift ;;
            --port)      PANEL_PORT="$2"; shift 2 ;;
            -y|--yes)    NON_INTERACTIVE=true; shift ;;
            -h|--help)   usage; exit 0 ;;
            *)           warn "Unknown option: $1"; shift ;;
        esac
    done
}

usage() {
    cat <<EOF
${BOLD}CaddyPanel Installer v${CADDYPANEL_VERSION}${NC}

Usage: bash install.sh [OPTIONS]

Options:
  --uninstall     Remove CaddyPanel (keeps data)
  --purge         Remove CaddyPanel and all data
  --no-caddy      Skip Caddy installation
  --port PORT     Set panel port (default: 39921)
  -y, --yes       Non-interactive mode (skip prompts)
  -h, --help      Show this help

Supported OS:
  Ubuntu 20.04+, Debian 11+, CentOS Stream 8+,
  AlmaLinux 8+, Rocky Linux 8+, Fedora 38+,
  openAnolis, Alibaba Cloud Linux, openEuler,
  openCloudOS, Kylin (银河麒麟)
EOF
}

# ==================== Uninstall ====================
do_uninstall() {
    step "Uninstalling CaddyPanel"

    # Stop and disable service
    if systemctl is-active --quiet caddypanel 2>/dev/null; then
        info "Stopping CaddyPanel service..."
        systemctl stop caddypanel
    fi
    if systemctl is-enabled --quiet caddypanel 2>/dev/null; then
        systemctl disable caddypanel
    fi

    # Remove files
    rm -f /etc/systemd/system/caddypanel.service
    rm -f "$INSTALL_DIR/caddypanel"
    systemctl daemon-reload

    if $PURGE; then
        warn "Purging all data..."
        rm -rf "$DATA_DIR"
        rm -rf "$LOG_DIR"
        rm -rf "$CONFIG_DIR"
        userdel -r "$SERVICE_USER" 2>/dev/null || true
        groupdel "$SERVICE_USER" 2>/dev/null || true
        success "CaddyPanel completely removed (including data)"
    else
        info "Data preserved at: $DATA_DIR"
        info "Config preserved at: $CONFIG_DIR"
        success "CaddyPanel removed (data kept). Use --purge to remove everything."
    fi

    exit 0
}

# ==================== Install Dependencies ====================
install_deps_debian() {
    info "Updating package index..."
    apt-get update -qq

    info "Installing base dependencies..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
        curl wget git gcc make \
        ca-certificates gnupg lsb-release \
        sqlite3 > /dev/null
}

install_deps_rhel() {
    info "Installing base dependencies..."
    if command -v dnf &>/dev/null; then
        dnf install -y -q \
            curl wget git gcc make \
            ca-certificates sqlite \
            tar gzip which
    else
        yum install -y -q \
            curl wget git gcc make \
            ca-certificates sqlite \
            tar gzip which
    fi
}

install_deps() {
    step "Installing system dependencies"
    case "$OS_FAMILY" in
        debian) install_deps_debian ;;
        rhel)   install_deps_rhel ;;
    esac
    success "Dependencies installed"
}

# ==================== Install Go ====================
install_go() {
    step "Installing Go $GO_VERSION"

    # Check if Go is already installed with sufficient version
    if command -v go &>/dev/null; then
        CURRENT_GO=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
        REQUIRED="1.22"
        if printf '%s\n%s' "$REQUIRED" "$CURRENT_GO" | sort -V | head -1 | grep -q "^${REQUIRED}$"; then
            success "Go $CURRENT_GO already installed (>= $REQUIRED)"
            return
        fi
        warn "Go $CURRENT_GO found but need >= $REQUIRED, upgrading..."
    fi

    GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    GO_URL="https://go.dev/dl/${GO_TAR}"

    info "Downloading Go from $GO_URL ..."
    wget -q --show-progress -O "/tmp/${GO_TAR}" "$GO_URL"

    info "Installing to /usr/local/go ..."
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${GO_TAR}"
    rm -f "/tmp/${GO_TAR}"

    # Setup PATH
    if ! grep -q '/usr/local/go/bin' /etc/profile.d/go.sh 2>/dev/null; then
        echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
    fi
    export PATH=$PATH:/usr/local/go/bin

    success "Go $(go version | grep -oP 'go\K\S+') installed"
}

# ==================== Install Node.js ====================
install_nodejs() {
    step "Installing Node.js $NODE_VERSION"

    if command -v node &>/dev/null; then
        NODE_VER=$(node --version | tr -d 'v' | cut -d. -f1)
        if [[ "$NODE_VER" -ge "$NODE_VERSION" ]]; then
            success "Node.js $(node --version) already installed (>= v$NODE_VERSION)"
            return
        fi
        warn "Node.js v$NODE_VER found but need >= v$NODE_VERSION, upgrading..."
    fi

    case "$OS_FAMILY" in
        debian)
            info "Setting up NodeSource repository..."
            mkdir -p /etc/apt/keyrings
            curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg 2>/dev/null
            echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_VERSION}.x nodistro main" > /etc/apt/sources.list.d/nodesource.list
            apt-get update -qq
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs > /dev/null
            ;;
        rhel)
            info "Setting up NodeSource repository..."
            curl -fsSL https://rpm.nodesource.com/setup_${NODE_VERSION}.x | bash - > /dev/null 2>&1
            if command -v dnf &>/dev/null; then
                dnf install -y -q nodejs
            else
                yum install -y -q nodejs
            fi
            ;;
    esac

    success "Node.js $(node --version) installed"
}

# ==================== Install Caddy ====================
install_caddy() {
    if $SKIP_CADDY; then
        warn "Skipping Caddy installation (--no-caddy)"
        return
    fi

    step "Installing Caddy"

    if command -v caddy &>/dev/null; then
        success "Caddy $(caddy version 2>/dev/null || echo 'unknown') already installed"
        return
    fi

    case "$OS_FAMILY" in
        debian)
            info "Setting up Caddy repository..."
            apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https > /dev/null 2>&1 || true
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg 2>/dev/null
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
            apt-get update -qq
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq caddy > /dev/null
            ;;
        rhel)
            info "Setting up Caddy repository..."
            if command -v dnf &>/dev/null; then
                dnf install -y -q 'dnf-command(copr)' > /dev/null 2>&1 || true
                dnf copr enable -y @caddy/caddy > /dev/null 2>&1
                dnf install -y -q caddy
            else
                yum-config-manager --add-repo https://copr.fedorainfracloud.org/coprs/@caddy/caddy/repo/epel-8/@caddy-caddy-epel-8.repo > /dev/null 2>&1
                yum install -y -q caddy
            fi
            ;;
    esac

    # Stop the default caddy service (we manage it via CaddyPanel)
    systemctl stop caddy 2>/dev/null || true
    systemctl disable caddy 2>/dev/null || true

    success "Caddy $(caddy version 2>/dev/null || echo '') installed"
}

# ==================== Build CaddyPanel ====================
build_caddypanel() {
    step "Building CaddyPanel"

    # Determine source directory
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    # Check if we're in the source tree
    if [[ -f "$SCRIPT_DIR/main.go" ]]; then
        SRC_DIR="$SCRIPT_DIR"
    elif [[ -f "$SCRIPT_DIR/../main.go" ]]; then
        SRC_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
    else
        # Clone from repository
        info "Cloning CaddyPanel source..."
        SRC_DIR="/tmp/caddypanel-build"
        rm -rf "$SRC_DIR"
        git clone --depth 1 https://github.com/caddypanel/caddypanel.git "$SRC_DIR"
    fi

    info "Source directory: $SRC_DIR"

    # Build frontend
    info "Building frontend (npm install + build)..."
    cd "$SRC_DIR/web"
    npm ci --loglevel=warn 2>&1 | tail -3
    npm run build 2>&1 | tail -5
    success "Frontend built"

    # Build backend
    info "Building Go backend..."
    cd "$SRC_DIR"
    export PATH=$PATH:/usr/local/go/bin
    export CGO_ENABLED=1
    go build -ldflags="-s -w -X main.Version=${CADDYPANEL_VERSION}" -o caddypanel .
    success "Backend built"

    # Install binary
    info "Installing binary to $INSTALL_DIR/caddypanel ..."
    cp -f caddypanel "$INSTALL_DIR/caddypanel"
    chmod 755 "$INSTALL_DIR/caddypanel"

    # Copy frontend dist
    mkdir -p "$DATA_DIR/web/dist"
    cp -r web/dist/* "$DATA_DIR/web/dist/"

    success "CaddyPanel v${CADDYPANEL_VERSION} installed to $INSTALL_DIR/caddypanel"
}

# ==================== Setup System ====================
setup_user() {
    step "Setting up system user and directories"

    # Create system user
    if ! id "$SERVICE_USER" &>/dev/null; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
        info "Created system user: $SERVICE_USER"
    else
        info "User $SERVICE_USER already exists"
    fi

    # Create directories
    mkdir -p "$DATA_DIR"/{backups,web/dist}
    mkdir -p "$LOG_DIR"
    mkdir -p "$CONFIG_DIR"

    # Set ownership
    chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"
    chown -R "$SERVICE_USER:$SERVICE_USER" "$LOG_DIR"

    success "Directories created"
}

setup_config() {
    step "Configuring CaddyPanel"

    ENV_FILE="$CONFIG_DIR/caddypanel.env"

    if [[ -f "$ENV_FILE" ]]; then
        info "Config file already exists, preserving: $ENV_FILE"
        return
    fi

    # Generate random JWT secret
    JWT_SECRET=$(head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 48)

    # Determine Caddy binary path
    CADDY_BIN=$(command -v caddy 2>/dev/null || echo "/usr/bin/caddy")

    cat > "$ENV_FILE" <<ENVEOF
# CaddyPanel Configuration
# Generated on $(date -Iseconds)

CADDYPANEL_PORT=${PANEL_PORT}
CADDYPANEL_DATA_DIR=${DATA_DIR}
CADDYPANEL_DB_PATH=${DATA_DIR}/caddypanel.db
CADDYPANEL_JWT_SECRET=${JWT_SECRET}
CADDYPANEL_CADDY_BIN=${CADDY_BIN}
CADDYPANEL_CADDYFILE_PATH=${DATA_DIR}/Caddyfile
CADDYPANEL_LOG_DIR=${LOG_DIR}
CADDYPANEL_ADMIN_API=http://localhost:2019
ENVEOF

    chmod 600 "$ENV_FILE"
    chown root:root "$ENV_FILE"

    success "Config written to $ENV_FILE (JWT secret auto-generated)"

    # Create default Caddyfile so Caddy can start immediately
    CADDYFILE="${DATA_DIR}/Caddyfile"
    if [[ ! -f "$CADDYFILE" ]]; then
        cat > "$CADDYFILE" <<CFEOF
# ============================================
# Auto-generated by CaddyPanel
# DO NOT EDIT MANUALLY — changes will be overwritten
# ============================================

{
	admin localhost:2019
	log {
		output file ${LOG_DIR}/caddy.log {
			roll_size 100MiB
			roll_keep 5
		}
		level INFO
	}
}
CFEOF
        chown "$SERVICE_USER:$SERVICE_USER" "$CADDYFILE"
        success "Default Caddyfile created"
    fi
}

setup_systemd() {
    step "Setting up systemd service"

    # Determine Caddy binary for the service dependency
    CADDY_BIN=$(command -v caddy 2>/dev/null || echo "/usr/bin/caddy")

    cat > /etc/systemd/system/caddypanel.service <<SVCEOF
[Unit]
Description=CaddyPanel - Caddy Reverse Proxy Management Panel
Documentation=https://github.com/caddypanel/caddypanel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
ExecStart=${INSTALL_DIR}/caddypanel
WorkingDirectory=${DATA_DIR}
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Environment
EnvironmentFile=-${CONFIG_DIR}/caddypanel.env

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${DATA_DIR} ${LOG_DIR}
PrivateTmp=true

# Caddy needs to bind to privileged ports
AmbientCapabilities=CAP_NET_BIND_SERVICE

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=caddypanel

[Install]
WantedBy=multi-user.target
SVCEOF

    systemctl daemon-reload
    systemctl enable caddypanel

    success "Systemd service installed and enabled"
}

setup_firewall() {
    step "Configuring firewall"

    if command -v ufw &>/dev/null; then
        info "Configuring UFW..."
        ufw allow "$PANEL_PORT"/tcp comment "CaddyPanel" > /dev/null 2>&1 || true
        ufw allow 80/tcp comment "HTTP" > /dev/null 2>&1 || true
        ufw allow 443/tcp comment "HTTPS" > /dev/null 2>&1 || true
        success "UFW rules added (ports $PANEL_PORT, 80, 443)"
    elif command -v firewall-cmd &>/dev/null; then
        info "Configuring firewalld..."
        firewall-cmd --permanent --add-port="${PANEL_PORT}/tcp" > /dev/null 2>&1 || true
        firewall-cmd --permanent --add-service=http > /dev/null 2>&1 || true
        firewall-cmd --permanent --add-service=https > /dev/null 2>&1 || true
        firewall-cmd --reload > /dev/null 2>&1 || true
        success "Firewalld rules added (ports $PANEL_PORT, 80, 443)"
    else
        warn "No firewall detected. Make sure ports $PANEL_PORT, 80, 443 are open."
    fi
}

# ==================== Allow Caddy to bind privileged ports ====================
setup_caddy_permissions() {
    if $SKIP_CADDY; then return; fi

    # Allow the caddypanel user to use caddy on privileged ports
    CADDY_BIN=$(command -v caddy 2>/dev/null || echo "")
    if [[ -n "$CADDY_BIN" ]]; then
        # Grant cap_net_bind_service to caddy binary
        setcap 'cap_net_bind_service=+ep' "$CADDY_BIN" 2>/dev/null || true
        # Allow caddypanel user to run caddy
        if [[ -d /etc/sudoers.d ]]; then
            echo "${SERVICE_USER} ALL=(ALL) NOPASSWD: ${CADDY_BIN}" > /etc/sudoers.d/caddypanel 2>/dev/null || true
            chmod 0440 /etc/sudoers.d/caddypanel 2>/dev/null || true
        fi
    fi
}

# ==================== Start Service ====================
start_service() {
    step "Starting CaddyPanel"

    systemctl start caddypanel

    sleep 2

    if systemctl is-active --quiet caddypanel; then
        success "CaddyPanel is running!"
    else
        error "CaddyPanel failed to start. Check logs with:"
        echo "  journalctl -u caddypanel -n 50 --no-pager"
        exit 1
    fi
}

# ==================== Interactive Prompts ====================
prompt_port() {
    # Skip if port was already set via --port flag or in non-interactive mode
    if $NON_INTERACTIVE; then
        info "Panel port: $PANEL_PORT (non-interactive mode)"
        return
    fi

    echo ""
    echo -e "${BOLD}📌 Panel Port Configuration${NC}"
    echo -e "   CaddyPanel will listen on this port for the management UI."
    echo -e "   Default: ${GREEN}${PANEL_PORT}${NC}"
    echo ""

    read -t 15 -p "   Enter panel port [${PANEL_PORT}]: " INPUT_PORT || true
    echo ""

    if [[ -n "$INPUT_PORT" ]]; then
        # Validate port number
        if [[ "$INPUT_PORT" =~ ^[0-9]+$ ]] && [ "$INPUT_PORT" -ge 1 ] && [ "$INPUT_PORT" -le 65535 ]; then
            PANEL_PORT="$INPUT_PORT"
            success "Panel port set to: $PANEL_PORT"
        else
            warn "Invalid port '$INPUT_PORT', using default: $PANEL_PORT"
        fi
    else
        info "Using default port: $PANEL_PORT"
    fi
}

# ==================== Print Summary ====================
print_summary() {
    # Get local IP
    LOCAL_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "YOUR_SERVER_IP")

    echo ""
    echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}${BOLD}║           CaddyPanel Installation Complete! 🎉              ║${NC}"
    echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  ${BOLD}Panel URL:${NC}      http://${LOCAL_IP}:${PANEL_PORT}"
    echo -e "  ${BOLD}Local URL:${NC}      http://localhost:${PANEL_PORT}"
    echo ""
    echo -e "  ${BOLD}Config:${NC}         ${CONFIG_DIR}/caddypanel.env"
    echo -e "  ${BOLD}Data:${NC}           ${DATA_DIR}/"
    echo -e "  ${BOLD}Logs:${NC}           ${LOG_DIR}/"
    echo -e "  ${BOLD}Binary:${NC}         ${INSTALL_DIR}/caddypanel"
    echo ""
    echo -e "  ${BOLD}Service Commands:${NC}"
    echo -e "    systemctl status caddypanel    ${CYAN}# Check status${NC}"
    echo -e "    systemctl restart caddypanel   ${CYAN}# Restart${NC}"
    echo -e "    journalctl -u caddypanel -f    ${CYAN}# View logs${NC}"
    echo ""
    echo -e "  ${YELLOW}⚠  First visit: create your admin account at the URL above${NC}"
    echo ""
}

# ==================== Main ====================
main() {
    echo -e "${GREEN}${BOLD}"
    echo '   ____          _     _       ____                  _ '
    echo '  / ___|__ _  __| | __| |_   _|  _ \ __ _ _ __   ___| |'
    echo ' | |   / _` |/ _` |/ _` | | | | |_) / _` |  _ \ / _ \ |'
    echo ' | |__| (_| | (_| | (_| | |_| |  __/ (_| | | | |  __/ |'
    echo '  \____\__,_|\__,_|\__,_|\__, |_|   \__,_|_| |_|\___|_|'
    echo '                         |___/                          '
    echo -e "${NC}"
    echo -e "  ${BOLD}One-Click Installer v${CADDYPANEL_VERSION}${NC}"
    echo ""

    parse_args "$@"
    check_root
    detect_os

    # Auto-detect piped input (curl | bash)
    if [[ ! -t 0 ]]; then
        NON_INTERACTIVE=true
    fi

    if $UNINSTALL; then
        do_uninstall
    fi

    # Interactive port prompt
    prompt_port

    install_deps
    install_go
    install_nodejs
    install_caddy
    setup_user
    build_caddypanel
    setup_config
    setup_systemd
    setup_caddy_permissions
    setup_firewall
    start_service
    print_summary
}

main "$@"
