#!/usr/bin/env python3
"""
Plot simulation route timing results.
Compares timing CSVs from different routing modes (bidir vs bidir_ep).
"""

import argparse
import csv
import glob
import sys
import os
import numpy as np
import matplotlib.pyplot as plt

def load_timing_csv(path):
    rows = []
    with open(path, newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append({
                "query_id": int(row["query_id"]),
                "src": int(row["src"]),
                "dst": int(row["dst"]),
                "ms": float(row["ms"]),
                "algorithm": row["algorithm"],
            })
    return rows

def plot_comparison(csv1, csv2, out_path="benchmarks/simulation_comparison.png"):
    """Compare two timing CSVs from different algorithms"""
    rows1 = load_timing_csv(csv1)
    rows2 = load_timing_csv(csv2)

    algo1 = rows1[0]["algorithm"] if rows1 else "Algorithm 1"
    algo2 = rows2[0]["algorithm"] if rows2 else "Algorithm 2"

    times1 = np.array([r["ms"] for r in rows1])
    times2 = np.array([r["ms"] for r in rows2])

    fig, axes = plt.subplots(2, 2, figsize=(14, 10))
    fig.suptitle(f"Simulation Benchmark: {algo1} vs {algo2}", fontsize=16, fontweight="bold")

    # Histogram comparison
    ax1 = axes[0][0]
    ax1.hist(times1, bins=50, alpha=0.7, label=algo1, color="#4CAF50")
    ax1.hist(times2, bins=50, alpha=0.7, label=algo2, color="#9C27B0")
    ax1.set_xlabel("Latency (ms)")
    ax1.set_ylabel("Frequency")
    ax1.set_title("Latency Distribution")
    ax1.legend()
    ax1.grid(alpha=0.3)

    # Box plot
    ax2 = axes[0][1]
    bp = ax2.boxplot([times1, times2], labels=[algo1, algo2], patch_artist=True)
    bp["boxes"][0].set_facecolor("#4CAF50")
    bp["boxes"][1].set_facecolor("#9C27B0")
    ax2.set_ylabel("Latency (ms)")
    ax2.set_title("Latency Box Plot")
    ax2.grid(alpha=0.3)

    # CDF
    ax3 = axes[1][0]
    sorted1 = np.sort(times1)
    sorted2 = np.sort(times2)
    cdf1 = np.arange(1, len(sorted1) + 1) / len(sorted1)
    cdf2 = np.arange(1, len(sorted2) + 1) / len(sorted2)
    ax3.plot(sorted1, cdf1, label=algo1, color="#4CAF50", linewidth=2)
    ax3.plot(sorted2, cdf2, label=algo2, color="#9C27B0", linewidth=2)
    ax3.set_xlabel("Latency (ms)")
    ax3.set_ylabel("CDF")
    ax3.set_title("Cumulative Distribution")
    ax3.legend()
    ax3.grid(alpha=0.3)

    # Summary stats
    ax4 = axes[1][1]
    ax4.axis("off")
    stats_text = f"""
    {algo1}:
      Count: {len(times1)}
      Mean: {np.mean(times1):.2f} ms
      Median: {np.median(times1):.2f} ms
      Std Dev: {np.std(times1):.2f} ms
      Min: {np.min(times1):.2f} ms
      Max: {np.max(times1):.2f} ms
      Total: {np.sum(times1):.0f} ms

    {algo2}:
      Count: {len(times2)}
      Mean: {np.mean(times2):.2f} ms
      Median: {np.median(times2):.2f} ms
      Std Dev: {np.std(times2):.2f} ms
      Min: {np.min(times2):.2f} ms
      Max: {np.max(times2):.2f} ms
      Total: {np.sum(times2):.0f} ms

    Speedup: {np.sum(times1)/np.sum(times2):.2f}x ({algo2} vs {algo1})
    """
    ax4.text(0.1, 0.9, stats_text, transform=ax4.transAxes, fontsize=11,
             verticalalignment="top", fontfamily="monospace",
             bbox=dict(boxstyle="round", facecolor="wheat", alpha=0.5))

    plt.tight_layout(rect=[0, 0, 1, 0.95])
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved comparison plot to {out_path}")
    plt.close()

def plot_single(csv_path, out_path=None):
    """Plot a single timing CSV"""
    rows = load_timing_csv(csv_path)
    if not rows:
        print("Error: CSV is empty", file=sys.stderr)
        return

    algo = rows[0]["algorithm"]
    times = np.array([r["ms"] for r in rows])

    if out_path is None:
        out_path = csv_path.replace(".csv", ".png")

    fig, axes = plt.subplots(1, 3, figsize=(15, 5))
    fig.suptitle(f"Simulation Timing: {algo} ({len(rows)} queries)", fontsize=14, fontweight="bold")

    # Histogram
    axes[0].hist(times, bins=50, color="#4CAF50", edgecolor="black", alpha=0.7)
    axes[0].axvline(np.mean(times), color="red", linestyle="--", label=f"Mean: {np.mean(times):.2f} ms")
    axes[0].axvline(np.median(times), color="blue", linestyle="--", label=f"Median: {np.median(times):.2f} ms")
    axes[0].set_xlabel("Latency (ms)")
    axes[0].set_ylabel("Frequency")
    axes[0].set_title("Latency Distribution")
    axes[0].legend()
    axes[0].grid(alpha=0.3)

    # Time series
    axes[1].plot(range(len(times)), times, alpha=0.7, linewidth=0.5)
    axes[1].set_xlabel("Query #")
    axes[1].set_ylabel("Latency (ms)")
    axes[1].set_title("Latency Over Time")
    axes[1].grid(alpha=0.3)

    # Stats
    axes[2].axis("off")
    stats = f"""
    Algorithm: {algo}
    Queries: {len(times)}

    Mean: {np.mean(times):.2f} ms
    Median: {np.median(times):.2f} ms
    Std Dev: {np.std(times):.2f} ms
    Min: {np.min(times):.2f} ms
    Max: {np.max(times):.2f} ms

    Total: {np.sum(times)/1000:.2f} seconds
    """
    axes[2].text(0.1, 0.9, stats, transform=axes[2].transAxes, fontsize=12,
                 verticalalignment="top", fontfamily="monospace",
                 bbox=dict(boxstyle="round", facecolor="wheat", alpha=0.5))

    plt.tight_layout(rect=[0, 0, 1, 0.95])
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved plot to {out_path}")
    plt.close()

def main():
    parser = argparse.ArgumentParser(description="Plot simulation timing results")
    parser.add_argument("--compare", nargs=2, metavar=("CSV1", "CSV2"),
                        help="Compare two timing CSVs")
    parser.add_argument("--single", metavar="CSV",
                        help="Plot a single timing CSV")
    parser.add_argument("--auto", action="store_true",
                        help="Auto-find and compare latest bidir vs bidir_ep CSVs")
    parser.add_argument("-o", "--output", default=None,
                        help="Output file path")
    args = parser.parse_args()

    if args.compare:
        out = args.output or "benchmarks/simulation_comparison.png"
        plot_comparison(args.compare[0], args.compare[1], out)
    elif args.single:
        plot_single(args.single, args.output)
    elif args.auto:
        # Find latest CSVs for each algorithm
        bidir_files = sorted(glob.glob("benchmarks/route_timings_bidir_*.csv"))
        bidir_ep_files = sorted(glob.glob("benchmarks/route_timings_bidir_ep_*.csv"))

        if not bidir_files:
            print("No bidir timing files found. Run simulation with -routing=bidir first.")
            sys.exit(1)
        if not bidir_ep_files:
            print("No bidir_ep timing files found. Run simulation with -routing=bidir_ep first.")
            sys.exit(1)

        latest_bidir = bidir_files[-1]
        latest_bidir_ep = bidir_ep_files[-1]
        print(f"Comparing: {latest_bidir} vs {latest_bidir_ep}")

        out = args.output or "benchmarks/simulation_comparison.png"
        plot_comparison(latest_bidir, latest_bidir_ep, out)
    else:
        parser.print_help()

if __name__ == "__main__":
    main()
