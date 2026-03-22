#!/usr/bin/env python3
"""
Compares the 2 most recent server timing CSVs and plots them like the vs benchmarks.
"""

import csv
import glob
import os
import sys
import numpy as np
import matplotlib.pyplot as plt


def find_latest_csvs(directory="benchmarks", count=2):
    pattern = os.path.join(directory, "route_timings_*.csv")
    files = glob.glob(pattern)
    files.sort(key=os.path.getmtime, reverse=True)
    return files[:count]


def load_csv(path):
    rows = []
    with open(path, newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append({
                "src": int(row["src"]),
                "dst": int(row["dst"]),
                "ms": float(row["ms"]),
                "algorithm": row["algorithm"],
            })
    return rows


def match_pairs(rows1, rows2):
    """Match rows by (src, dst) pairs, return aligned arrays."""
    lookup = {(r["src"], r["dst"]): r["ms"] for r in rows2}
    ms1, ms2 = [], []
    for r in rows1:
        key = (r["src"], r["dst"])
        if key in lookup:
            ms1.append(r["ms"])
            ms2.append(lookup[key])
    return np.array(ms1), np.array(ms2)


def plot_scatter(ax, ms1, ms2, label1, label2, colors):
    ax.scatter(ms1, ms2, alpha=0.3, s=10, color="#555555")
    max_val = max(ms1.max(), ms2.max()) * 1.05
    ax.plot([0, max_val], [0, max_val], "r--", linewidth=1.5, label="Equal performance")
    ax.set_xlabel(f"{label1} Latency (ms)")
    ax.set_ylabel(f"{label2} Latency (ms)")
    ax.set_title("Per-Query Latency Comparison")
    ax.legend()
    ax.set_xlim(0, max_val)
    ax.set_ylim(0, max_val)
    ax.set_aspect("equal")
    ax.text(max_val * 0.7, max_val * 0.2, f"{label1} slower\n{label2} faster",
            fontsize=10, color=colors[1], fontweight="bold", ha="center")
    ax.text(max_val * 0.2, max_val * 0.7, f"{label1} faster\n{label2} slower",
            fontsize=10, color=colors[0], fontweight="bold", ha="center")
    ax.grid(alpha=0.3)


def speedup_text(ms1, ms2, label1, label2):
    overall_speedup = np.mean(ms1) / np.mean(ms2)
    if overall_speedup > 1:
        return f"{label2} is {overall_speedup:.2f}x faster on average"
    else:
        return f"{label1} is {1/overall_speedup:.2f}x faster on average"


def plot_pie(ax, ms1, ms2, label1, label2, colors):
    wins2 = int(np.sum(ms1 > ms2))
    wins1 = int(np.sum(ms2 > ms1))
    ties = int(np.sum(ms1 == ms2))
    sizes = [wins2, wins1]
    labels_pie = [f"{label2}\n({wins2})", f"{label1}\n({wins1})"]
    pie_colors = [colors[1], colors[0]]
    if ties > 0:
        sizes.append(ties)
        labels_pie.append(f"Tie\n({ties})")
        pie_colors.append("#CCCCCC")
    ax.pie(sizes, labels=labels_pie, colors=pie_colors, autopct="%1.1f%%",
           startangle=90, textprops={"fontsize": 10, "fontweight": "bold"})
    ax.set_title(f"Win Rate\n{speedup_text(ms1, ms2, label1, label2)}", fontsize=10)


def main():
    csvs = find_latest_csvs()
    if len(csvs) < 2:
        print("Error: Need at least 2 route_timings CSVs in benchmarks/", file=sys.stderr)
        sys.exit(1)

    print(f"Comparing:\n  {csvs[0]}\n  {csvs[1]}")

    rows1 = load_csv(csvs[0])
    rows2 = load_csv(csvs[1])

    if not rows1 or not rows2:
        print("Error: One of the CSVs is empty.", file=sys.stderr)
        sys.exit(1)

    label1 = rows1[0]["algorithm"]
    label2 = rows2[0]["algorithm"]

    ms1, ms2 = match_pairs(rows1, rows2)
    if len(ms1) == 0:
        print("Error: No matching (src, dst) pairs found between the two files.", file=sys.stderr)
        sys.exit(1)

    print(f"Matched {len(ms1)} pairs")

    colors = ["#4CAF50", "#9C27B0"]

    fig, axes = plt.subplots(1, 2, figsize=(14, 6))
    fig.suptitle(f"{label1} vs {label2}", fontsize=16, fontweight="bold")

    plot_scatter(axes[0], ms1, ms2, label1, label2, colors)
    plot_pie(axes[1], ms1, ms2, label1, label2, colors)

    plt.tight_layout(rect=[0, 0, 1, 0.93])
    out_dir = "benchmarks/server_comparison"
    os.makedirs(out_dir, exist_ok=True)
    out_path = f"{out_dir}/{label1}_vs_{label2}.png"
    plt.savefig(out_path, dpi=150, bbox_inches="tight")
    print(f"Saved plot to {out_path}")
    plt.close()


if __name__ == "__main__":
    main()
