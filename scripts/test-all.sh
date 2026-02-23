#!/usr/bin/env bash
# test-all.sh — Runs the full CacheGrid test suite.
#
# Usage: ./scripts/test-all.sh [options]
#
# Options:
#   --skip-cluster   Skip cluster tests
#   --skip-disk      Skip disk persistence tests
#   --skip-perf      Skip performance tests
#   --quick          Only run unit tests and feature tests
#
# This runs, in order:
#   1. Go unit tests (go test ./...)
#   2. Feature tests (REST API against a running server)
#   3. Disk persistence tests
#   4. Cluster tests (3-node)
#   5. Performance tests (benchmarks + HTTP load)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="./build/cachegrid"
SERVER_PID=""
FEATURE_PORT=16399
RESULTS=()
SKIP_CLUSTER=false
SKIP_DISK=false
SKIP_PERF=false
QUICK=false

# ── Parse args ───────────────────────────────────────────

for arg in "$@"; do
    case "$arg" in
        --skip-cluster) SKIP_CLUSTER=true ;;
        --skip-disk)    SKIP_DISK=true ;;
        --skip-perf)    SKIP_PERF=true ;;
        --quick)        QUICK=true; SKIP_CLUSTER=true; SKIP_DISK=true; SKIP_PERF=true ;;
        --help|-h)
            echo "Usage: $0 [--skip-cluster] [--skip-disk] [--skip-perf] [--quick]"
            exit 0
            ;;
        *)
            echo "Unknown option: $arg"
            exit 1
            ;;
    esac
done

# ── Helpers ──────────────────────────────────────────────

green()  { printf "\033[32m%s\033[0m" "$1"; }
red()    { printf "\033[31m%s\033[0m" "$1"; }
yellow() { printf "\033[33m%s\033[0m" "$1"; }
bold()   { printf "\033[1m%s\033[0m" "$1"; }

banner() {
    echo ""
    echo "$(bold "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")"
    echo "  $(bold "$1")"
    echo "$(bold "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")"
    echo ""
}

record_result() {
    local name="$1" status="$2"
    if [[ "$status" -eq 0 ]]; then
        RESULTS+=("$(green "PASS") $name")
    else
        RESULTS+=("$(red "FAIL") $name")
    fi
}

cleanup_server() {
    if [[ -n "$SERVER_PID" ]]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
        SERVER_PID=""
    fi
}
trap cleanup_server EXIT

# ── Header ───────────────────────────────────────────────

echo ""
echo "$(bold "╔═══════════════════════════════════════════════╗")"
echo "$(bold "║     CacheGrid — Full Test Suite               ║")"
echo "$(bold "╚═══════════════════════════════════════════════╝")"
echo ""

if $QUICK; then
    echo "  Mode: $(yellow "quick") (unit tests + feature tests only)"
else
    echo "  Mode: $(green "full")"
    $SKIP_CLUSTER && echo "  Skipping: cluster tests"
    $SKIP_DISK    && echo "  Skipping: disk tests"
    $SKIP_PERF    && echo "  Skipping: performance tests"
fi
echo ""

# ── 1. Build ─────────────────────────────────────────────

banner "Step 1: Build"

echo "Building binary..."
go build -trimpath -o "$BINARY" ./cmd/cachegrid
echo "$(green "✓") Built $BINARY"

# ── 2. Unit Tests ────────────────────────────────────────

banner "Step 2: Go Unit Tests"

if go test -count=1 -timeout 120s ./...; then
    record_result "Unit Tests (go test ./...)" 0
else
    record_result "Unit Tests (go test ./...)" 1
fi

# ── 3. Feature Tests ────────────────────────────────────

banner "Step 3: Feature Tests (REST API)"

echo "Starting server on port $FEATURE_PORT..."
CACHEGRID_HTTP_PORT=$FEATURE_PORT \
$BINARY > /tmp/cachegrid-test-all-server.log 2>&1 &
SERVER_PID=$!

# Wait for server
for i in $(seq 1 10); do
    if curl -sf "http://localhost:$FEATURE_PORT/health" > /dev/null 2>&1; then
        echo "$(green "✓") Server ready"
        break
    fi
    sleep 1
done

if bash "$SCRIPT_DIR/test-features.sh" "$FEATURE_PORT"; then
    record_result "Feature Tests (REST API)" 0
else
    record_result "Feature Tests (REST API)" 1
fi

# Stop server
cleanup_server

# ── 4. Disk Persistence Tests ───────────────────────────

if ! $SKIP_DISK; then
    banner "Step 4: Disk Persistence Tests"

    if bash "$SCRIPT_DIR/test-disk.sh"; then
        record_result "Disk Persistence Tests" 0
    else
        record_result "Disk Persistence Tests" 1
    fi
else
    echo ""
    echo "  $(yellow "SKIP") Disk persistence tests (--skip-disk)"
fi

# ── 5. Cluster Tests ────────────────────────────────────

if ! $SKIP_CLUSTER; then
    banner "Step 5: Cluster Tests (3-Node)"

    if bash "$SCRIPT_DIR/test-cluster.sh"; then
        record_result "Cluster Tests (3-Node)" 0
    else
        record_result "Cluster Tests (3-Node)" 1
    fi
else
    echo ""
    echo "  $(yellow "SKIP") Cluster tests (--skip-cluster)"
fi

# ── 6. Performance Tests ────────────────────────────────

if ! $SKIP_PERF; then
    banner "Step 6: Performance Tests"

    if bash "$SCRIPT_DIR/test-performance.sh"; then
        record_result "Performance Tests" 0
    else
        record_result "Performance Tests" 1
    fi
else
    echo ""
    echo "  $(yellow "SKIP") Performance tests (--skip-perf)"
fi

# ── Summary ──────────────────────────────────────────────

echo ""
echo ""
echo "$(bold "╔═══════════════════════════════════════════════╗")"
echo "$(bold "║               Test Suite Summary              ║")"
echo "$(bold "╚═══════════════════════════════════════════════╝")"
echo ""

fail_count=0
for result in "${RESULTS[@]}"; do
    echo "  $result"
    if [[ "$result" == *"FAIL"* ]]; then
        fail_count=$((fail_count + 1))
    fi
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [[ $fail_count -eq 0 ]]; then
    echo "  $(green "ALL ${#RESULTS[@]} TEST SUITES PASSED")"
else
    echo "  $(red "$fail_count SUITE(S) FAILED") out of ${#RESULTS[@]}"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

exit $fail_count
