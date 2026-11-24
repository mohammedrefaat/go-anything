import sys
import os
import time
import json
import subprocess
from statistics import quantiles

DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(DIR, "python"))
import worker

GO_MAIN = os.path.join(DIR, "go", "main.go")
GO_BIN = os.path.join(DIR, "go_worker.exe" if os.name == "nt" else "go_worker")

SAMPLE_TASKS = [
    ("calc_stats", {"numbers": [12.5, 45.0, 78.2, 99.1, 10.4, 23.8, 55.6, 67.9, 88.0, 31.2]}),
    ("hash_payload", {"data": "performance benchmarking payload for go-anything migration testing", "algo": "sha256"}),
    ("filter_records", {
        "records": [{"id": f"item-{i}", "score": float(i * 3.5 % 100), "category": "A" if i % 2 == 0 else "B"} for i in range(50)],
        "min_score": 50.0,
        "category": "A"
    })
]

def build_go_binary():
    print(f"Compiling Go worker binary to {GO_BIN}...")
    subprocess.run(["go", "build", "-o", GO_BIN, GO_MAIN], check=True, cwd=DIR)

def run_python_bench(iterations=200):
    latencies = []
    sample_inputs = [(t, json.dumps(p)) for t, p in SAMPLE_TASKS]
    py_worker_path = os.path.join(DIR, "python", "worker.py")

    # Warmup
    for i in range(5):
        t_name, p_json = sample_inputs[i % len(sample_inputs)]
        subprocess.run(["python", py_worker_path, t_name, p_json], capture_output=True, check=True)

    t0_total = time.perf_counter()
    for i in range(iterations):
        t_name, p_json = sample_inputs[i % len(sample_inputs)]
        t0 = time.perf_counter()
        subprocess.run(["python", py_worker_path, t_name, p_json], capture_output=True, check=True)
        t1 = time.perf_counter()
        latencies.append((t1 - t0) * 1000.0) # ms
    t1_total = time.perf_counter()

    latencies.sort()
    total_time = t1_total - t0_total
    p50 = latencies[int(len(latencies) * 0.50)]
    p95 = latencies[int(len(latencies) * 0.95)]
    p99 = latencies[int(len(latencies) * 0.99)]
    throughput = int(iterations / total_time)

    return {
        "iterations": iterations,
        "total_time_seconds": round(total_time, 4),
        "throughput_tasks_per_sec": throughput,
        "p50_latency_ms": round(p50, 4),
        "p95_latency_ms": round(p95, 4),
        "p99_latency_ms": round(p99, 4),
        "latencies_sample": [round(x, 4) for x in latencies[:50]]
    }

def run_go_bench(iterations=1000):
    # Benchmark Go port execution
    latencies = []
    # Test through Go process
    sample_inputs = [(t, json.dumps(p)) for t, p in SAMPLE_TASKS]

    # Warmup
    for i in range(5):
        t_name, p_json = sample_inputs[i % len(sample_inputs)]
        subprocess.run([GO_BIN, t_name, p_json], capture_output=True, check=True)

    t0_total = time.perf_counter()
    for i in range(iterations):
        t_name, p_json = sample_inputs[i % len(sample_inputs)]
        t0 = time.perf_counter()
        subprocess.run([GO_BIN, t_name, p_json], capture_output=True, check=True)
        t1 = time.perf_counter()
        latencies.append((t1 - t0) * 1000.0) # ms
    t1_total = time.perf_counter()

    latencies.sort()
    total_time = t1_total - t0_total
    p50 = latencies[int(len(latencies) * 0.50)]
    p95 = latencies[int(len(latencies) * 0.95)]
    p99 = latencies[int(len(latencies) * 0.99)]
    throughput = int(iterations / total_time)

    return {
        "iterations": iterations,
        "total_time_seconds": round(total_time, 4),
        "throughput_tasks_per_sec": throughput,
        "p50_latency_ms": round(p50, 4),
        "p95_latency_ms": round(p95, 4),
        "p99_latency_ms": round(p99, 4),
        "latencies_sample": [round(x, 4) for x in latencies[:50]]
    }

def main():
    print("=== External Port #2 (Python Worker -> Go) Benchmark ===\n")
    build_go_binary()

    iterations = 200 # Standard iteration count for process-level measurement
    print(f"\nRunning {iterations} iterations against Python baseline...")
    py_results = run_python_bench(iterations)

    print(f"Running {iterations} iterations against Go port...")
    go_results = run_go_bench(iterations)

    with open(os.path.join(DIR, "raw_bench_python.json"), "w") as f:
        json.dump(py_results, f, indent=2)

    with open(os.path.join(DIR, "raw_bench_go.json"), "w") as f:
        json.dump(go_results, f, indent=2)

    print("\n--- Benchmark Results ---")
    print(f"Python baseline: p50={py_results['p50_latency_ms']}ms, p95={py_results['p95_latency_ms']}ms, p99={py_results['p99_latency_ms']}ms, throughput={py_results['throughput_tasks_per_sec']} tasks/s")
    print(f"Go port:         p50={go_results['p50_latency_ms']}ms, p95={go_results['p95_latency_ms']}ms, p99={go_results['p99_latency_ms']}ms, throughput={go_results['throughput_tasks_per_sec']} tasks/s")
    print(f"\nRaw benchmark data saved to raw_bench_python.json and raw_bench_go.json")

    # Cleanup binary
    if os.path.exists(GO_BIN):
        try:
            os.remove(GO_BIN)
        except Exception:
            pass

if __name__ == "__main__":
    main()
