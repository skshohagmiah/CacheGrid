#!/usr/bin/env bash
# test-cluster.sh — Starts a 3-node CacheGrid cluster and tests distributed operations.
#
# Usage: ./scripts/test-cluster.sh
#
# This script:
#   1. Builds the binary
#   2. Starts 3 nodes on different ports
#   3. Waits for cluster formation
#   4. Tests partitioned reads/writes across nodes
#   5. Tests cluster node discovery
#   6. Tests node failure handling
#   7. Cleans up all processes

set -euo pipefail

BINARY="./build/cachegrid"
PIDS=()
PASS=0
FAIL=0
TOTAL=0

# Ports for 3 nodes
#           HTTP   Gossip  RPC
NODE1_HTTP=16380; NODE1_GOSSIP=17946; NODE1_RPC=17947
NODE2_HTTP=16381; NODE2_GOSSIP=17948; NODE2_RPC=17949
NODE3_HTTP=16382; NODE3_GOSSIP=17950; NODE3_RPC=17951

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
    echo "Shutting down cluster..."
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null || true
    echo "Done."
}
trap cleanup EXIT

wait_for_node() {
    local url="$1" name="$2" max_wait=10
    for i in $(seq 1 $max_wait); do
        if curl -sf "$url/health" > /dev/null 2>&1; then
            echo "  $(green "✓") $name is ready"
            return 0
        fi
        sleep 1
    done
    echo "  $(red "✗") $name failed to start"
    return 1
}

# ── Build ─────────────────────────────────────────────────

echo "$(bold "CacheGrid Cluster Test Suite")"
echo ""
echo "Building..."
go build -trimpath -o "$BINARY" ./cmd/cachegrid
echo "$(green "Built") $BINARY"

# ── Start Cluster ─────────────────────────────────────────

section "Starting 3-Node Cluster"

# Node 1 (seed)
CACHEGRID_NODE_NAME=node-1 \
CACHEGRID_LISTEN_ADDR=":${NODE1_GOSSIP}" \
CACHEGRID_HTTP_PORT=$NODE1_HTTP \
CACHEGRID_RPC_PORT=$NODE1_RPC \
$BINARY > /tmp/cachegrid-node1.log 2>&1 &
PIDS+=($!)
echo "  Starting node-1 (HTTP=$NODE1_HTTP, gossip=$NODE1_GOSSIP)..."

sleep 1

# Node 2
CACHEGRID_NODE_NAME=node-2 \
CACHEGRID_LISTEN_ADDR=":${NODE2_GOSSIP}" \
CACHEGRID_HTTP_PORT=$NODE2_HTTP \
CACHEGRID_RPC_PORT=$NODE2_RPC \
CACHEGRID_SEEDS="127.0.0.1:${NODE1_GOSSIP}" \
$BINARY > /tmp/cachegrid-node2.log 2>&1 &
PIDS+=($!)
echo "  Starting node-2 (HTTP=$NODE2_HTTP, gossip=$NODE2_GOSSIP)..."

# Node 3
CACHEGRID_NODE_NAME=node-3 \
CACHEGRID_LISTEN_ADDR=":${NODE3_GOSSIP}" \
CACHEGRID_HTTP_PORT=$NODE3_HTTP \
CACHEGRID_RPC_PORT=$NODE3_RPC \
CACHEGRID_SEEDS="127.0.0.1:${NODE1_GOSSIP}" \
$BINARY > /tmp/cachegrid-node3.log 2>&1 &
PIDS+=($!)
echo "  Starting node-3 (HTTP=$NODE3_HTTP, gossip=$NODE3_GOSSIP)..."

echo ""
echo "Waiting for nodes to come up..."
wait_for_node "http://localhost:$NODE1_HTTP" "node-1"
wait_for_node "http://localhost:$NODE2_HTTP" "node-2"
wait_for_node "http://localhost:$NODE3_HTTP" "node-3"

# Give gossip time to propagate
sleep 2

# ── Cluster Discovery ────────────────────────────────────

section "Cluster Discovery"

resp1=$(curl -sf "http://localhost:$NODE1_HTTP/cluster/nodes")
resp2=$(curl -sf "http://localhost:$NODE2_HTTP/cluster/nodes")
resp3=$(curl -sf "http://localhost:$NODE3_HTTP/cluster/nodes")

