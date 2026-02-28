#!/usr/bin/env bash
# ============================================================================
#  Web.Casa Install Script Verification
#  Tests install.sh --from-source in fresh Docker containers
#
#  Usage:
#    bash scripts/test-install.sh              # Test all distros
#    bash scripts/test-install.sh ubuntu       # Test specific distro
#    bash scripts/test-install.sh debian alma  # Test multiple
#
#  Available distros: ubuntu, debian, alma, rocky, fedora
# ============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Capture node path before potential sudo PATH change
NODE_BIN="${NODE_BIN:-$(which node 2>/dev/null || echo "node")}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

pass() { echo -e "  ${GREEN}PASS${NC} $1"; PASS_COUNT=$((PASS_COUNT+1)); }
fail() { echo -e "  ${RED}FAIL${NC} $1"; FAIL_COUNT=$((FAIL_COUNT+1)); }
skip() { echo -e "  ${YELLOW}SKIP${NC} $1"; SKIP_COUNT=$((SKIP_COUNT+1)); }

# Distro → base image mapping
declare -A DISTRO_IMAGES=(
    [ubuntu]="ubuntu:22.04"
    [debian]="debian:12"
    [alma]="almalinux:9"
    [rocky]="rockylinux:9"
    [fedora]="fedora:40"
)

# ==================== Build Test Image ====================
build_test_image() {
    local DISTRO="$1"
    local BASE_IMAGE="${DISTRO_IMAGES[$DISTRO]}"
    local TAG="webcasa-install-test-${DISTRO}"

    echo -e "\n${CYAN}${BOLD}Building test image for ${DISTRO} (${BASE_IMAGE})...${NC}"

    # Determine package manager setup
    local SETUP_CMD
    case "$DISTRO" in
        ubuntu|debian)
            SETUP_CMD="apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl wget sudo > /dev/null"
            ;;
        alma|rocky|fedora)
            # AlmaLinux/Rocky ship curl-minimal which conflicts with curl; swap it first
            SETUP_CMD="dnf swap -y curl-minimal curl > /dev/null 2>&1 || true; dnf install -y -q curl wget sudo which procps-ng > /dev/null 2>&1"
            ;;
    esac

    docker build --no-cache -t "$TAG" -f - "$PROJECT_DIR" <<DOCKERFILE
FROM ${BASE_IMAGE}

# Install minimal bootstrap dependencies
RUN ${SETUP_CMD}

# Copy project source
COPY . /src

WORKDIR /src
DOCKERFILE

    echo "$TAG"
}

# ==================== Run Install Test ====================
run_install_test() {
    local DISTRO="$1"
    local TAG="webcasa-install-test-${DISTRO}"
    local CONTAINER="webcasa-install-test-${DISTRO}-$$"
    local API="http://localhost:39921"

    echo -e "\n${CYAN}${BOLD}============================================${NC}"
    echo -e "${CYAN}${BOLD}  Testing install.sh on ${DISTRO}${NC}"
    echo -e "${CYAN}${BOLD}============================================${NC}"

    # Start container in background
    docker run -d \
        --name "$CONTAINER" \
        --tmpfs /run \
        -p 0:39921 \
        "$TAG" \
        sleep 3600

    # Get mapped port
    local HOST_PORT
    HOST_PORT=$(docker port "$CONTAINER" 39921 | head -1 | cut -d: -f2)
    API="http://localhost:${HOST_PORT}"

    echo -e "${YELLOW}[1/5] Running install.sh --from-source -y ...${NC}"

    # Run install script inside container (skip systemctl since no systemd in Docker)
    # We create a fake systemctl that simulates success
    docker exec "$CONTAINER" bash -c '
        # Create fake systemctl that simulates a working service
        cat > /usr/local/bin/systemctl <<'"'"'FAKESC'"'"'
#!/bin/bash
echo "$@" >> /tmp/systemctl-log/calls.log
case "$1" in
    is-active)  exit 0 ;;  # pretend service is running
    is-enabled) exit 0 ;;
    start|stop|enable|disable|daemon-reload) exit 0 ;;
    *)          exit 0 ;;
