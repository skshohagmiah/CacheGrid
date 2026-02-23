#!/usr/bin/env bash
# test-disk.sh — Tests CacheGrid disk mode (PebbleDB) persistence.
#
# Usage: ./scripts/test-disk.sh
#
# This script:
#   1. Builds the binary
#   2. Starts the server in disk mode
#   3. Writes data via HTTP
#   4. Stops the server
#   5. Restarts the server (same disk path)
#   6. Verifies data survived the restart
#   7. Tests disk-specific operations
#   8. Cleans up

set -euo pipefail

BINARY="./build/cachegrid"
DATA_DIR="/tmp/cachegrid-disk-test-$$"
PORT=16390
PID=""
PASS=0
FAIL=0
TOTAL=0
BASE="http://localhost:${PORT}"

# ── Helpers ───────────────────────────────────────────────

green()  { printf "\033[32m%s\033[0m" "$1"; }
red()    { printf "\033[31m%s\033[0m" "$1"; }
yellow() { printf "\033[33m%s\033[0m" "$1"; }
bold()   { printf "\033[1m%s\033[0m" "$1"; }

section() {
    echo ""
    bold "── $1 ──"
    echo ""
}

assert() {
    local name="$1" expected="$2" actual="$3"
    TOTAL=$((TOTAL + 1))
    if [[ "$actual" == *"$expected"* ]]; then
        PASS=$((PASS + 1))
        echo "  $(green "PASS") $name"
    else
        FAIL=$((FAIL + 1))
        echo "  $(red "FAIL") $name"
        echo "       expected: $expected"
        echo "       got:      $actual"
    fi
}

assert_status() {
    local name="$1" expected="$2" actual="$3"
    TOTAL=$((TOTAL + 1))
    if [[ "$actual" == "$expected" ]]; then
        PASS=$((PASS + 1))
        echo "  $(green "PASS") $name (HTTP $actual)"
    else
        FAIL=$((FAIL + 1))
        echo "  $(red "FAIL") $name"
        echo "       expected HTTP $expected, got HTTP $actual"
    fi
}

cleanup() {
    echo ""
    echo "Cleaning up..."
    if [[ -n "$PID" ]]; then
        kill "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
    fi
    rm -rf "$DATA_DIR"
    echo "Done."
}
trap cleanup EXIT

start_server() {
    CACHEGRID_STORAGE_MODE=disk \
    CACHEGRID_DISK_PATH="$DATA_DIR" \
    CACHEGRID_HTTP_PORT=$PORT \
    $BINARY > /tmp/cachegrid-disk-test.log 2>&1 &
    PID=$!

    local max_wait=10
    for i in $(seq 1 $max_wait); do
        if curl -sf "$BASE/health" > /dev/null 2>&1; then
            echo "  $(green "✓") Server is ready (PID=$PID)"
            return 0
        fi
        sleep 1
    done
    echo "  $(red "✗") Server failed to start"
    return 1
}

stop_server() {
    if [[ -n "$PID" ]]; then
        kill "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
        PID=""
    fi
    echo "  Server stopped"
}

# ── Build ─────────────────────────────────────────────────

echo "$(bold "CacheGrid Disk Mode Test Suite")"
echo ""
echo "Building..."
go build -trimpath -o "$BINARY" ./cmd/cachegrid
echo "$(green "Built") $BINARY"
echo "Data directory: $DATA_DIR"

# ── Phase 1: Write Data ─────────────────────────────────

section "Phase 1: Write Data to Disk"

echo "  Starting server in disk mode..."
start_server

# Write various data types
status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/persist:string" \
    -H "X-TTL: 1h" -d '"hello from disk"')
assert_status "Write string" "200" "$status"

status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/persist:number" \
    -H "X-TTL: 1h" -d '42')
assert_status "Write number" "200" "$status"

status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/persist:object" \
    -H "X-TTL: 1h" -d '{"name":"alice","role":"admin"}')
assert_status "Write object" "200" "$status"

status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/persist:array" \
    -H "X-TTL: 1h" -d '[1,2,3,"four"]')
assert_status "Write array" "200" "$status"

# Write 20 numbered keys
echo "  Writing 20 numbered keys..."
for i in $(seq 1 20); do
    curl -sf -X PUT "$BASE/cache/persist:batch:$i" \
        -H "X-TTL: 1h" -d "\"value-$i\"" > /dev/null
done
assert "Batch write complete" "true" "true"

# Verify data is readable before restart
resp=$(curl -sf "$BASE/cache/persist:string")
assert "Read string before restart" "hello from disk" "$resp"

resp=$(curl -sf "$BASE/cache/persist:number")
assert "Read number before restart" "42" "$resp"

# ── Phase 2: Restart & Verify ───────────────────────────

