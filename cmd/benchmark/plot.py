#!/usr/bin/env python3
"""
Reads benchmark_results.csv and produces benchmark plots
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

def plot_scatter(ax, astar, delta, title, colors):
    ax.scatter(astar, delta, alpha=0.3, s=10, color="#555555")
    max_val = max(astar.max(), delta.max()) * 1.05
    ax.plot([0, max_val], [0, max_val], "r--", linewidth=1.5, label="Equal performance")
    ax.set_xlabel("A* Latency (ms)")
    ax.set_ylabel("Delta-Stepping Latency (ms)")
    ax.set_title(title)
    ax.legend()
    ax.set_xlim(0, max_val)
    ax.set_ylim(0, max_val)
    ax.set_aspect("equal")
    ax.text(max_val * 0.7, max_val * 0.2, "A* slower\nDelta faster",
            fontsize=10, color=colors["delta"], fontweight="bold", ha="center")
    ax.text(max_val * 0.2, max_val * 0.7, "A* faster\nDelta slower",
            fontsize=10, color=colors["astar"], fontweight="bold", ha="center")
    ax.grid(alpha=0.3)

def speedup_text(astar, delta):
    overall_speedup = np.mean(astar) / np.mean(delta)
    if overall_speedup > 1:
        return f"EntryPoint is {overall_speedup:.2f}x faster on average"
    else:
        return f"A* is {1/overall_speedup:.2f}x faster on average"

def plot_pie(ax, astar, delta, title, colors):
    delta_wins = int(np.sum(astar > delta))
    astar_wins = int(np.sum(delta > astar))
    ties = int(np.sum(astar == delta))
    sizes = [delta_wins, astar_wins]
    labels_pie = [f"Delta-Stepping\n({delta_wins})", f"A*\n({astar_wins})"]
    pie_colors = [colors["delta"], colors["astar"]]
    if ties > 0:
        sizes.append(ties)
        labels_pie.append(f"Tie\n({ties})")
        pie_colors.append("#CCCCCC")
    ax.pie(sizes, labels=labels_pie, colors=pie_colors, autopct="%1.1f%%",
           startangle=90, textprops={"fontsize": 10, "fontweight": "bold"})
    ax.set_title(title + f"\n{speedup_text(astar, delta)}", fontsize=10)

def plot_single(csv_path, out_dir, out_name):
    rows = load_csv(csv_path)
    if not rows:
        print("Error: CSV is empty.", file=sys.stderr)
        sys.exit(1)

    astar = np.array([r["astar_ms"] for r in rows])
    delta = np.array([r["entrypoint_ms"] for r in rows])
    colors = {"astar": "#2196F3", "delta": "#FF9800"}

    fig, axes = plt.subplots(1, 2, figsize=(14, 6))
    fig.suptitle("A* vs Delta-Stepping Benchmark", fontsize=16, fontweight="bold")

    plot_scatter(axes[0], astar, delta, "Per-Query: A* vs Delta-Stepping", colors)
    plot_pie(axes[1], astar, delta, "Win Rate", colors)

    plt.tight_layout(rect=[0, 0, 1, 0.93])
    os.makedirs(out_dir, exist_ok=True)
    out_path = f"{out_dir}/{out_name}"
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved plot to {out_path}")
    plt.close()

def plot_all():
    modes = [
        ("All Pairs", "benchmark_results.csv"),
        ("Short", "short_benchmark_results.csv"),
        ("Mid", "mid_benchmark_results.csv"),
        ("Long", "long_benchmark_results.csv"),
    ]
    colors = {"astar": "#2196F3", "delta": "#FF9800"}

    # Check all CSVs exist
    for label, path in modes:
        if not os.path.exists(path):
            print(f"Error: {path} not found. Run the Go benchmark with --all first.", file=sys.stderr)
            sys.exit(1)

    fig, axes = plt.subplots(2, 4, figsize=(24, 10))
    fig.suptitle("A* vs Delta-Stepping Benchmark — All Distance Categories", fontsize=16, fontweight="bold")

    for col, (label, path) in enumerate(modes):
        rows = load_csv(path)
        if not rows:
            continue
        astar = np.array([r["astar_ms"] for r in rows])
        delta = np.array([r["entrypoint_ms"] for r in rows])

        plot_scatter(axes[0][col], astar, delta, f"{label} — Per-Query ({len(rows)})", colors)
        plot_pie(axes[1][col], astar, delta, f"{label} — Win Rate", colors)

    plt.tight_layout(rect=[0, 0, 1, 0.95])
    out_dir = "allbenchmarks"
    os.makedirs(out_dir, exist_ok=True)
    # Use pair count from first CSV for naming
    first_rows = load_csv(modes[0][1])
    rounds = sorted(set(r["round"] for r in first_rows))
    num_rounds = len(rounds)
    pairs_per_round = max(sum(1 for r in first_rows if r["round"] == rd) for rd in rounds)
    out_path = f"{out_dir}/{num_rounds}rounds{pairs_per_round}pairs.png"
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved plot to {out_path}")
    plt.close()

def plot_threads():
    csv_path = "threads_benchmark_results.csv"
    if not os.path.exists(csv_path):
        print(f"Error: {csv_path} not found. Run the Go benchmark with --threads first.", file=sys.stderr)
        sys.exit(1)

    rows = []
    with open(csv_path, newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append({
                "threads": int(row["threads"]),
                "astar_ms": float(row["astar_ms"]),
                "entrypoint_ms": float(row["entrypoint_ms"]),
            })

    if not rows:
        print("Error: CSV is empty.", file=sys.stderr)
        sys.exit(1)

    thread_counts = sorted(set(r["threads"] for r in rows))
    astar_means = []
    delta_means = []
    astar_medians = []
    delta_medians = []
    for tc in thread_counts:
        a = np.array([r["astar_ms"] for r in rows if r["threads"] == tc])
        d = np.array([r["entrypoint_ms"] for r in rows if r["threads"] == tc])
        astar_means.append(np.mean(a))
        delta_means.append(np.mean(d))
        astar_medians.append(np.median(a))
        delta_medians.append(np.median(d))

    colors = {"astar": "#2196F3", "delta": "#FF9800"}
    x_labels = [str(tc) for tc in thread_counts]

    fig, axes = plt.subplots(1, 2, figsize=(14, 6))
    fig.suptitle("Thread Scaling Benchmark — A* vs EntryPoint", fontsize=16, fontweight="bold")

    # Plot 1: Mean latency vs thread count
    ax1 = axes[0]
    ax1.plot(x_labels, astar_means, "o-", color=colors["astar"], linewidth=2, markersize=8, label="A* (mean)")
    ax1.plot(x_labels, delta_means, "o-", color=colors["delta"], linewidth=2, markersize=8, label="EntryPoint (mean)")
    ax1.set_xlabel("Thread Count")
    ax1.set_ylabel("Mean Latency (ms)")
    ax1.set_title("Mean Latency vs Threads")
    ax1.legend()
    ax1.grid(alpha=0.3)
    for i, tc in enumerate(x_labels):
        ax1.annotate(f"{astar_means[i]:.1f}", (tc, astar_means[i]),
                     textcoords="offset points", xytext=(0, 10), ha="center", fontsize=8, color=colors["astar"])
        ax1.annotate(f"{delta_means[i]:.1f}", (tc, delta_means[i]),
                     textcoords="offset points", xytext=(0, -15), ha="center", fontsize=8, color=colors["delta"])

    # Plot 2: Speedup relative to 1 thread
    ax2 = axes[1]
    astar_speedup = [astar_means[0] / m for m in astar_means]
    delta_speedup = [delta_means[0] / m for m in delta_means]
    ax2.plot(x_labels, astar_speedup, "o-", color=colors["astar"], linewidth=2, markersize=8, label="A* speedup")
    ax2.plot(x_labels, delta_speedup, "o-", color=colors["delta"], linewidth=2, markersize=8, label="EntryPoint speedup")
    ax2.axhline(y=1, color="red", linestyle="--", linewidth=1, alpha=0.5)
    ax2.set_xlabel("Thread Count")
    ax2.set_ylabel("Speedup vs 1 Thread")
    ax2.set_title("Scaling (speedup relative to 1 thread)")
    ax2.legend()
    ax2.grid(alpha=0.3)
    for i, tc in enumerate(x_labels):
        ax2.annotate(f"{astar_speedup[i]:.2f}x", (tc, astar_speedup[i]),
                     textcoords="offset points", xytext=(0, 10), ha="center", fontsize=8, color=colors["astar"])
        ax2.annotate(f"{delta_speedup[i]:.2f}x", (tc, delta_speedup[i]),
                     textcoords="offset points", xytext=(0, -15), ha="center", fontsize=8, color=colors["delta"])

    plt.tight_layout(rect=[0, 0, 1, 0.93])
    out_dir = "threadbenchmarks"
    os.makedirs(out_dir, exist_ok=True)
    num_pairs = len([r for r in rows if r["threads"] == thread_counts[0]])
    out_path = f"{out_dir}/{num_pairs}pairs.png"
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved plot to {out_path}")
    plt.close()

def main():
    parser = argparse.ArgumentParser(description="Plot benchmark results")
    parser.add_argument("--long", action="store_true",
                        help="Read from long_benchmark_results.csv and save plots to longbenchmarks/")
    parser.add_argument("--short", action="store_true",
                        help="Read from short_benchmark_results.csv and save plots to shortbenchmarks/")
    parser.add_argument("--mid", action="store_true",
                        help="Read from mid_benchmark_results.csv and save plots to midbenchmarks/")
    parser.add_argument("--all", action="store_true",
                        help="Read all 4 CSVs and plot them side by side in allbenchmarks/")
    parser.add_argument("--threads", action="store_true",
                        help="Plot thread scaling from threads_benchmark_results.csv")
    args = parser.parse_args()

    if sum([args.long, args.short, args.mid, args.all, args.threads]) > 1:
        print("Error: Use only one mode at a time.", file=sys.stderr)
        sys.exit(1)

    if args.threads:
        plot_threads()
        return

    if getattr(args, "all"):
        plot_all()
        return

    if args.long:
        csv_path = "long_benchmark_results.csv"
        out_dir = "longbenchmarks"
    elif args.short:
        csv_path = "short_benchmark_results.csv"
        out_dir = "shortbenchmarks"
    elif args.mid:
        csv_path = "mid_benchmark_results.csv"
        out_dir = "midbenchmarks"
    else:
        csv_path = "benchmark_results.csv"
        out_dir = "benchmarks"

    if not os.path.exists(csv_path):
        print(f"Error: {csv_path} not found. Run the Go benchmark first.", file=sys.stderr)
        sys.exit(1)

    rows = load_csv(csv_path)
    rounds = sorted(set(r["round"] for r in rows))
    num_rounds = len(rounds)
    pairs_per_round = max(sum(1 for r in rows if r["round"] == rd) for rd in rounds)
    out_name = f"{num_rounds}rounds{pairs_per_round}pairs.png"
    plot_single(csv_path, out_dir, out_name)

if __name__ == "__main__":
    main()
