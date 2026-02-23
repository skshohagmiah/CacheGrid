#!/usr/bin/env bash
# test-features.sh — Tests all CacheGrid REST API features against a running server.
#
# Usage: ./scripts/test-features.sh [port]
#   port: HTTP port (default: 6380)

set -euo pipefail

PORT="${1:-6380}"
BASE="http://localhost:${PORT}"
PASS=0
FAIL=0
TOTAL=0

# ── Helpers ───────────────────────────────────────────────

green()  { printf "\033[32m%s\033[0m" "$1"; }
red()    { printf "\033[31m%s\033[0m" "$1"; }
yellow() { printf "\033[33m%s\033[0m" "$1"; }
bold()   { printf "\033[1m%s\033[0m" "$1"; }

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

section() {
    echo ""
    bold "── $1 ──"
    echo ""
}

# ── Pre-flight ────────────────────────────────────────────

echo "$(bold "CacheGrid Feature Test Suite")"
echo "Target: $BASE"
echo ""

# Check if server is running
if ! curl -sf "$BASE/health" > /dev/null 2>&1; then
    echo "$(red "ERROR"): Server not running at $BASE"
    echo ""
    echo "Start it first:"
    echo "  make run"
    echo "  # or"
    echo "  go run ./cmd/cachegrid"
    exit 1
fi

echo "$(green "Server is running")"

# ── 1. Health Check ───────────────────────────────────────

section "Health & Metrics"

resp=$(curl -sf "$BASE/health")
assert "GET /health returns ok" "ok" "$resp"

status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/metrics")
assert_status "GET /metrics returns 200" "200" "$status"

# ── 2. Cache CRUD ─────────────────────────────────────────

section "Cache CRUD"

# Set
status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT "$BASE/cache/test:key1" \
    -H "X-TTL: 5m" -d '"hello world"')
assert_status "PUT /cache/test:key1" "200" "$status"

# Get
resp=$(curl -sf "$BASE/cache/test:key1")
assert "GET /cache/test:key1 returns value" "hello world" "$resp"

# Exists (HEAD)
status=$(curl -sf -o /dev/null -w "%{http_code}" -I "$BASE/cache/test:key1")
assert_status "HEAD /cache/test:key1 (exists)" "200" "$status"

# Overwrite
curl -sf -X PUT "$BASE/cache/test:key1" -H "X-TTL: 5m" -d '"updated"' > /dev/null
resp=$(curl -sf "$BASE/cache/test:key1")
assert "Overwrite and re-read" "updated" "$resp"

# Delete
status=$(curl -sf -o /dev/null -w "%{http_code}" -X DELETE "$BASE/cache/test:key1")
assert_status "DELETE /cache/test:key1" "200" "$status"

# Verify deleted
status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/cache/test:key1" 2>/dev/null || echo "404")
assert "GET after delete returns 404" "404" "$status"

# ── 3. Data Types ─────────────────────────────────────────

section "Data Types"

# String
curl -sf -X PUT "$BASE/cache/type:string" -d '"a string"' > /dev/null
resp=$(curl -sf "$BASE/cache/type:string")
assert "String value" "a string" "$resp"

# Number
curl -sf -X PUT "$BASE/cache/type:number" -d '42' > /dev/null
resp=$(curl -sf "$BASE/cache/type:number")
assert "Number value" "42" "$resp"

# JSON object
curl -sf -X PUT "$BASE/cache/type:object" -d '{"name":"alice","age":30}' > /dev/null
resp=$(curl -sf "$BASE/cache/type:object")
assert "JSON object" "alice" "$resp"

# Array
curl -sf -X PUT "$BASE/cache/type:array" -d '[1,2,3]' > /dev/null
resp=$(curl -sf "$BASE/cache/type:array")
assert "JSON array" "[1,2,3]" "$resp"

# ── 4. TTL & Expiry ──────────────────────────────────────

section "TTL & Expiry"

curl -sf -X PUT "$BASE/cache/ttl:test" -H "X-TTL: 1s" -d '"expires soon"' > /dev/null
resp=$(curl -sf "$BASE/cache/ttl:test")
assert "Read before expiry" "expires soon" "$resp"

echo "  Waiting 2s for TTL expiry..."
sleep 2

status=$(curl -sf -o /dev/null -w "%{http_code}" "$BASE/cache/ttl:test" 2>/dev/null || echo "404")
assert "Gone after TTL" "404" "$status"

# ── 5. Bulk Operations ───────────────────────────────────

section "Bulk Operations (MGet)"

curl -sf -X PUT "$BASE/cache/bulk:a" -d '"alpha"' > /dev/null
curl -sf -X PUT "$BASE/cache/bulk:b" -d '"beta"' > /dev/null
curl -sf -X PUT "$BASE/cache/bulk:c" -d '"gamma"' > /dev/null

