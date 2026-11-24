/**
 * jest_recorder: Jest and Node test recording hook for go-anything.
 * Captures test executions and function observations when GO_ANYTHING_RECORD is set.
 */

const fs = require('fs');
const path = require('path');

const targetDest = process.env.GO_ANYTHING_RECORD;
const targetUnit = process.env.GO_ANYTHING_UNIT || 'default-unit';

function recordObservation(unitId, testName, input, output, durationMs = 0) {
  if (!targetDest) return;

  const resolvedUnit = unitId || targetUnit;
  if (process.env.GO_ANYTHING_UNIT && process.env.GO_ANYTHING_UNIT !== resolvedUnit) {
    return;
  }

  const record = {
    unit_id: resolvedUnit,
    test_name: testName,
    input: input,
    output: output,
    duration_ms: durationMs,
    timestamp: new Date().toISOString()
  };

  let targetFile = targetDest;
  try {
    if (fs.existsSync(targetDest) && fs.statSync(targetDest).isDirectory()) {
      targetFile = path.join(targetDest, `${resolvedUnit}_observed.jsonl`);
    }
  } catch (e) {
    // Treat as file path
  }

  const dir = path.dirname(path.resolve(targetFile));
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }

  fs.appendFileSync(targetFile, JSON.stringify(record) + '\n', 'utf8');
}

// Jest custom reporter integration
class GoAnythingJestReporter {
  constructor(globalConfig, options) {
    this._globalConfig = globalConfig;
    this._options = options;
  }

  onTestCaseResult(test, testCaseResult) {
    if (!targetDest) return;
    const dur = testCaseResult.duration || 0;
    const inp = { test: testCaseResult.fullName, title: testCaseResult.title };
    const out = { status: testCaseResult.status };
    recordObservation(targetUnit, testCaseResult.fullName, inp, out, dur);
  }
}

module.exports = GoAnythingJestReporter;
module.exports.recordObservation = recordObservation;
