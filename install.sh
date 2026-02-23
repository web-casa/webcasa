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
#    --port PORT     Set panel port (default: 39921)
#    --from-source   Build from source instead of downloading pre-built binary
#    -y, --yes       Non-interactive mode (skip prompts)
# ============================================================================

set -euo pipefail

# ==================== Configuration ====================
# Auto-detect version: local VERSION file → GitHub latest release → fallback
SCRIPT_SELF="${BASH_SOURCE[0]:-}"
if [[ -n "$SCRIPT_SELF" && -f "$(dirname "$SCRIPT_SELF")/VERSION" ]]; then
    CADDYPANEL_VERSION="$(cat "$(dirname "$SCRIPT_SELF")/VERSION" | tr -d '[:space:]')"
elif command -v curl &>/dev/null; then
    CADDYPANEL_VERSION="$(curl -fsSL https://api.github.com/repos/caddypanel/caddypanel/releases/latest 2>/dev/null | grep -oP '"tag_name":\s*"v?\K[^"]+' || echo "0.4.3")"
else
    CADDYPANEL_VERSION="0.4.3"
fi
GITHUB_REPO="caddypanel/caddypanel"
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
FROM_SOURCE=false

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
                fatal "Unsupported OS: $OS_NAME ($OS_ID)."
            fi
            ;;
    esac

    # Detect architecture
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)     ARCH_SUFFIX="linux-amd64"; GO_ARCH="amd64" ;;
        aarch64)    ARCH_SUFFIX="linux-arm64"; GO_ARCH="arm64" ;;
        *)          fatal "Unsupported architecture: $ARCH" ;;
    esac

    info "Detected OS: ${BOLD}$OS_NAME${NC} ($OS_FAMILY/$ARCH)"
}

# ==================== Parse Arguments ====================
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --uninstall)    UNINSTALL=true; shift ;;
            --purge)        PURGE=true; UNINSTALL=true; shift ;;
            --no-caddy)     SKIP_CADDY=true; shift ;;
            --port)         PANEL_PORT="$2"; shift 2 ;;
            --from-source)  FROM_SOURCE=true; shift ;;
            -y|--yes)       NON_INTERACTIVE=true; shift ;;
            -h|--help)      usage; exit 0 ;;
            *)              warn "Unknown option: $1"; shift ;;
        esac
    done
}

usage() {
    cat <<EOF
${BOLD}CaddyPanel Installer v${CADDYPANEL_VERSION}${NC}

Usage: bash install.sh [OPTIONS]

Options:
  --uninstall      Remove CaddyPanel (keeps data)
  --purge          Remove CaddyPanel and all data
  --no-caddy       Skip Caddy installation
  --port PORT      Set panel port (default: 39921)
  --from-source    Build from source (requires Go + Node.js)
  -y, --yes        Non-interactive mode (skip prompts)
  -h, --help       Show this help

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
install_deps() {
    step "Installing base dependencies"
    case "$OS_FAMILY" in
        debian)
            apt-get update -qq
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
                curl wget ca-certificates tar gzip > /dev/null
            ;;
        rhel)
            if command -v dnf &>/dev/null; then
                dnf install -y -q curl wget ca-certificates tar gzip which
            else
                yum install -y -q curl wget ca-certificates tar gzip which
            fi
            ;;
    esac
    success "Dependencies installed"
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

    # Stop the default caddy service (CaddyPanel manages it)
    systemctl stop caddy 2>/dev/null || true
    systemctl disable caddy 2>/dev/null || true

    success "Caddy $(caddy version 2>/dev/null || echo '') installed"
}