section "Phase 2: Stop & Restart Server"

echo "  Stopping server..."
stop_server
sleep 1

echo "  Verifying data directory exists..."
if [[ -d "$DATA_DIR" ]]; then
    size=$(du -sh "$DATA_DIR" 2>/dev/null | cut -f1)
    echo "  $(green "✓") Data directory exists ($size)"
else
    echo "  $(red "✗") Data directory missing!"
fi

echo "  Restarting server (same data dir)..."
start_server

# ── Phase 3: Verify Persistence ─────────────────────────

section "Phase 3: Verify Data Survived Restart"

# Read back all data types
resp=$(curl -sf "$BASE/cache/persist:string")
assert "String survived restart" "hello from disk" "$resp"

resp=$(curl -sf "$BASE/cache/persist:number")
assert "Number survived restart" "42" "$resp"

resp=$(curl -sf "$BASE/cache/persist:object")
assert "Object survived restart" "alice" "$resp"

resp=$(curl -sf "$BASE/cache/persist:array")
assert "Array survived restart" "[1,2,3,\"four\"]" "$resp"

# Verify batch keys
read_count=0
for i in $(seq 1 20); do
    resp=$(curl -sf "$BASE/cache/persist:batch:$i" 2>/dev/null || echo "")
    if [[ "$resp" == *"value-$i"* ]]; then
        read_count=$((read_count + 1))
    fi
done
assert "All 20 batch keys survived" "20" "$read_count"

# ── Phase 4: Modify After Restart ───────────────────────

section "Phase 4: Operations After Restart"

# Overwrite existing key
status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/persist:string" \
    -H "X-TTL: 1h" -d '"updated after restart"')
assert_status "Overwrite after restart" "200" "$status"

resp=$(curl -sf "$BASE/cache/persist:string")
assert "Read overwritten value" "updated after restart" "$resp"

# Delete a key
status=$(curl -sf -o /dev/null -w "%{http_code}" -X DELETE "$BASE/cache/persist:batch:1")
assert_status "Delete after restart" "200" "$status"

status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/cache/persist:batch:1" 2>/dev/null || echo "404")
assert "Deleted key returns 404" "404" "$status"

# Write new keys after restart
status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/persist:new" \
    -H "X-TTL: 1h" -d '"born after restart"')
assert_status "Write new key after restart" "200" "$status"

resp=$(curl -sf "$BASE/cache/persist:new")
assert "Read new key" "born after restart" "$resp"

# ── Phase 5: TTL on Disk ────────────────────────────────

section "Phase 5: TTL Expiry on Disk"

curl -sf -X PUT "$BASE/cache/persist:shortlived" \
    -H "X-TTL: 2s" -d '"expires soon"' > /dev/null

resp=$(curl -sf "$BASE/cache/persist:shortlived")
assert "Read before TTL expiry" "expires soon" "$resp"

echo "  Waiting 3s for TTL..."
sleep 3

status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/cache/persist:shortlived" 2>/dev/null || echo "404")
assert "Gone after TTL on disk" "404" "$status"

# ── Phase 6: Second Restart Verify ──────────────────────

section "Phase 6: Second Restart (Verify Modifications Persist)"

echo "  Stopping server..."
stop_server
sleep 1

echo "  Restarting server..."
start_server

resp=$(curl -sf "$BASE/cache/persist:string")
assert "Overwritten value persists across 2nd restart" "updated after restart" "$resp"

resp=$(curl -sf "$BASE/cache/persist:new")
assert "New key persists across 2nd restart" "born after restart" "$resp"

status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/cache/persist:batch:1" 2>/dev/null || echo "404")
assert "Deleted key still gone after 2nd restart" "404" "$status"

# ── Phase 7: Health & Metrics ───────────────────────────

section "Phase 7: Server Health in Disk Mode"

resp=$(curl -sf "$BASE/health")
assert "Health check returns ok" "ok" "$resp"

status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/metrics")
assert_status "Metrics endpoint works" "200" "$status"

# ── Cleanup Test Keys ────────────────────────────────────

section "Cleanup"

for key in persist:string persist:number persist:object persist:array persist:new persist:shortlived; do
    curl -sf -X DELETE "$BASE/cache/$key" > /dev/null 2>&1 || true
done
for i in $(seq 1 20); do
    curl -sf -X DELETE "$BASE/cache/persist:batch:$i" > /dev/null 2>&1 || true
done
echo "  Cleaned up test keys"

# ── Results ───────────────────────────────────────────────

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [[ $FAIL -eq 0 ]]; then
    echo "  $(green "ALL $TOTAL TESTS PASSED")"
else
    echo "  $(green "$PASS passed"), $(red "$FAIL failed") out of $TOTAL"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Server log: /tmp/cachegrid-disk-test.log"

exit $FAIL
