#!/usr/bin/env bash
# scripts/test-local.sh — Self-contained local Docker integration test
# Usage: bash scripts/test-local.sh
#   or:  sudo bash scripts/test-local.sh
set -euo pipefail

# Ensure node/jq are in PATH (nvm may not be inherited under sudo)
for p in /usr/local/bin /usr/bin "$HOME/.nvm/versions/node"/*/bin; do
    [ -d "$p" ] && export PATH="$p:$PATH"
done
# Also check the invoking user's nvm if running as sudo
if [ -n "${SUDO_USER:-}" ]; then
    SUDO_HOME=$(eval echo "~$SUDO_USER")
    for p in "$SUDO_HOME/.nvm/versions/node"/*/bin; do
        [ -d "$p" ] && export PATH="$p:$PATH"
    done
fi

# Check dependencies
for cmd in docker jq node curl; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "Error: '$cmd' is required but not found in PATH."
        exit 1
    fi
done

IMAGE_NAME="webcasa-test"
CONTAINER_NAME="webcasa-test-$$"
HOST_PORT=39921
PASS=0
FAIL=0
TOTAL=0

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
    docker rmi "$IMAGE_NAME" 2>/dev/null || true
}
trap cleanup EXIT

assert() {
    local name="$1"
    local result="$2"
    TOTAL=$((TOTAL + 1))
    if [ "$result" = "0" ]; then
        echo -e "  ${GREEN}PASS${NC} $name"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${NC} $name"
        FAIL=$((FAIL + 1))
    fi
}

echo "============================================"
echo "  WebCasa Local Docker Integration Test"
echo "============================================"

# Step 1: Build Docker image
echo -e "\n${YELLOW}[1/6] Building Docker image...${NC}"
docker build -t "$IMAGE_NAME" . || { echo "Build failed!"; exit 1; }
echo -e "${GREEN}Image built.${NC}"

# Step 2: Start container
echo -e "\n${YELLOW}[2/6] Starting container...${NC}"
docker run -d --name "$CONTAINER_NAME" \
    -p "$HOST_PORT:8080" \
    -e WEBCASA_PORT=8080 \
    -e WEBCASA_JWT_SECRET=test-secret-local \
    -e GIN_MODE=release \
    "$IMAGE_NAME"

# Step 3: Wait for API
echo -e "\n${YELLOW}[3/6] Waiting for API to be ready...${NC}"
API="http://localhost:$HOST_PORT"
for i in $(seq 1 30); do
    if curl -sf "$API/api/auth/need-setup" > /dev/null 2>&1; then
        echo -e "${GREEN}API ready after ${i}s${NC}"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo -e "${RED}API did not start in 30s!${NC}"
        docker logs "$CONTAINER_NAME"
        exit 1
    fi
    sleep 1
done

# Step 4: Setup admin account
echo -e "\n${YELLOW}[4/6] Setting up admin account...${NC}"
HTTP_CODE=$(curl -s -o /tmp/setup.json -w "%{http_code}" \
    -X POST "$API/api/auth/setup" \
    -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"testpassword123"}')
assert "Admin setup" "$([ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "201" ]; echo $?)"

# Step 5: Login (solve ALTCHA PoW)
echo -e "\n${YELLOW}[5/6] Logging in (solving ALTCHA PoW)...${NC}"
CHALLENGE_JSON=$(curl -sf "$API/api/auth/altcha-challenge")
CHALLENGE=$(echo "$CHALLENGE_JSON" | jq -r '.challenge')
SALT=$(echo "$CHALLENGE_JSON" | jq -r '.salt')
ALGORITHM=$(echo "$CHALLENGE_JSON" | jq -r '.algorithm')
SIGNATURE=$(echo "$CHALLENGE_JSON" | jq -r '.signature')

