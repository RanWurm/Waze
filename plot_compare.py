#!/usr/bin/env python3
import csv
import matplotlib.pyplot as plt
import glob
import os

# Get the 2 latest CSVs from server folder
csv_files = sorted(glob.glob("benchmarks/server/*.csv"), key=os.path.getmtime, reverse=True)

if len(csv_files) < 2:
    print("Need at least 2 CSV files to compare")
    print("Run the server with different routing modes and save timings")
    exit(1)

print(f"Comparing:\n  1: {csv_files[0]}\n  2: {csv_files[1]}")

def get_name_from_filename(path):
    # Extract name from filename like "bidir_hybrid_cache_2024-03-18_15-04-05.csv"
    basename = os.path.basename(path)
    # Remove .csv and timestamp (last 2 underscore-separated parts)
    parts = basename.replace('.csv', '').split('_')
    # Remove timestamp parts (date and time at the end)
    # Format: algo_cache_YYYY-MM-DD_HH-MM-SS
    if len(parts) >= 4:
        return '_'.join(parts[:-2])  # everything except date and time
    return basename.replace('.csv', '')

def load_csv(path):
    times = []
    with open(path) as f:
        reader = csv.DictReader(f)
        for row in reader:
            times.append(float(row['ms']))
    return times

times1 = load_csv(csv_files[0])
times2 = load_csv(csv_files[1])
algo1 = get_name_from_filename(csv_files[0])
algo2 = get_name_from_filename(csv_files[1])

# Stats
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

# Save to plots folder
os.makedirs('benchmarks/plots', exist_ok=True)
output_path = f'benchmarks/plots/{algo1}_vs_{algo2}.png'
plt.savefig(output_path, dpi=150)
print(f"Saved to {output_path}")
plt.show()