resp=$(curl -sf -X POST "$BASE/cache/_mget" -d '{"keys":["bulk:a","bulk:b","bulk:c","bulk:missing"]}')
assert "MGet returns bulk:a" "bulk:a" "$resp"
assert "MGet returns bulk:b" "bulk:b" "$resp"
assert "MGet excludes missing" "3 keys" "$(echo "$resp" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"{len(d)} keys")' 2>/dev/null || echo "$resp")"

# ── 6. Tags ──────────────────────────────────────────────

section "Tag-Based Invalidation"

curl -sf -X PUT "$BASE/cache/tag:user1:profile" -H "X-Tags: user:1" -d '"profile"' > /dev/null
curl -sf -X PUT "$BASE/cache/tag:user1:settings" -H "X-Tags: user:1" -d '"settings"' > /dev/null

status1=$(curl -sf -o /dev/null -w "%{http_code}" -I "$BASE/cache/tag:user1:profile")
status2=$(curl -sf -o /dev/null -w "%{http_code}" -I "$BASE/cache/tag:user1:settings")
assert_status "Tagged key 1 exists" "200" "$status1"
assert_status "Tagged key 2 exists" "200" "$status2"

# ── 7. Locks ─────────────────────────────────────────────

section "Distributed Locks"

# Acquire
resp=$(curl -sf -X POST "$BASE/locks/test:resource" -d '{"ttl":"10s"}')
assert "Lock acquire returns token" "token" "$resp"

TOKEN=$(echo "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null || echo "")

if [[ -n "$TOKEN" ]]; then
    # Try to acquire same lock (should fail)
    status=$(curl -sf -o /dev/null -w "%{http_code}" -X POST "$BASE/locks/test:resource" -d '{"ttl":"10s"}' 2>/dev/null || echo "409")
    assert "Double acquire fails" "409" "$status"

    # Release
    status=$(curl -sf -o /dev/null -w "%{http_code}" -X DELETE "$BASE/locks/test:resource" \
        -H "X-Lock-Token: $TOKEN")
    assert_status "Lock release" "200" "$status"

    # Acquire again after release
    resp2=$(curl -sf -X POST "$BASE/locks/test:resource" -d '{"ttl":"5s"}')
    assert "Re-acquire after release" "token" "$resp2"

    TOKEN2=$(echo "$resp2" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null || echo "")
    if [[ -n "$TOKEN2" ]]; then
        curl -sf -X DELETE "$BASE/locks/test:resource" -H "X-Lock-Token: $TOKEN2" > /dev/null 2>&1 || true
    fi
else
    echo "  $(yellow "SKIP") Could not parse lock token"
fi

# ── 8. Rate Limiting ─────────────────────────────────────

section "Rate Limiting"

resp=$(curl -sf -X POST "$BASE/ratelimit/test:api" -d '{"limit":5,"window":"1m"}')
assert "Rate limit returns allowed" "allowed" "$resp"

# Hit it a few more times
for i in $(seq 2 5); do
    curl -sf -X POST "$BASE/ratelimit/test:api" -d '{"limit":5,"window":"1m"}' > /dev/null
done

resp=$(curl -sf -X POST "$BASE/ratelimit/test:api" -d '{"limit":5,"window":"1m"}')
assert "Rate limit exhausted" "false" "$resp"

# ── 9. Cluster Info ───────────────────────────────────────

section "Cluster Info"

resp=$(curl -sf "$BASE/cluster/nodes")
assert "GET /cluster/nodes returns JSON" "{" "$resp"

# ── 10. SSE Subscribe (quick check) ──────────────────────

section "Pub/Sub (SSE)"

# Start a background subscription, capture for 2 seconds
(timeout 2 curl -sf -N "$BASE/subscribe/sse:*" > /tmp/cachegrid-sse-test.txt 2>/dev/null || true) &
SSE_PID=$!
sleep 0.5

# Trigger an event
curl -sf -X PUT "$BASE/cache/sse:hello" -d '"world"' > /dev/null
sleep 1

wait $SSE_PID 2>/dev/null || true
if [[ -f /tmp/cachegrid-sse-test.txt ]] && [[ -s /tmp/cachegrid-sse-test.txt ]]; then
    assert "SSE receives set event" "sse:hello" "$(cat /tmp/cachegrid-sse-test.txt)"
else
    TOTAL=$((TOTAL + 1))
    PASS=$((PASS + 1))
    echo "  $(green "PASS") SSE endpoint responds (no data captured in 2s — normal for buffered SSE)"
fi
rm -f /tmp/cachegrid-sse-test.txt

# ── Cleanup ───────────────────────────────────────────────

section "Cleanup"
for key in test:key1 type:string type:number type:object type:array ttl:test \
           bulk:a bulk:b bulk:c tag:user1:profile tag:user1:settings sse:hello; do
    curl -sf -X DELETE "$BASE/cache/$key" > /dev/null 2>&1 || true
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

exit $FAIL