assert "Node-1 sees cluster" "node-2" "$resp1"
assert "Node-2 sees cluster" "node-1" "$resp2"
assert "Node-3 sees cluster" "node-1" "$resp3"

# ── Write to One, Read from All ──────────────────────────

section "Distributed Read/Write"

# Write to node-1
status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT \
    "http://localhost:$NODE1_HTTP/cache/cluster:key1" \
    -H "X-TTL: 5m" -d '"distributed-value"')
assert_status "Write to node-1" "200" "$status"

sleep 1  # allow replication/routing

# Read from all nodes (in partitioned mode, the owner has it)
found=false
for port in $NODE1_HTTP $NODE2_HTTP $NODE3_HTTP; do
    resp=$(curl -sf "http://localhost:$port/cache/cluster:key1" 2>/dev/null || echo "")
    if [[ "$resp" == *"distributed-value"* ]]; then
        found=true
        break
    fi
done
assert "Read from cluster returns value" "true" "$found"

# ── Write Multiple Keys Across Nodes ─────────────────────

section "Key Distribution"

echo "  Writing 50 keys to node-1..."
for i in $(seq 1 50); do
    curl -sf -X PUT "http://localhost:$NODE1_HTTP/cache/dist:$i" \
        -H "X-TTL: 5m" -d "\"val-$i\"" > /dev/null
done

# Check some keys are readable from different nodes
read_count=0
for i in $(seq 1 50); do
    for port in $NODE1_HTTP $NODE2_HTTP $NODE3_HTTP; do
        resp=$(curl -sf "http://localhost:$port/cache/dist:$i" 2>/dev/null || echo "")
        if [[ "$resp" == *"val-$i"* ]]; then
            read_count=$((read_count + 1))
            break
        fi
    done
done
assert "All 50 keys readable from cluster" "50" "$read_count"

# ── Locks Across Nodes ───────────────────────────────────

section "Distributed Locks"

# Acquire lock on node-1
resp=$(curl -sf -X POST "http://localhost:$NODE1_HTTP/locks/cluster:resource" \
    -d '{"ttl":"10s"}')
assert "Lock acquire on node-1" "token" "$resp"

TOKEN=$(echo "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))' 2>/dev/null || echo "")

if [[ -n "$TOKEN" ]]; then
    # Try to acquire same lock on node-2 (should fail)
    status=$(curl -sf -o /dev/null -w "%{http_code}" -X POST \
        "http://localhost:$NODE2_HTTP/locks/cluster:resource" \
        -d '{"ttl":"10s"}' 2>/dev/null || echo "409")
    assert "Lock conflict on node-2" "409" "$status"

    # Release from node-1
    curl -sf -X DELETE "http://localhost:$NODE1_HTTP/locks/cluster:resource" \
        -H "X-Lock-Token: $TOKEN" > /dev/null 2>&1 || true
fi

# ── Node Failure ──────────────────────────────────────────

section "Node Failure & Recovery"

echo "  Killing node-3 (PID=${PIDS[2]})..."
kill "${PIDS[2]}" 2>/dev/null || true
sleep 3

# Remaining nodes should still work
status=$(curl -sf -o /dev/null -w "%{http_code}" -X PUT \
    "http://localhost:$NODE1_HTTP/cache/after:failure" \
    -H "X-TTL: 5m" -d '"still-works"')
assert_status "Write after node failure" "200" "$status"

resp=$(curl -sf "http://localhost:$NODE2_HTTP/health")
assert "Node-2 healthy after node-3 failure" "ok" "$resp"

# ── Cleanup Test Keys ────────────────────────────────────

section "Cleanup"

for i in $(seq 1 50); do
    curl -sf -X DELETE "http://localhost:$NODE1_HTTP/cache/dist:$i" > /dev/null 2>&1 &
done
curl -sf -X DELETE "http://localhost:$NODE1_HTTP/cache/cluster:key1" > /dev/null 2>&1 || true
curl -sf -X DELETE "http://localhost:$NODE1_HTTP/cache/after:failure" > /dev/null 2>&1 || true
wait 2>/dev/null || true
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
echo "Node logs: /tmp/cachegrid-node{1,2,3}.log"

exit $FAIL