# ==================== Install CaddyPanel (Download Pre-built) ====================
install_prebuilt() {
    step "Installing CaddyPanel v${CADDYPANEL_VERSION}"

    # Check GLIBC version (pre-built binary requires >= 2.32)
    local GLIBC_VER
    GLIBC_VER=$(ldd --version 2>&1 | head -1 | grep -oP '\d+\.\d+$' || echo "0.0")
    local GLIBC_MAJOR="${GLIBC_VER%%.*}"
    local GLIBC_MINOR="${GLIBC_VER##*.}"
    if [[ "$GLIBC_MAJOR" -lt 2 ]] || { [[ "$GLIBC_MAJOR" -eq 2 ]] && [[ "$GLIBC_MINOR" -lt 32 ]]; }; then
        error "系统 GLIBC 版本为 ${GLIBC_VER}，预编译二进制需要 GLIBC >= 2.32"
        error "AlmaLinux/CentOS/RHEL 8 等旧发行版不支持预编译安装"
        error "请升级到 RHEL 9 系列或 Ubuntu 22.04+，或使用源码编译："
        error "  bash install.sh --from-source"
        exit 1
    fi

    # Determine download URL
    local TARBALL="caddypanel-${ARCH_SUFFIX}.tar.gz"
    local URL="https://github.com/${GITHUB_REPO}/releases/download/v${CADDYPANEL_VERSION}/${TARBALL}"

    info "Downloading from ${URL} ..."
    wget -q --show-progress -O "/tmp/${TARBALL}" "$URL" || {
        error "Download failed. The release v${CADDYPANEL_VERSION} may not exist."
        error "Try: bash install.sh --from-source"
        exit 1
    }

    # Verify checksum if available
    local SHA_URL="${URL}.sha256"
    if wget -q -O "/tmp/${TARBALL}.sha256" "$SHA_URL" 2>/dev/null; then
        info "Verifying checksum..."
        cd /tmp && sha256sum -c "${TARBALL}.sha256" && success "Checksum verified" || warn "Checksum mismatch!"
    fi

    # Extract
    info "Extracting..."
    tar -xzf "/tmp/${TARBALL}" -C /tmp/

    local EXTRACT_DIR="/tmp/caddypanel-${ARCH_SUFFIX}"

    # Install binary
    cp -f "${EXTRACT_DIR}/caddypanel" "$INSTALL_DIR/caddypanel"
    chmod 755 "$INSTALL_DIR/caddypanel"

    # Install frontend
    mkdir -p "$DATA_DIR/web"
    cp -r "${EXTRACT_DIR}/web/dist" "$DATA_DIR/web/"

    # Cleanup
    rm -rf "/tmp/${TARBALL}" "/tmp/${TARBALL}.sha256" "$EXTRACT_DIR"

    success "CaddyPanel v${CADDYPANEL_VERSION} installed"
}

