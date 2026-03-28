#!/bin/bash
# Usage: ./run.sh
# Interactive menu to choose between simulation or benchmarks.

set -e
cd "$(dirname "$0")"

echo ""
echo "=== Waze Traffic Simulation ==="
echo ""
echo "  1. Run simulation + 3D visualization"
echo "  2. Run interactive benchmark"
echo ""
read -p "Choice [1-2]: " MODE

if [ "$MODE" = "2" ]; then
    echo ""
    echo "=== Starting interactive benchmark ==="
    go run benchmarks/interactive_benchmark/main.go
    exit 0
fi

# --- Simulation mode ---
read -p "Algorithm [1=hybrid 2=bidir 3=bidir_ep 4=bidir_hybrid] (default 4): " ALGO
ALGO="${ALGO:-4}"
read -p "Cache [1=yes 2=no] (default 1): " CACHE
CACHE="${CACHE:-1}"

cleanup() {
    echo ""
    echo "Shutting down..."
    kill $SIM_PID $GUI_PID $SRV_PID 2>/dev/null
    wait $SRV_PID $GUI_PID $SIM_PID 2>/dev/null
    echo "Done."
}
trap cleanup EXIT INT TERM

# 1) Start server
echo ""
echo "=== Starting server (algo=$ALGO, cache=$CACHE) ==="
printf "${ALGO}\n${CACHE}\n" | go run cmd/server/main.go &
SRV_PID=$!
sleep 3

# 2) Start simulation
echo "=== Starting simulation ==="
go run ./cmd/simulation &
SIM_PID=$!
sleep 2

# 3) Start 3D visualization
echo "=== Starting 3D visualization ==="
python visualization/app.py &
GUI_PID=$!

echo ""
echo "All running. Press Ctrl+C to stop everything."
wait
