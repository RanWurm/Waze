#!/usr/bin/env python3
import csv
import matplotlib.pyplot as plt
import os
import sys

csv_path = "benchmarks/bidir_hybrid_cache_vs_nocache_benchmark_results.csv"
if not os.path.exists(csv_path):
    print(f"Error: {csv_path} not found. Run the Go benchmark first.", file=sys.stderr)
    sys.exit(1)

def load_csv(path):
    cache, nocache, rounds_set = [], [], set()
    with open(path) as f:
        reader = csv.DictReader(f)
        for row in reader:
            cache.append(float(row['bidir_hybrid_cache_ms']))
            nocache.append(float(row['bidir_hybrid_nocache_ms']))
            rounds_set.add(int(row['round']))
    num_rounds = len(rounds_set)
    pairs_per_round = len(cache) // num_rounds if num_rounds > 0 else len(cache)
    return cache, nocache, num_rounds, pairs_per_round

times1, times2, num_rounds, pairs_per_round = load_csv(csv_path)
algo1, algo2 = "Hybrid Cache", "Hybrid NoCache"

avg1, avg2 = sum(times1)/len(times1), sum(times2)/len(times2)
total1, total2 = sum(times1), sum(times2)
max1, max2 = max(times1), max(times2)

print(f"\n{'='*50}")
print(f"  {algo1}: {len(times1)} queries")
print(f"    Avg: {avg1:.2f}ms | Total: {total1:.0f}ms | Max: {max1:.2f}ms")
print(f"\n  {algo2}: {len(times2)} queries")
print(f"    Avg: {avg2:.2f}ms | Total: {total2:.0f}ms | Max: {max2:.2f}ms")
print(f"\n  Winner: {algo1 if avg1 < avg2 else algo2} ({abs(avg1-avg2)/max(avg1,avg2)*100:.1f}% faster avg)")
print(f"{'='*50}\n")

fig, axes = plt.subplots(2, 2, figsize=(12, 8))

# 1. Average time bar chart
axes[0,0].bar([algo1, algo2], [avg1, avg2], color=['#2ecc71', '#3498db'])
axes[0,0].set_ylabel('Avg Time (ms)')
axes[0,0].set_title('Average Route Computation Time')
for i, v in enumerate([avg1, avg2]):
    axes[0,0].text(i, v + 0.5, f'{v:.2f}', ha='center')

# 2. Cumulative time over queries
cum1 = [sum(times1[:i+1]) for i in range(len(times1))]
cum2 = [sum(times2[:i+1]) for i in range(len(times2))]
axes[0,1].plot(cum1, label=algo1, color='#2ecc71')
axes[0,1].plot(cum2, label=algo2, color='#3498db')
axes[0,1].set_xlabel('Query #')
axes[0,1].set_ylabel('Cumulative Time (ms)')
axes[0,1].set_title('Total Time Spent on Routing')
axes[0,1].legend()
axes[0,1].grid(True, alpha=0.3)

# 3. Time per query
axes[1,0].plot(times1, alpha=0.6, label=algo1, color='#2ecc71', linewidth=0.8)
axes[1,0].plot(times2, alpha=0.6, label=algo2, color='#3498db', linewidth=0.8)
axes[1,0].set_xlabel('Query #')
axes[1,0].set_ylabel('Time (ms)')
axes[1,0].set_title('Route Time per Query')
axes[1,0].legend()
axes[1,0].grid(True, alpha=0.3)

# 4. Histogram
axes[1,1].hist(times1, bins=50, alpha=0.6, label=algo1, color='#2ecc71')
axes[1,1].hist(times2, bins=50, alpha=0.6, label=algo2, color='#3498db')
axes[1,1].set_xlabel('Time (ms)')
axes[1,1].set_ylabel('Frequency')
axes[1,1].set_title('Time Distribution')
axes[1,1].legend()
axes[1,1].grid(True, alpha=0.3)

plt.suptitle(f'{algo1} vs {algo2}', fontsize=14, fontweight='bold')
plt.tight_layout()

os.makedirs('benchmarks/plots', exist_ok=True)
output_path = f'benchmarks/plots/bidir_hybrid_cache_vs_nocache_{num_rounds}rounds_{pairs_per_round}pairs.png'
plt.savefig(output_path, dpi=150)
print(f"Saved to {output_path}")
plt.show()
