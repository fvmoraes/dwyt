#!/bin/bash
# E2E Test for DWYT
# Tests complete workflow from installation to usage

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() { echo -e "${GREEN}[INFO]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# Cleanup function
cleanup() {
  info "Cleaning up..."
  pkill -f "dwyt.*daemon" 2>/dev/null || true
  rm -rf ~/.dwyt-test
  rm -rf /tmp/test-project-*
}

trap cleanup EXIT

info "=== E2E Test: First Run ==="

# 1. Clean previous state
info "Cleaning previous state..."
rm -rf ~/.dwyt-test
rm -rf /tmp/test-project-1
rm -rf /tmp/test-project-2

# 2. Create test project
info "Creating test project..."
mkdir -p /tmp/test-project-1
cd /tmp/test-project-1
git init -q
echo "console.log('test')" > index.js
echo "# Test Project" > README.md

# 3. Build DWYT binary
info "Building DWYT binary..."
cd "$(dirname "$0")"
go build -o dwyt . || error "Build failed"
DWYT_BIN="$(pwd)/dwyt"

# 4. Start daemon with test home
info "Starting daemon..."
export DWYT_HOME=~/.dwyt-test
export DWYT_PROJECT=/tmp/test-project-1
timeout 10s "$DWYT_BIN" daemon &
DWYT_PID=$!
sleep 3

# 5. Verify daemon is running
info "Verifying daemon health..."
curl -f http://127.0.0.1:2737/api/health || error "Daemon not responding"

# 6. Check setup status
info "Checking setup status..."
SETUP_STATUS=$(curl -s http://127.0.0.1:2737/api/setup/status)
echo "$SETUP_STATUS" | grep -q '"configured":false' || warn "Setup already configured"

# 7. Get context
info "Getting context..."
CONTEXT=$(curl -s http://127.0.0.1:2737/api/context)
echo "$CONTEXT" | grep -q "test-project-1" || error "Project not loaded"

# 8. Check tool status
info "Checking tool status..."
STATUS=$(curl -s http://127.0.0.1:2737/api/status)
echo "$STATUS" | grep -q '"tools"' || error "Status endpoint failed"

# 9. Test obsidian API
info "Testing obsidian API..."
curl -X POST http://127.0.0.1:2737/api/obsidian/save \
  -H "Content-Type: application/json" \
  -d '{"type":"decision","content":"Test decision from E2E"}' || error "Obsidian save failed"

sleep 1

SEARCH_RESULT=$(curl -s "http://127.0.0.1:2737/api/obsidian/search?q=decision")
echo "$SEARCH_RESULT" | grep -q '"count":1' || error "Obsidian search failed"

# 10. Test obsidian summarize
info "Testing obsidian summarize..."
curl -X POST http://127.0.0.1:2737/api/obsidian/summarize || error "Obsidian summarize failed"

# 11. Get obsidian status
info "Getting obsidian status..."
OBSIDIAN_STATUS=$(curl -s http://127.0.0.1:2737/api/obsidian/status)
echo "$OBSIDIAN_STATUS" | grep -q '"active":true' || error "Obsidian not active"

# 12. Test project switch
info "Testing project switch..."
mkdir -p /tmp/test-project-2
cd /tmp/test-project-2
git init -q
echo "print('test')" > main.py

curl -X POST http://127.0.0.1:2737/api/project/switch \
  -H "Content-Type: application/json" \
  -d '{"path":"/tmp/test-project-2"}' || error "Project switch failed"

sleep 2

CURRENT=$(curl -s http://127.0.0.1:2737/api/projects/current)
echo "$CURRENT" | grep -q "test-project-2" || error "Project not switched"

# 13. Verify obsidian is isolated per project
info "Verifying obsidian isolation..."
SEARCH_RESULT=$(curl -s "http://127.0.0.1:2737/api/obsidian/search?q=decision")
echo "$SEARCH_RESULT" | grep -q '"count":0' || error "Obsidian not isolated between projects"

# 14. Switch back to project 1
info "Switching back to project 1..."
curl -X POST http://127.0.0.1:2737/api/project/switch \
  -H "Content-Type: application/json" \
  -d '{"path":"/tmp/test-project-1"}' || error "Switch back failed"

sleep 2

SEARCH_RESULT=$(curl -s "http://127.0.0.1:2737/api/obsidian/search?q=decision")
echo "$SEARCH_RESULT" | grep -q '"count":1' || error "Obsidian data lost after switch"

# 15. Test state persistence
info "Testing state persistence..."
STATE=$(curl -s http://127.0.0.1:2737/api/state)
echo "$STATE" | grep -q '"current_project"' || error "State endpoint failed"

# 16. Stop daemon
info "Stopping daemon..."
kill $DWYT_PID 2>/dev/null || true
sleep 2

info "=== E2E Test: Restart ==="

# 17. Restart daemon
info "Restarting daemon..."
export DWYT_PROJECT=/tmp/test-project-1
timeout 10s "$DWYT_BIN" daemon &
DWYT_PID=$!
sleep 3

# 18. Verify state was restored
info "Verifying state restoration..."
curl -f http://127.0.0.1:2737/api/health || error "Daemon not responding after restart"

CURRENT=$(curl -s http://127.0.0.1:2737/api/projects/current)
echo "$CURRENT" | grep -q "test-project-1" || error "Project not restored"

# 19. Verify obsidian was preserved
info "Verifying obsidian persistence..."
SEARCH_RESULT=$(curl -s "http://127.0.0.1:2737/api/obsidian/search?q=decision")
echo "$SEARCH_RESULT" | grep -q '"count":1' || error "Obsidian data not persisted"

# 20. Test metrics endpoints
info "Testing metrics endpoints..."
curl -f http://127.0.0.1:2737/api/metrics || error "Metrics endpoint failed"
curl -f http://127.0.0.1:2737/api/tool-details || error "Tool details endpoint failed"

# 21. Test projects list
info "Testing projects list..."
PROJECTS=$(curl -s http://127.0.0.1:2737/api/projects)
echo "$PROJECTS" | grep -q "test-project-1" || error "Project 1 not in list"
echo "$PROJECTS" | grep -q "test-project-2" || error "Project 2 not in list"

# 22. Final cleanup
info "Final cleanup..."
kill $DWYT_PID 2>/dev/null || true

info "=== E2E Test: PASSED ==="
echo ""
echo -e "${GREEN}✓ All tests passed successfully!${NC}"
echo ""
echo "Test coverage:"
echo "  ✓ Daemon startup and health"
echo "  ✓ Obsidian save, search, and summarize"
echo "  ✓ Project switching"
echo "  ✓ Obsidian isolation between projects"
echo "  ✓ State persistence across restarts"
echo "  ✓ API endpoints"
echo ""