NUMBER=$(SALT="$SALT" CHALLENGE="$CHALLENGE" node -e "
const crypto = require('crypto');
const salt = process.env.SALT;
const challenge = process.env.CHALLENGE;
for(let i=0; i<=50000; i++) {
    if(crypto.createHash('sha256').update(salt + i).digest('hex') === challenge) {
        console.log(i);
        process.exit(0);
    }
}
process.exit(1);
")

PAYLOAD_JSON=$(jq -n \
    --arg alg "$ALGORITHM" \
    --arg chal "$CHALLENGE" \
    --argjson num "$NUMBER" \
    --arg salt "$SALT" \
    --arg sig "$SIGNATURE" \
    '{algorithm: $alg, challenge: $chal, number: $num, salt: $salt, signature: $sig}')
ALTCHA_B64=$(echo -n "$PAYLOAD_JSON" | base64 -w 0)

RESPONSE=$(curl -sf -X POST "$API/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"testpassword123\",\"altcha\":\"$ALTCHA_B64\"}")
TOKEN=$(echo "$RESPONSE" | jq -r '.token')
assert "Login" "$([ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; echo $?)"
AUTH="-H \"Authorization: Bearer $TOKEN\""

# Helper for authenticated requests
auth_get() { curl -sf "$API$1" -H "Authorization: Bearer $TOKEN"; }
auth_post() { curl -sf -X POST "$API$1" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$2"; }
auth_delete() { curl -sf -X DELETE "$API$1" -H "Authorization: Bearer $TOKEN"; }

# Step 6: Run tests
echo -e "\n${YELLOW}[6/6] Running integration tests...${NC}"

echo -e "\n  --- Core API ---"
# Host CRUD
RESP=$(auth_post "/api/hosts" '{"domain":"test.local:8888","tls_enabled":false,"http_redirect":false,"upstreams":[{"address":"localhost:3000"}]}')
HOST_ID=$(echo "$RESP" | jq -r '.id')
assert "Create host" "$([ -n "$HOST_ID" ] && [ "$HOST_ID" != "null" ]; echo $?)"

COUNT=$(auth_get "/api/hosts" | jq '.total')
assert "List hosts >= 1" "$([ "$COUNT" -ge 1 ]; echo $?)"

CONTENT=$(auth_get "/api/caddy/caddyfile" | jq -r '.content')
assert "Caddyfile has host" "$(echo "$CONTENT" | grep -q "test.local:8888"; echo $?)"

STATUS=$(auth_get "/api/caddy/status" | jq -r '.running')
assert "Caddy status endpoint" "$([ "$STATUS" = "true" ] || [ "$STATUS" = "false" ]; echo $?)"

auth_delete "/api/hosts/$HOST_ID" > /dev/null 2>&1
assert "Delete host" "$(auth_get "/api/hosts" | jq -e '.total == 0' > /dev/null; echo $?)"

echo -e "\n  --- File Manager Plugin ---"
# mkdir
FM_RESP=$(auth_post "/api/plugins/filemanager/mkdir" '{"path":"/tmp/fm-test-dir"}')
assert "FM: mkdir" "$(echo "$FM_RESP" | jq -e '.status == "ok"' > /dev/null; echo $?)"

# write
FM_RESP=$(auth_post "/api/plugins/filemanager/write" '{"path":"/tmp/fm-test-dir/hello.txt","content":"Hello WebCasa!"}')
assert "FM: write file" "$(echo "$FM_RESP" | jq -e '.status == "ok"' > /dev/null; echo $?)"

# read
FM_CONTENT=$(auth_get "/api/plugins/filemanager/read?path=/tmp/fm-test-dir/hello.txt" | jq -r '.content')
assert "FM: read file" "$([ "$FM_CONTENT" = "Hello WebCasa!" ]; echo $?)"

# list
FM_COUNT=$(auth_get "/api/plugins/filemanager/list?path=/tmp/fm-test-dir" | jq '.files | length')
assert "FM: list dir" "$([ "$FM_COUNT" -ge 1 ]; echo $?)"

# rename
FM_RESP=$(auth_post "/api/plugins/filemanager/rename" '{"old_path":"/tmp/fm-test-dir/hello.txt","new_path":"/tmp/fm-test-dir/renamed.txt"}')
assert "FM: rename" "$(echo "$FM_RESP" | jq -e '.status == "ok"' > /dev/null; echo $?)"

# info
FM_INFO=$(auth_get "/api/plugins/filemanager/info?path=/tmp/fm-test-dir/renamed.txt" | jq -r '.name')
assert "FM: info" "$([ "$FM_INFO" = "renamed.txt" ]; echo $?)"

# delete
FM_RESP=$(curl -sf -X DELETE "$API/api/plugins/filemanager/delete" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"paths":["/tmp/fm-test-dir"]}')
assert "FM: delete" "$(echo "$FM_RESP" | jq -e '.status == "ok"' > /dev/null; echo $?)"

echo -e "\n  --- Deploy Plugin ---"
FW_COUNT=$(auth_get "/api/plugins/deploy/frameworks" | jq '. | length' 2>/dev/null)
assert "Deploy: list frameworks >= 1" "$([ "${FW_COUNT:-0}" -ge 1 ]; echo $?)"

PROJ_RESP=$(auth_get "/api/plugins/deploy/projects" 2>&1)
assert "Deploy: list projects" "$(echo "$PROJ_RESP" | jq -e 'type == "array"' > /dev/null 2>&1; echo $?)"

echo -e "\n  --- AI Plugin ---"
AI_CFG=$(auth_get "/api/plugins/ai/config" 2>&1)
assert "AI: get config" "$(echo "$AI_CFG" | jq -e '.base_url != null' > /dev/null 2>&1; echo $?)"

AI_UPD=$(curl -sf -X PUT "$API/api/plugins/ai/config" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"base_url":"https://api.example.com","model":"test-model"}')
assert "AI: update config" "$(echo "$AI_UPD" | jq -e '.status == "ok"' > /dev/null 2>&1; echo $?)"

echo -e "\n  --- Docker Plugin ---"
DOCKER_RESP=$(curl -s -o /tmp/docker_info.json -w "%{http_code}" \
    "$API/api/plugins/docker/info" \
    -H "Authorization: Bearer $TOKEN")
# Docker daemon may not be available inside the test container, so 200 or 500 is OK
assert "Docker: info endpoint exists" "$([ "$DOCKER_RESP" = "200" ] || [ "$DOCKER_RESP" = "500" ]; echo $?)"

echo -e "\n  --- Plugin System ---"
PLUGIN_COUNT=$(auth_get "/api/plugins" | jq '.plugins | length')
assert "Plugin list >= 4" "$([ "$PLUGIN_COUNT" -ge 4 ]; echo $?)"

MANIFEST_COUNT=$(auth_get "/api/plugins/frontend-manifests" | jq '. | length')
assert "Frontend manifests >= 4" "$([ "$MANIFEST_COUNT" -ge 4 ]; echo $?)"

# ============ Summary ============
echo -e "\n============================================"
echo -e "  Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}, $TOTAL total"
echo "============================================"

if [ "$FAIL" -gt 0 ]; then
    echo -e "\n${RED}Some tests failed!${NC}"
    echo "Container logs:"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -50
    exit 1
fi

echo -e "\n${GREEN}All tests passed!${NC}"
