#!/usr/bin/env python3
"""
Reads benchmark_results.csv and produces benchmark_results.png
comparing A* vs Delta-Stepping performance.
"""

import argparse
import csv
import sys
import os
import numpy as np
import matplotlib.pyplot as plt

def load_csv(path):
    rows = []
    with open(path, newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append({
                "round": int(row["round"]),
                "pair": int(row["pair"]),
                "src": int(row["src"]),
                "dst": int(row["dst"]),
                "astar_ms": float(row["astar_ms"]),
                "entrypoint_ms": float(row["entrypoint_ms"]),
            })
    return rows

def main():
    parser = argparse.ArgumentParser(description="Plot benchmark results")
    parser.add_argument("--long", action="store_true",
                        help="Read from long_benchmark_results.csv and save plots to longbenchmarks/")
    parser.add_argument("--short", action="store_true",
                        help="Read from short_benchmark_results.csv and save plots to shortbenchmarks/")
    parser.add_argument("--mid", action="store_true",
                        help="Read from mid_benchmark_results.csv and save plots to midbenchmarks/")
    args = parser.parse_args()

    if sum([args.long, args.short, args.mid]) > 1:
        print("Error: Cannot use --long, --short, and --mid at the same time.", file=sys.stderr)
        sys.exit(1)

    if args.long:
        csv_path = "long_benchmark_results.csv"
    elif args.short:
        csv_path = "short_benchmark_results.csv"
    elif args.mid:
        csv_path = "mid_benchmark_results.csv"
    else:
        csv_path = "benchmark_results.csv"
    if not os.path.exists(csv_path):
        print(f"Error: {csv_path} not found. Run the Go benchmark first.", file=sys.stderr)
        sys.exit(1)

    rows = load_csv(csv_path)
    if not rows:
        print("Error: CSV is empty.", file=sys.stderr)
        sys.exit(1)

    all_astar = np.array([r["astar_ms"] for r in rows])
    all_delta = np.array([r["entrypoint_ms"] for r in rows])
    speedups = all_astar / all_delta  # >1 means EP is faster

    rounds = sorted(set(r["round"] for r in rows))
    astar_by_round = {rd: [] for rd in rounds}
    delta_by_round = {rd: [] for rd in rounds}
    for r in rows:
        astar_by_round[r["round"]].append(r["astar_ms"])
        delta_by_round[r["round"]].append(r["entrypoint_ms"])

    colors = {"astar": "#2196F3", "delta": "#FF9800"}

    fig, axes = plt.subplots(2, 2, figsize=(14, 12))
    fig.suptitle("A* vs Delta-Stepping Benchmark", fontsize=16, fontweight="bold")

    # --- Plot 1: Scatter plot (top-left) ---
    ax1 = axes[0][0]
    ax1.scatter(all_astar, all_delta, alpha=0.3, s=10, color="#555555")
    max_val = max(all_astar.max(), all_delta.max()) * 1.05
    ax1.plot([0, max_val], [0, max_val], "r--", linewidth=1.5, label="Equal performance")
    ax1.set_xlabel("A* Latency (ms)")
    ax1.set_ylabel("Delta-Stepping Latency (ms)")
    ax1.set_title("Per-Query: A* vs Delta-Stepping")
    ax1.legend()
    ax1.set_xlim(0, max_val)
    ax1.set_ylim(0, max_val)
    ax1.set_aspect("equal")
    # Label regions
    ax1.text(max_val * 0.7, max_val * 0.2, "A* slower\nDelta faster",
             fontsize=10, color=colors["delta"], fontweight="bold", ha="center")
    ax1.text(max_val * 0.2, max_val * 0.7, "A* faster\nDelta slower",
             fontsize=10, color=colors["astar"], fontweight="bold", ha="center")
    ax1.grid(alpha=0.3)

    # --- Plot 2: Per-round speedup (top-right) ---
    ax2 = axes[0][1]
    round_speedups = [np.mean(astar_by_round[rd]) / np.mean(delta_by_round[rd]) for rd in rounds]
    bar_colors = [colors["delta"] if s > 1 else colors["astar"] for s in round_speedups]
    bars = ax2.bar([str(rd) for rd in rounds], round_speedups, color=bar_colors, edgecolor="white")
    ax2.axhline(y=1, color="red", linestyle="--", linewidth=1.5, label="Equal (1x)")
    ax2.set_xlabel("Round")
    ax2.set_ylabel("Speedup (A* time / Delta time)")
    ax2.set_title("Per-Round Speedup")
    ax2.legend()
    ax2.grid(axis="y", alpha=0.3)
    # Label bars
    for bar, s in zip(bars, round_speedups):
        label = f"{s:.2f}x"
        ax2.text(bar.get_x() + bar.get_width() / 2, bar.get_height() + 0.02,
                 label, ha="center", va="bottom", fontweight="bold", fontsize=9)

    # --- Plot 3: Win rate pie chart (bottom-left) ---
    ax3 = axes[1][0]
    delta_wins = int(np.sum(all_astar > all_delta))
    astar_wins = int(np.sum(all_delta > all_astar))
    ties = int(np.sum(all_astar == all_delta))
    sizes = [delta_wins, astar_wins]
    labels_pie = [f"Delta-Stepping\n({delta_wins} queries)", f"A*\n({astar_wins} queries)"]
    pie_colors = [colors["delta"], colors["astar"]]
    if ties > 0:
        sizes.append(ties)
        labels_pie.append(f"Tie\n({ties} queries)")
        pie_colors.append("#CCCCCC")
    ax3.pie(sizes, labels=labels_pie, colors=pie_colors, autopct="%1.1f%%",
            startangle=90, textprops={"fontsize": 11, "fontweight": "bold"})
    ax3.set_title("Win Rate (which algorithm was faster)")

    # --- Plot 4: Speedup distribution histogram (bottom-right) ---
    ax4 = axes[1][1]
    log_speedups = np.log2(speedups)
    ax4.hist(log_speedups, bins=50, color="#555555", edgecolor="white", alpha=0.8)
    ax4.axvline(x=0, color="red", linestyle="--", linewidth=1.5, label="Equal (1x)")
    ax4.axvline(x=np.median(log_speedups), color=colors["delta"], linestyle="-",
                linewidth=2, label=f"Median: {2**np.median(log_speedups):.2f}x")
    ax4.set_xlabel("Speedup (log2 scale): ← A* faster | Delta faster →")
    ax4.set_ylabel("Number of queries")
    ax4.set_title("Speedup Distribution")
    ax4.legend()
    ax4.grid(axis="y", alpha=0.3)

    # Overall summary text
    overall_speedup = np.mean(all_astar) / np.mean(all_delta)
    winner = "Delta-Stepping" if overall_speedup > 1 else "A*"
    factor = overall_speedup if overall_speedup > 1 else 1 / overall_speedup
    fig.text(0.5, 0.01,
             f"Overall: {winner} is {factor:.2f}x faster on average "
             f"(A* mean={np.mean(all_astar):.1f}ms, Delta mean={np.mean(all_delta):.1f}ms, "
             f"{len(rows)} queries)",
             ha="center", fontsize=12, fontweight="bold",
             bbox=dict(boxstyle="round,pad=0.4", facecolor="#f0f0f0", edgecolor="#cccccc"))

    plt.tight_layout(rect=[0, 0.04, 1, 0.95])
    num_rounds = len(rounds)
    pairs_per_round = max(len(astar_by_round[rd]) for rd in rounds)
    if args.long:
        out_dir = "longbenchmarks"
    elif args.short:
        out_dir = "shortbenchmarks"
    elif args.mid:
        out_dir = "midbenchmarks"
    else:
        out_dir = "benchmarks"
    out_path = f"{out_dir}/{num_rounds}rounds{pairs_per_round}pairs.png"
    os.makedirs(out_dir, exist_ok=True)
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved plot to {out_path}")
    plt.close()

if __name__ == "__main__":
    main()