# ==================== Install CaddyPanel (Build from Source) ====================
install_from_source() {
    step "Building CaddyPanel from source"

    # Install Go
    install_go

    # Install Node.js
    install_nodejs

    # Install build tools
    case "$OS_FAMILY" in
        debian) DEBIAN_FRONTEND=noninteractive apt-get install -y -qq gcc make git > /dev/null ;;
        rhel)
            if command -v dnf &>/dev/null; then
                dnf install -y -q gcc make git
            else
                yum install -y -q gcc make git
            fi
            ;;
    esac

    # Determine source directory
    SCRIPT_DIR="$(cd "$(dirname "${SCRIPT_SELF:-$0}")" && pwd)"

    if [[ -f "$SCRIPT_DIR/main.go" ]]; then
        SRC_DIR="$SCRIPT_DIR"
    elif [[ -f "$SCRIPT_DIR/../main.go" ]]; then
        SRC_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
    else
        info "Cloning CaddyPanel source..."
        SRC_DIR="/tmp/caddypanel-build"
        rm -rf "$SRC_DIR"
        git clone --depth 1 https://github.com/${GITHUB_REPO}.git "$SRC_DIR"
    fi

    info "Source directory: $SRC_DIR"

    # Build frontend
    info "Building frontend..."
    cd "$SRC_DIR/web"
    npm install --unsafe-perm --loglevel=warn 2>&1
    npm run build 2>&1
    success "Frontend built"

    # Build backend
    info "Building Go backend..."
    cd "$SRC_DIR"
    export PATH=$PATH:/usr/local/go/bin
    export CGO_ENABLED=1
    go build -ldflags="-s -w -X main.Version=${CADDYPANEL_VERSION}" -o caddypanel .
    success "Backend built"

    # Install binary
    cp -f caddypanel "$INSTALL_DIR/caddypanel"
    chmod 755 "$INSTALL_DIR/caddypanel"

    # Install frontend
    mkdir -p "$DATA_DIR/web/dist"
    cp -r web/dist/* "$DATA_DIR/web/dist/"

    success "CaddyPanel v${CADDYPANEL_VERSION} installed"
}

install_go() {
    local GO_MAJOR="1.26"

    if command -v go &>/dev/null; then
        CURRENT_GO=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
        if printf '%s\n%s' "1.22" "$CURRENT_GO" | sort -V | head -1 | grep -q "^1.22$"; then
            info "Go $CURRENT_GO already installed"
            return
        fi
    fi

    # Auto-detect latest patch version
    info "Fetching latest Go ${GO_MAJOR}.x version..."
    local GO_VERSION
    GO_VERSION=$(curl -fsSL "https://go.dev/dl/?mode=json" | \
        grep -oP "go${GO_MAJOR}\.[0-9]+" | head -1 | sed 's/^go//')

    if [[ -z "$GO_VERSION" ]]; then
        # Fallback to .0
        GO_VERSION="${GO_MAJOR}.0"
        warn "Could not detect latest Go version, falling back to $GO_VERSION"
    fi

    info "Installing Go $GO_VERSION ..."
    local GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    wget -q --show-progress -O "/tmp/${GO_TAR}" "https://go.dev/dl/${GO_TAR}"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${GO_TAR}"
    rm -f "/tmp/${GO_TAR}"
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
    export PATH=$PATH:/usr/local/go/bin
    success "Go $(go version | grep -oP 'go\K\S+') installed"
}

install_nodejs() {
    local NODE_MAJOR="24"

    if command -v node &>/dev/null; then
        NODE_VER=$(node --version | tr -d 'v' | cut -d. -f1)
        if [[ "$NODE_VER" -ge "$NODE_MAJOR" ]]; then
            info "Node.js $(node --version) already installed"
            return
        fi
    fi

    info "Fetching latest Node.js v${NODE_MAJOR}.x ..."
    local NODE_FULL_VER
    NODE_FULL_VER=$(curl -fsSL "https://nodejs.org/dist/latest-v${NODE_MAJOR}.x/" \
        | grep -oP 'node-v\K[0-9]+\.[0-9]+\.[0-9]+' | head -1)

    [[ -z "$NODE_FULL_VER" ]] && fatal "Failed to fetch Node.js version"

    # Node.js uses "x64" not "amd64"
    local NODE_ARCH="$GO_ARCH"
    [[ "$NODE_ARCH" == "amd64" ]] && NODE_ARCH="x64"

    local NODE_TAR="node-v${NODE_FULL_VER}-linux-${NODE_ARCH}.tar.xz"
    info "Downloading Node.js v${NODE_FULL_VER} ..."
    wget -q --show-progress -O "/tmp/${NODE_TAR}" "https://nodejs.org/dist/v${NODE_FULL_VER}/${NODE_TAR}"
    tar -C /usr/local --strip-components=1 -xJf "/tmp/${NODE_TAR}"
    rm -f "/tmp/${NODE_TAR}"
    hash -r
    success "Node.js $(node --version) installed"
}

# ==================== Setup System ====================
setup_user() {
    step "Setting up system user and directories"

    if ! id "$SERVICE_USER" &>/dev/null; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
        info "Created system user: $SERVICE_USER"
    else
        info "User $SERVICE_USER already exists"
    fi

    mkdir -p "$DATA_DIR"/{backups,web/dist}
    mkdir -p "$LOG_DIR"
    mkdir -p "$CONFIG_DIR"

    chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR"
    chown -R "$SERVICE_USER:$SERVICE_USER" "$LOG_DIR"

    success "Directories created"
}

setup_config() {
    step "Configuring CaddyPanel"

    ENV_FILE="$CONFIG_DIR/caddypanel.env"

    if [[ -f "$ENV_FILE" ]]; then
        info "Config file already exists, preserving: $ENV_FILE"
    else
        JWT_SECRET=$(head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 48)
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
        success "Config written to $ENV_FILE"
    fi

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

    cat > /etc/systemd/system/caddypanel.service <<SVCEOF
[Unit]
Description=CaddyPanel - Caddy Reverse Proxy Management Panel
Documentation=https://github.com/${GITHUB_REPO}
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
        ufw allow "$PANEL_PORT"/tcp comment "CaddyPanel" > /dev/null 2>&1 || true
        ufw allow 80/tcp comment "HTTP" > /dev/null 2>&1 || true
        ufw allow 443/tcp comment "HTTPS" > /dev/null 2>&1 || true
        success "UFW rules added (ports $PANEL_PORT, 80, 443)"
    elif command -v firewall-cmd &>/dev/null; then
        firewall-cmd --permanent --add-port="${PANEL_PORT}/tcp" > /dev/null 2>&1 || true
        firewall-cmd --permanent --add-service=http > /dev/null 2>&1 || true
        firewall-cmd --permanent --add-service=https > /dev/null 2>&1 || true
        firewall-cmd --reload > /dev/null 2>&1 || true
        success "Firewalld rules added (ports $PANEL_PORT, 80, 443)"
    else
        warn "No firewall detected. Make sure ports $PANEL_PORT, 80, 443 are open."
    fi
}

setup_caddy_permissions() {
    if $SKIP_CADDY; then return; fi

    CADDY_BIN=$(command -v caddy 2>/dev/null || echo "")
    if [[ -n "$CADDY_BIN" ]]; then
        setcap 'cap_net_bind_service=+ep' "$CADDY_BIN" 2>/dev/null || true
        if [[ -d /etc/sudoers.d ]]; then
            echo "${SERVICE_USER} ALL=(ALL) NOPASSWD: ${CADDY_BIN}" > /etc/sudoers.d/caddypanel 2>/dev/null || true
            chmod 0440 /etc/sudoers.d/caddypanel 2>/dev/null || true
        fi
    fi
}

# ==================== Interactive Prompts ====================
prompt_port() {
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

# ==================== Detect Public IP ====================
detect_public_ip() {
    step "Detecting public IP addresses"

    PUBLIC_IPV4=""
    PUBLIC_IPV6=""

    # Detect IPv4 (priority: icanhazip → api.ip.sb → ifconfig.me → Cloudflare trace)
    for svc in "https://ipv4.icanhazip.com" "https://api.ip.sb/ip" "https://ifconfig.me/ip"; do
        PUBLIC_IPV4=$(curl -4 -fsSL --connect-timeout 3 --max-time 5 "$svc" 2>/dev/null | tr -d '[:space:]')
        if [[ -n "$PUBLIC_IPV4" ]]; then break; fi
    done
    # Fallback: Cloudflare trace (parse ip= field)
    if [[ -z "$PUBLIC_IPV4" ]]; then
        PUBLIC_IPV4=$(curl -4 -fsSL --connect-timeout 3 --max-time 5 "https://1.1.1.1/cdn-cgi/trace" 2>/dev/null | grep -oP '^ip=\K.*')
    fi

    # Detect IPv6 (priority: icanhazip → api.ip.sb → Cloudflare trace)
    for svc in "https://ipv6.icanhazip.com" "https://api.ip.sb/ip" "https://ifconfig.me/ip"; do
        PUBLIC_IPV6=$(curl -6 -fsSL --connect-timeout 3 --max-time 5 "$svc" 2>/dev/null | tr -d '[:space:]')
        if [[ -n "$PUBLIC_IPV6" ]]; then break; fi
    done
    if [[ -z "$PUBLIC_IPV6" ]]; then
        PUBLIC_IPV6=$(curl -6 -fsSL --connect-timeout 3 --max-time 5 "https://[2606:4700:4700::1111]/cdn-cgi/trace" 2>/dev/null | grep -oP '^ip=\K.*')
    fi

    # Write to SQLite settings table
    local DB_PATH="${DATA_DIR}/caddypanel.db"
    if command -v sqlite3 &>/dev/null && [[ -f "$DB_PATH" ]]; then
        if [[ -n "$PUBLIC_IPV4" ]]; then
            sqlite3 "$DB_PATH" "INSERT OR REPLACE INTO settings (key, value) VALUES ('server_ipv4', '$PUBLIC_IPV4');" 2>/dev/null || true
        fi
        if [[ -n "$PUBLIC_IPV6" ]]; then
            sqlite3 "$DB_PATH" "INSERT OR REPLACE INTO settings (key, value) VALUES ('server_ipv6', '$PUBLIC_IPV6');" 2>/dev/null || true
        fi
    fi

    if [[ -n "$PUBLIC_IPV4" ]]; then
        success "IPv4: $PUBLIC_IPV4"
    else
        warn "IPv4 not detected"
    fi
    if [[ -n "$PUBLIC_IPV6" ]]; then
        success "IPv6: $PUBLIC_IPV6"
    else
        info "IPv6 not available"
    fi
}

# ==================== Print Summary ====================
print_summary() {
    # Use detected public IP, fallback to LAN IP
    local DISPLAY_IP="${PUBLIC_IPV4:-$(hostname -I 2>/dev/null | awk '{print $1}' || echo "YOUR_SERVER_IP")}"

    echo ""
    echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}${BOLD}║           CaddyPanel Installation Complete! 🎉              ║${NC}"
    echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  ${BOLD}Panel URL:${NC}      http://${DISPLAY_IP}:${PANEL_PORT}"
    echo -e "  ${BOLD}Local URL:${NC}      http://localhost:${PANEL_PORT}"
    if [[ -n "$PUBLIC_IPV4" ]]; then
        echo -e "  ${BOLD}IPv4:${NC}           ${PUBLIC_IPV4}"
    fi
    if [[ -n "$PUBLIC_IPV6" ]]; then
        echo -e "  ${BOLD}IPv6:${NC}           ${PUBLIC_IPV6}"
    fi
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
    install_caddy
    setup_user

    # Install CaddyPanel binary + frontend
    if $FROM_SOURCE; then
        info "Building from source (--from-source)"
        install_from_source
    else
        install_prebuilt
    fi

    setup_config
    setup_systemd
    setup_caddy_permissions
    setup_firewall
    start_service
    detect_public_ip
    print_summary
}

main "$@"
