"""
go_anything_pytest: Pytest plugin and runner hook for go-anything behavioral test recording.
Captures test executions, function calls, inputs, and outputs when GO_ANYTHING_RECORD is set.
"""

import json
import os
import sys
import time

TARGET_DEST = os.environ.get("GO_ANYTHING_RECORD")
TARGET_UNIT = os.environ.get("GO_ANYTHING_UNIT", "default-unit")


def record_observation(unit_id, test_name, inp, out, duration_ms=0.0):
    if not TARGET_DEST:
        return

    if TARGET_UNIT and unit_id and TARGET_UNIT != unit_id:
        return

    record = {
        "unit_id": unit_id or TARGET_UNIT,
        "test_name": test_name,
        "input": inp,
        "output": out,
        "duration_ms": duration_ms,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }

    target_file = TARGET_DEST
    if os.path.isdir(TARGET_DEST):
        target_file = os.path.join(TARGET_DEST, f"{unit_id or TARGET_UNIT}_observed.jsonl")

    os.makedirs(os.path.dirname(os.path.abspath(target_file)), exist_ok=True)
    with open(target_file, "a", encoding="utf-8") as f:
        f.write(json.dumps(record) + "\n")


# Pytest hook implementations
def pytest_runtest_call(item):
    if not TARGET_DEST:
        return
    start = time.time()
    try:
        pass
    finally:
        dur = (time.time() - start) * 1000.0


def pytest_runtest_makereport(item, call):
    if not TARGET_DEST:
        return
    if call.when == "call":
        dur = getattr(call, "duration", 0.0) * 1000.0
        # Capture test name and report
        inp = {"test": item.name, "nodeid": item.nodeid, "params": getattr(item, "callspec", {}).params if hasattr(item, "callspec") else {}}
        out = {"status": "passed" if not call.excinfo else "failed"}
        record_observation(TARGET_UNIT, item.nodeid, inp, out, dur)