esac
FAKESC
        chmod +x /usr/local/bin/systemctl
        mkdir -p /tmp/systemctl-log
    '

    # Run install script (redirect systemctl, override start_service)
    local INSTALL_EXIT=0
    docker exec "$CONTAINER" bash -c '
        cd /src
        # Patch start_service to just record and not fail
        # The fake systemctl handles it
        bash install.sh --from-source -y --no-caddy 2>&1
    ' || INSTALL_EXIT=$?

    if [[ $INSTALL_EXIT -ne 0 ]]; then
        fail "install.sh exited with code $INSTALL_EXIT"
        # Show last 30 lines of output for debugging
        docker logs "$CONTAINER" 2>&1 | tail -30
        cleanup_container "$CONTAINER" "$TAG"
        return
    fi
    pass "install.sh completed successfully"

    # ---- Verify installation artifacts ----
    echo -e "${YELLOW}[2/5] Verifying installation artifacts...${NC}"

    # Binary exists
    docker exec "$CONTAINER" test -f /usr/local/bin/webcasa && \
        pass "Binary at /usr/local/bin/webcasa" || fail "Binary missing"

    # Binary is executable and responds to --version
    local VERSION_OUT
    VERSION_OUT=$(docker exec "$CONTAINER" /usr/local/bin/webcasa --version 2>&1) && \
        pass "Binary --version: $VERSION_OUT" || fail "Binary --version failed"

    # Config file exists
    docker exec "$CONTAINER" test -f /etc/webcasa/webcasa.env && \
        pass "Config at /etc/webcasa/webcasa.env" || fail "Config missing"

    # Config contains required variables
    docker exec "$CONTAINER" grep -q "WEBCASA_JWT_SECRET" /etc/webcasa/webcasa.env && \
        pass "Config has JWT_SECRET" || fail "Config missing JWT_SECRET"

    docker exec "$CONTAINER" grep -q "GIN_MODE=release" /etc/webcasa/webcasa.env && \
        pass "Config has GIN_MODE=release" || fail "Config missing GIN_MODE"

    # Data directories
    docker exec "$CONTAINER" test -d /var/lib/webcasa && \
        pass "Data dir /var/lib/webcasa" || fail "Data dir missing"

    docker exec "$CONTAINER" test -d /var/lib/webcasa/plugins/filemanager && \
        pass "Plugin dir plugins/filemanager" || fail "Plugin dir filemanager missing"

    docker exec "$CONTAINER" test -d /var/lib/webcasa/plugins/docker && \
        pass "Plugin dir plugins/docker" || fail "Plugin dir docker missing"

    docker exec "$CONTAINER" test -d /var/lib/webcasa/plugins/deploy && \
        pass "Plugin dir plugins/deploy" || fail "Plugin dir deploy missing"

    docker exec "$CONTAINER" test -d /var/lib/webcasa/plugins/ai && \
        pass "Plugin dir plugins/ai" || fail "Plugin dir ai missing"

    # Frontend files
    docker exec "$CONTAINER" test -f /var/lib/webcasa/web/dist/index.html && \
        pass "Frontend dist/index.html" || fail "Frontend files missing"

    # Log directory
    docker exec "$CONTAINER" test -d /var/log/webcasa && \
        pass "Log dir /var/log/webcasa" || fail "Log dir missing"

    # Systemd service file
    docker exec "$CONTAINER" test -f /etc/systemd/system/webcasa.service && \
        pass "Systemd service file" || fail "Systemd service file missing"

    # Verify systemd service runs as root (Pro requirement)
    docker exec "$CONTAINER" grep -q "User=root" /etc/systemd/system/webcasa.service && \
        pass "Service runs as root" || fail "Service not running as root"

    # Verify no ProtectHome (Pro requirement: file manager needs /home access)
    if docker exec "$CONTAINER" grep -q "ProtectHome" /etc/systemd/system/webcasa.service 2>/dev/null; then
        fail "Service still has ProtectHome (blocks file manager)"
    else
        pass "No ProtectHome restriction"
    fi

    # Verify no ProtectSystem=strict
    if docker exec "$CONTAINER" grep -q "ProtectSystem=strict" /etc/systemd/system/webcasa.service 2>/dev/null; then
        fail "Service still has ProtectSystem=strict"
    else
        pass "No ProtectSystem=strict restriction"
    fi

    # Git is installed (needed by Deploy plugin)
    docker exec "$CONTAINER" which git > /dev/null 2>&1 && \
        pass "git installed" || fail "git not installed"

    # Bash is installed (needed by Web Terminal)
    docker exec "$CONTAINER" which bash > /dev/null 2>&1 && \
        pass "bash installed" || fail "bash not installed"

    # Caddyfile exists
    docker exec "$CONTAINER" test -f /var/lib/webcasa/Caddyfile && \
        pass "Default Caddyfile" || fail "Caddyfile missing"

    # ---- Start binary and test API ----
    echo -e "${YELLOW}[3/5] Starting WebCasa binary...${NC}"

    docker exec -d "$CONTAINER" bash -c '
        source /etc/webcasa/webcasa.env
        export WEBCASA_PORT WEBCASA_DATA_DIR WEBCASA_DB_PATH WEBCASA_JWT_SECRET
        export WEBCASA_LOG_DIR GIN_MODE
        export WEBCASA_CADDY_BIN=/bin/false
        cd /var/lib/webcasa
        /usr/local/bin/webcasa > /tmp/webcasa.log 2>&1
    '

    # Wait for API
    local READY=false
    for i in $(seq 1 30); do
        if docker exec "$CONTAINER" curl -sf http://localhost:39921/api/auth/need-setup > /dev/null 2>&1; then
            READY=true
            pass "API ready after ${i}s"
            break
        fi
        sleep 1
    done

    if ! $READY; then
        fail "API not ready after 30s"
        docker exec "$CONTAINER" cat /tmp/webcasa.log 2>/dev/null | tail -20
        cleanup_container "$CONTAINER" "$TAG"
        return
    fi

    # ---- Setup admin & login ----
    echo -e "${YELLOW}[4/5] Setting up admin and logging in...${NC}"

    # Admin setup
    local SETUP_RES
    SETUP_RES=$(docker exec "$CONTAINER" curl -sf -X POST http://localhost:39921/api/auth/setup \
        -H 'Content-Type: application/json' \
        -d '{"username":"admin","password":"TestPass123!"}' 2>&1) && \
        pass "Admin setup" || fail "Admin setup failed: $SETUP_RES"

    # Solve ALTCHA and login
    local CHALLENGE_JSON
    CHALLENGE_JSON=$(docker exec "$CONTAINER" curl -sf http://localhost:39921/api/auth/altcha-challenge)
    local SALT CHALLENGE SIGNATURE
    SALT=$(echo "$CHALLENGE_JSON" | docker exec -i "$CONTAINER" jq -r '.salt')
    CHALLENGE=$(echo "$CHALLENGE_JSON" | docker exec -i "$CONTAINER" jq -r '.challenge')
    SIGNATURE=$(echo "$CHALLENGE_JSON" | docker exec -i "$CONTAINER" jq -r '.signature')

    # Solve PoW (node runs on host, not in container)
    local NUMBER
    NUMBER=$(SALT="$SALT" CHALLENGE="$CHALLENGE" "$NODE_BIN" -e "
        const crypto = require('crypto');
        const salt = process.env.SALT;
        const challenge = process.env.CHALLENGE;
        for(let i=0; i<=50000; i++) {
            if(crypto.createHash('sha256').update(salt+i).digest('hex') === challenge) {
                console.log(i);
                process.exit(0);
            }
        }
        process.exit(1);
    ")

    local ALTCHA_B64
    ALTCHA_B64=$(echo -n "{\"algorithm\":\"SHA-256\",\"challenge\":\"$CHALLENGE\",\"number\":$NUMBER,\"salt\":\"$SALT\",\"signature\":\"$SIGNATURE\"}" | base64 -w0)

    local TOKEN
    TOKEN=$(docker exec "$CONTAINER" curl -sf -X POST http://localhost:39921/api/auth/login \
        -H 'Content-Type: application/json' \
        -d "{\"username\":\"admin\",\"password\":\"TestPass123!\",\"altcha\":\"$ALTCHA_B64\"}" | \
        docker exec -i "$CONTAINER" jq -r '.token')

    if [[ -n "$TOKEN" && "$TOKEN" != "null" ]]; then
        pass "Login successful"
    else
        fail "Login failed"
        cleanup_container "$CONTAINER" "$TAG"
        return
    fi

    # ---- Test plugin endpoints ----
    echo -e "${YELLOW}[5/5] Testing plugin endpoints...${NC}"

    # Helper
    auth_get() {
        docker exec "$CONTAINER" curl -sf "http://localhost:39921$1" \
            -H "Authorization: Bearer $TOKEN"
    }

    # Plugin list
    local PLUGIN_COUNT
    PLUGIN_COUNT=$(auth_get "/api/plugins" | docker exec -i "$CONTAINER" jq '.plugins | length')
    if [[ "$PLUGIN_COUNT" -ge 4 ]]; then
        pass "Plugin list: $PLUGIN_COUNT plugins registered"
    else
        fail "Plugin list: only $PLUGIN_COUNT plugins (expected >= 4)"
    fi

    # Frontend manifests
    local MANIFEST_COUNT
    MANIFEST_COUNT=$(auth_get "/api/plugins/frontend-manifests" | docker exec -i "$CONTAINER" jq '. | length')
    if [[ "$MANIFEST_COUNT" -ge 4 ]]; then
        pass "Frontend manifests: $MANIFEST_COUNT"
    else
        fail "Frontend manifests: only $MANIFEST_COUNT (expected >= 4)"
    fi

    # File Manager: mkdir + list
    auth_get "/api/plugins/filemanager/list?path=/tmp" > /dev/null && \
        pass "File Manager: list /tmp" || fail "File Manager: list failed"

    # Deploy: list frameworks
    local FW_COUNT
    FW_COUNT=$(auth_get "/api/plugins/deploy/frameworks" | docker exec -i "$CONTAINER" jq '. | length')
    if [[ "$FW_COUNT" -ge 1 ]]; then
        pass "Deploy: $FW_COUNT frameworks"
    else
        fail "Deploy: no frameworks"
    fi

    # AI: get config
    auth_get "/api/plugins/ai/config" > /dev/null && \
        pass "AI: get config" || fail "AI: get config failed"

    # Docker: info endpoint (may fail if no Docker daemon, that's OK)
    local DOCKER_HTTP
    DOCKER_HTTP=$(docker exec "$CONTAINER" curl -s -o /dev/null -w '%{http_code}' \
        "http://localhost:39921/api/plugins/docker/info" \
        -H "Authorization: Bearer $TOKEN" 2>/dev/null || echo "000")
    if [[ "$DOCKER_HTTP" == "200" || "$DOCKER_HTTP" == "500" ]]; then
        pass "Docker: info endpoint exists (HTTP $DOCKER_HTTP)"
    else
        skip "Docker: endpoint unreachable (HTTP $DOCKER_HTTP)"
    fi

    # ---- Cleanup ----
    cleanup_container "$CONTAINER" "$TAG"
}

cleanup_container() {
    local CONTAINER="$1"
    local TAG="$2"
    echo -e "${YELLOW}Cleaning up ${CONTAINER}...${NC}"
    docker rm -f "$CONTAINER" > /dev/null 2>&1 || true
    docker rmi -f "$TAG" > /dev/null 2>&1 || true
}

# ==================== Main ====================
main() {
    echo -e "${CYAN}${BOLD}============================================${NC}"
    echo -e "${CYAN}${BOLD}  Web.Casa Install Script Verification${NC}"
    echo -e "${CYAN}${BOLD}============================================${NC}"

    cd "$PROJECT_DIR"

    # Determine which distros to test
    local DISTROS=()
    if [[ $# -gt 0 ]]; then
        DISTROS=("$@")
    else
        # Default: test Ubuntu (Debian-family) and AlmaLinux (RHEL-family)
        DISTROS=("ubuntu" "alma")
    fi

    # Validate distro names
    for d in "${DISTROS[@]}"; do
        if [[ -z "${DISTRO_IMAGES[$d]:-}" ]]; then
            echo -e "${RED}Unknown distro: $d${NC}"
            echo "Available: ${!DISTRO_IMAGES[*]}"
            exit 1
        fi
    done

    # Build and test each distro
    for d in "${DISTROS[@]}"; do
        build_test_image "$d"
        run_install_test "$d"
    done

    # Summary
    local TOTAL=$((PASS_COUNT + FAIL_COUNT + SKIP_COUNT))
    echo ""
    echo -e "${CYAN}${BOLD}============================================${NC}"
    echo -e "  Results: ${GREEN}${PASS_COUNT} passed${NC}, ${RED}${FAIL_COUNT} failed${NC}, ${YELLOW}${SKIP_COUNT} skipped${NC}, ${TOTAL} total"
    echo -e "${CYAN}${BOLD}============================================${NC}"

    if [[ $FAIL_COUNT -gt 0 ]]; then
        echo -e "\n${RED}Some tests failed!${NC}"
        exit 1
    else
        echo -e "\n${GREEN}All tests passed!${NC}"
    fi
}

main "$@"
