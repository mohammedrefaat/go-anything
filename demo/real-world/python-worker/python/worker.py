import sys
import json
import hashlib
import statistics

def task_calc_stats(payload):
    numbers = payload.get("numbers", [])
    if not numbers or not isinstance(numbers, list):
        return {"error": "numbers list required"}
    return {
        "count": len(numbers),
        "mean": float(statistics.mean(numbers)),
        "median": float(statistics.median(numbers)),
        "min": float(min(numbers)),
        "max": float(max(numbers)),
        "sum": float(sum(numbers))
    }

def task_hash_payload(payload):
    data = payload.get("data", "")
    algo = payload.get("algo", "sha256")
    if not isinstance(data, str):
        return {"error": "data string required"}
    encoded = data.encode("utf-8")
    if algo == "sha256":
        digest = hashlib.sha256(encoded).hexdigest()
    elif algo == "md5":
        digest = hashlib.md5(encoded).hexdigest()
    elif algo == "sha1":
        digest = hashlib.sha1(encoded).hexdigest()
    else:
        return {"error": f"unsupported algorithm: {algo}"}
    return {"algo": algo, "digest": digest, "bytes": len(encoded)}

def task_filter_records(payload):
    records = payload.get("records", [])
    min_score = payload.get("min_score", 0.0)
    category = payload.get("category")
    if not isinstance(records, list):
        return {"error": "records list required"}

    filtered = []
    for r in records:
        score = float(r.get("score", 0.0))
        if score >= min_score:
            if category is None or r.get("category") == category:
                filtered.append(r)
    return {"total_input": len(records), "filtered_count": len(filtered), "results": filtered}

def process_job(job_type, payload):
    handlers = {
        "calc_stats": task_calc_stats,
        "hash_payload": task_hash_payload,
        "filter_records": task_filter_records
    }
    handler = handlers.get(job_type)
    if not handler:
        return {"status": "error", "error": f"unknown task: {job_type}"}
    res = handler(payload)
    if "error" in res:
        return {"status": "error", "error": res["error"]}
    return {"status": "success", "result": res}

def main():
    if len(sys.argv) < 3:
        print(json.dumps({"error": "usage: worker.py <task_name> <json_payload>"}))
        sys.exit(1)
    task_name = sys.argv[1]
    raw_payload = sys.argv[2]
    try:
        payload = json.loads(raw_payload)
    except Exception as e:
        print(json.dumps({"status": "error", "error": f"malformed json: {e}"}))
        sys.exit(2)
    output = process_job(task_name, payload)
    print(json.dumps(output))

if __name__ == "__main__":
    main()
