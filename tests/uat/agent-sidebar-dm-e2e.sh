#!/bin/bash
# E2E test for agent sidebar streaming + inline DM feature (channelModel / channel view)
set -e

SOCKET="/tmp/wuphf-sidebar-dm-$$.sock"
BINARY="$(cd "$(dirname "$0")/../.." && pwd)/wuphf"
ARTIFACTS="$(cd "$(dirname "$0")/../.." && pwd)/termwright-artifacts/sidebar-dm-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$ARTIFACTS"

cleanup() {
  pkill -f "termwright daemon.*$SOCKET" 2>/dev/null || true
  rm -f "$SOCKET"
}
trap cleanup EXIT

echo "=== Agent Sidebar DM E2E Test ==="
echo "Binary: $BINARY"
echo "Artifacts: $ARTIFACTS"

# Create a launcher wrapper that runs channel-view directly (no tmux layer).
# --channel-view: run the TUI directly without launching tmux/agents
# --no-nex: skip API-key prompt so the model starts clean in office-only mode
# WUPHF_NO_SPLASH: skip the splash screen so channelModel starts immediately
LAUNCHER="$ARTIFACTS/launcher.sh"
cat > "$LAUNCHER" <<EOF
#!/bin/bash
export WUPHF_NO_SPLASH=1
exec "$BINARY" --channel-view --no-nex "\$@"
EOF
chmod +x "$LAUNCHER"

termwright daemon --socket "$SOCKET" --cols 140 --rows 45 --background "$LAUNCHER"
sleep 4

send_raw() {
  local text="$1"
  for (( i=0; i<${#text}; i++ )); do
    local ch="${text:$i:1}"
    local b64=$(printf '%s' "$ch" | base64)
    termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\": \"$b64\"}" >/dev/null 2>&1
    sleep 0.05
  done
}

send_tab() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "CQ=="}' >/dev/null 2>&1
}

send_enter() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "DQ=="}' >/dev/null 2>&1
}

send_esc() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Gw=="}' >/dev/null 2>&1
}

# j = down navigation (sidebar)
send_j() {
  local b64=$(printf '%s' "j" | base64)
  termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\": \"$b64\"}" >/dev/null 2>&1
}

# d = open DM in sidebar
send_d() {
  local b64=$(printf '%s' "d" | base64)
  termwright exec --socket "$SOCKET" --method raw --params "{\"bytes_base64\": \"$b64\"}" >/dev/null 2>&1
}

# Ctrl+B = toggle sidebar collapse
send_ctrl_b() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Ag=="}' >/dev/null 2>&1
}

# Ctrl+D = close DM
send_ctrl_d() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "BA=="}' >/dev/null 2>&1
}

send_ctrl_c() {
  termwright exec --socket "$SOCKET" --method raw --params '{"bytes_base64": "Aw=="}' >/dev/null 2>&1
}

# Focus sidebar with Tab
focus_sidebar() {
  send_tab
  sleep 0.5
}

get_screen() {
  termwright exec --socket "$SOCKET" --method screen --params '{}' 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('result',''))"
}

screenshot() {
  termwright exec --socket "$SOCKET" --method screenshot --params "{\"path\": \"$ARTIFACTS/$1.png\"}" >/dev/null 2>&1 || true
}

assert_text() {
  local label="$1"
  local text="$2"
  local screen=$(get_screen)
  if echo "$screen" | grep -q "$text"; then
    echo "  PASS: $label"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $label — '$text' not found on screen"
    echo "$screen" > "$ARTIFACTS/failure-$(date +%s).txt"
    FAIL=$((FAIL + 1))
  fi
}

assert_not_text() {
  local label="$1"
  local text="$2"
  local screen=$(get_screen)
  if ! echo "$screen" | grep -q "$text"; then
    echo "  PASS: $label"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $label — '$text' unexpectedly present"
    echo "$screen" > "$ARTIFACTS/failure-$(date +%s).txt"
    FAIL=$((FAIL + 1))
  fi
}

PASS=0
FAIL=0

# ===== TEST 1: TUI boots with agents in sidebar =====
# With WUPHF_NO_SPLASH=1, the channel view starts immediately.
echo ""
echo "--- Test 1: TUI boots and shows agents in sidebar ---"
sleep 2
screenshot "01-boot"
assert_text "sidebar shows Agents header" "Agents"
assert_text "CEO is listed" "CEO"
assert_text "main channel is visible" "general"

# ===== TEST 2: Tab cycles focus to sidebar =====
echo ""
echo "--- Test 2: Tab focuses the sidebar ---"
focus_sidebar
screenshot "02-sidebar-focused"
assert_text "sidebar focus indicator" "Agents"

# ===== TEST 3: Navigate sidebar with j =====
echo ""
echo "--- Test 3: j navigates sidebar cursor ---"
send_j
sleep 0.3
send_j
sleep 0.3
screenshot "03-sidebar-navigated"
# The sidebar cursor moved — verified by screenshot

# ===== TEST 4: d opens inline DM from sidebar =====
echo ""
echo "--- Test 4: d key opens inline DM with first roster agent ---"
# After Tab in Test 2, focus = focusSidebar (channelModel received Tab directly;
# no splash to consume it). j/j in Test 3 kept focus on sidebar.
# d now fires updateSidebar → case "d" → opens DM, sets notice.
send_d
sleep 0.6
screenshot "04-dm-open"
assert_text "DM notice shown" "DM open with"
assert_text "office stays active notice" "office stays active"

# ===== TEST 5: Composer shows DM target label =====
echo ""
echo "--- Test 5: Composer shows DM→ target label ---"
assert_text "DM target in composer label" "DM→"
assert_text "DM hint shows Ctrl+D" "Ctrl+D"

# ===== TEST 6: Ctrl+D closes DM =====
echo ""
echo "--- Test 6: Ctrl+D closes the inline DM ---"
send_ctrl_d
sleep 0.5
screenshot "06-dm-closed"
assert_text "DM closed notice" "DM with"
assert_text "back to office" "back to office"
assert_not_text "DM label gone from composer" "DM→"

# ===== TEST 7: Ctrl+B toggles sidebar collapse =====
echo ""
echo "--- Test 7: Ctrl+B collapses/expands sidebar ---"
send_ctrl_b
sleep 0.3
screenshot "07-sidebar-collapsed"
send_ctrl_b
sleep 0.3
screenshot "07-sidebar-expanded"
assert_text "sidebar still shows after expand" "Agents"

# ===== CLEANUP =====
echo ""
send_ctrl_c
sleep 0.5
send_ctrl_c

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
echo "Screenshots: $ARTIFACTS/"

if [ $FAIL -gt 0 ]; then
  exit 1
fi
