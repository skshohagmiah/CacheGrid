#!/usr/bin/env bash
# test-performance.sh — Runs Go benchmarks and HTTP load tests.
#
# Usage: ./scripts/test-performance.sh [port]
#   port: HTTP port for load tests (default: 6380)
#
# Requirements: curl (for load test). Optional: wrk or hey for advanced load testing.

set -euo pipefail

PORT="${1:-6380}"
BASE="http://localhost:${PORT}"

bold()   { printf "\033[1m%s\033[0m" "$1"; }
green()  { printf "\033[32m%s\033[0m" "$1"; }
yellow() { printf "\033[33m%s\033[0m" "$1"; }

section() {
    echo ""
    echo "$(bold "══ $1 ══")"
    echo ""
}

# ── 1. Go Benchmarks ─────────────────────────────────────

section "Go Benchmarks (Memory Mode)"

echo "Running go test -bench=. -benchmem ..."
echo ""
go test -bench=. -benchmem -benchtime=2s -count=1 . 2>&1 | grep -E "^Benchmark|^ok|^PASS"

# ── 2. Go Benchmarks with Race Detector ──────────────────

section "Race Detector"

echo "Running go test -race (verifies no data races)..."
echo ""
go test -race -count=1 -timeout=60s . 2>&1 | tail -5

# ── 3. HTTP Load Test (curl-based) ───────────────────────

section "HTTP Load Test"

# Check if server is running
if ! curl -sf "$BASE/health" > /dev/null 2>&1; then
    echo "$(yellow "SKIP"): No server running at $BASE"
    echo "Start one with: make run"
    echo ""
else
    echo "Target: $BASE"
    echo ""

    # Seed some data
    echo "Seeding 100 keys..."
    for i in $(seq 1 100); do
        curl -sf -X PUT "$BASE/cache/loadtest:$i" -H "X-TTL: 5m" -d "\"value-$i\"" > /dev/null &
    done
    wait
    echo "Done."
    echo ""

    # ── Write throughput ──
    echo "$(bold "Write Throughput") (1000 sequential PUTs)"
    START=$(date +%s%N)
    for i in $(seq 1 1000); do
        curl -sf -X PUT "$BASE/cache/perf:$i" -H "X-TTL: 5m" -d "\"v$i\"" > /dev/null
    done
    END=$(date +%s%N)
    ELAPSED_MS=$(( (END - START) / 1000000 ))
    OPS_SEC=$(( 1000 * 1000 / (ELAPSED_MS + 1) ))
    echo "  1000 writes in ${ELAPSED_MS}ms (~${OPS_SEC} ops/sec)"
    echo ""

    # ── Read throughput ──
    echo "$(bold "Read Throughput") (1000 sequential GETs)"
    START=$(date +%s%N)
    for i in $(seq 1 1000); do
        curl -sf "$BASE/cache/perf:$((i % 100 + 1))" > /dev/null
    done
    END=$(date +%s%N)
    ELAPSED_MS=$(( (END - START) / 1000000 ))
    OPS_SEC=$(( 1000 * 1000 / (ELAPSED_MS + 1) ))
    echo "  1000 reads in ${ELAPSED_MS}ms (~${OPS_SEC} ops/sec)"
    echo ""

    # ── Mixed throughput ──
    echo "$(bold "Mixed Throughput") (500 writes + 500 reads interleaved)"
    START=$(date +%s%N)
    for i in $(seq 1 500); do
        curl -sf -X PUT "$BASE/cache/mix:$i" -H "X-TTL: 5m" -d "\"m$i\"" > /dev/null
        curl -sf "$BASE/cache/mix:$i" > /dev/null
    done
    END=$(date +%s%N)
    ELAPSED_MS=$(( (END - START) / 1000000 ))
    OPS_SEC=$(( 1000 * 1000 / (ELAPSED_MS + 1) ))
    echo "  1000 mixed ops in ${ELAPSED_MS}ms (~${OPS_SEC} ops/sec)"
    echo ""

    # ── Concurrent load ──
    echo "$(bold "Concurrent Load") (100 parallel requests x 10 rounds)"
    START=$(date +%s%N)
    for round in $(seq 1 10); do
        for i in $(seq 1 100); do
            curl -sf "$BASE/cache/loadtest:$((i % 100 + 1))" > /dev/null &
        done
        wait
    done
    END=$(date +%s%N)
    ELAPSED_MS=$(( (END - START) / 1000000 ))
    OPS_SEC=$(( 1000 * 1000 / (ELAPSED_MS + 1) ))
    echo "  1000 concurrent reads in ${ELAPSED_MS}ms (~${OPS_SEC} ops/sec)"
    echo ""

    # ── Latency check ──
    echo "$(bold "Latency") (single request round-trip)"
    for op in "GET (hit)" "PUT (write)" "HEAD (exists)" "DELETE"; do
        case "$op" in
            "GET"*) TIME=$(curl -sf -o /dev/null -w "%{time_total}" "$BASE/cache/loadtest:1") ;;
            "PUT"*) TIME=$(curl -sf -o /dev/null -w "%{time_total}" -X PUT "$BASE/cache/latency:test" -d '"x"') ;;
            "HEAD"*) TIME=$(curl -sf -o /dev/null -w "%{time_total}" -I "$BASE/cache/loadtest:1") ;;
            "DELETE"*) TIME=$(curl -sf -o /dev/null -w "%{time_total}" -X DELETE "$BASE/cache/latency:test") ;;
        esac
        MS=$(echo "$TIME * 1000" | bc 2>/dev/null || echo "$TIME")
        printf "  %-15s %sms\n" "$op" "$MS"
    done
    echo ""

    # Cleanup
    echo "Cleaning up test keys..."
    for i in $(seq 1 1000); do
        curl -sf -X DELETE "$BASE/cache/perf:$i" > /dev/null 2>&1 &
        curl -sf -X DELETE "$BASE/cache/mix:$i" > /dev/null 2>&1 &
        curl -sf -X DELETE "$BASE/cache/loadtest:$i" > /dev/null 2>&1 &
    done
    wait
    echo "Done."

    # ── Suggest better tools ──
    echo ""
    if command -v wrk > /dev/null 2>&1; then
        echo "$(bold "wrk detected!") Running advanced load test..."
        echo ""
        # Seed a key for wrk to read
        curl -sf -X PUT "$BASE/cache/wrk:test" -H "X-TTL: 10m" -d '"benchmark"' > /dev/null
        wrk -t4 -c50 -d10s "$BASE/cache/wrk:test"
        curl -sf -X DELETE "$BASE/cache/wrk:test" > /dev/null 2>&1 || true
    elif command -v hey > /dev/null 2>&1; then
        echo "$(bold "hey detected!") Running advanced load test..."
        echo ""
        curl -sf -X PUT "$BASE/cache/hey:test" -H "X-TTL: 10m" -d '"benchmark"' > /dev/null
        hey -n 10000 -c 50 "$BASE/cache/hey:test"
        curl -sf -X DELETE "$BASE/cache/hey:test" > /dev/null 2>&1 || true
    else
        echo "$(yellow "TIP"): Install 'wrk' or 'hey' for more accurate HTTP benchmarks:"
        echo "  brew install wrk     # macOS"
        echo "  go install github.com/rakyll/hey@latest"
    fi
fi

echo ""
echo "$(green "Performance tests complete.")"
