const http = require('http');
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const DIR = __dirname;
const NODE_SERVER_PATH = path.join(DIR, 'node', 'server.js');
const GO_SERVER_DIR = path.join(DIR, 'go');

function httpRequest(url, options = {}, body = null) {
  return new Promise((resolve, reject) => {
    const parsed = new URL(url);
    const reqOpts = {
      hostname: parsed.hostname,
      port: parsed.port,
      path: parsed.pathname + parsed.search,
      method: options.method || 'GET',
      headers: options.headers || {}
    };

    const req = http.request(reqOpts, (res) => {
      let data = '';
      res.on('data', chunk => { data += chunk; });
      res.on('end', () => resolve({ statusCode: res.statusCode, body: data }));
    });

    req.on('error', reject);
    if (body) {
      req.write(typeof body === 'string' ? body : JSON.stringify(body));
    }
    req.end();
  });
}

async function waitForServer(url, timeoutMs = 5000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await httpRequest(`${url}/health`);
      if (res.statusCode === 200) return true;
    } catch (_) {}
    await new Promise(r => setTimeout(r, 50));
  }
  throw new Error(`Server at ${url} failed to become healthy within ${timeoutMs}ms`);
}

async function runBenchmark(baseURL, iterations = 500) {
  const latencies = [];
  const testEndpoints = [
    { method: 'GET', path: '/health' },
    { method: 'GET', path: '/items' },
    { method: 'GET', path: '/items/1' },
    { method: 'GET', path: '/items/999' },
    { method: 'POST', path: '/items', body: { name: "Bench Item", price: 49.99, in_stock: true } }
  ];

  // Warm-up
  for (let i = 0; i < 20; i++) {
    const ep = testEndpoints[i % testEndpoints.length];
    await httpRequest(`${baseURL}${ep.path}`, { method: ep.method }, ep.body);
  }

  // Timed run
  const benchStart = process.hrtime.bigint();
  for (let i = 0; i < iterations; i++) {
    const ep = testEndpoints[i % testEndpoints.length];
    const t0 = process.hrtime.bigint();
    await httpRequest(`${baseURL}${ep.path}`, { method: ep.method }, ep.body);
    const t1 = process.hrtime.bigint();
    latencies.push(Number(t1 - t0) / 1e6); // milliseconds
  }
  const benchEnd = process.hrtime.bigint();

  const totalTimeSec = Number(benchEnd - benchStart) / 1e9;
  latencies.sort((a, b) => a - b);

  const p50 = latencies[Math.floor(latencies.length * 0.50)];
  const p95 = latencies[Math.floor(latencies.length * 0.95)];
  const p99 = latencies[Math.floor(latencies.length * 0.99)];
  const throughput = Math.round(iterations / totalTimeSec);

  return {
    iterations,
    total_time_seconds: parseFloat(totalTimeSec.toFixed(4)),
    throughput_req_per_sec: throughput,
    p50_latency_ms: parseFloat(p50.toFixed(3)),
    p95_latency_ms: parseFloat(p95.toFixed(3)),
    p99_latency_ms: parseFloat(p99.toFixed(3)),
    latencies_sample: latencies.slice(0, 50)
  };
}

async function main() {
  console.log('=== External Port #1 (Node.js REST API -> Go) Benchmark ===\n');

  // 1. Start Node.js Server
  const nodePort = 4101;
  const nodeProc = spawn('node', [NODE_SERVER_PATH], {
    env: { ...process.env, PORT: String(nodePort) },
    stdio: 'ignore'
  });
  const nodeURL = `http://127.0.0.1:${nodePort}`;
  console.log(`Starting Node.js baseline on port ${nodePort}...`);
  await waitForServer(nodeURL);

  // 2. Start Go Server
  const goPort = 4102;
  const goProc = spawn('go', ['run', path.join(GO_SERVER_DIR, 'main.go')], {
    env: { ...process.env, PORT: String(goPort) },
    stdio: 'ignore'
  });
  const goURL = `http://127.0.0.1:${goPort}`;
  console.log(`Starting Go port on port ${goPort}...`);
  await waitForServer(goURL);

  try {
    console.log('\nRunning 500 requests against Node.js baseline...');
    const nodeResults = await runBenchmark(nodeURL, 500);

    console.log('Running 500 requests against Go port...');
    const goResults = await runBenchmark(goURL, 500);

    // Write raw data files
    fs.writeFileSync(path.join(DIR, 'raw_bench_node.json'), JSON.stringify(nodeResults, null, 2));
    fs.writeFileSync(path.join(DIR, 'raw_bench_go.json'), JSON.stringify(goResults, null, 2));

    console.log('\n--- Benchmark Results ---');
    console.log(`Node.js baseline: p50=${nodeResults.p50_latency_ms}ms, p95=${nodeResults.p95_latency_ms}ms, p99=${nodeResults.p99_latency_ms}ms, throughput=${nodeResults.throughput_req_per_sec} req/s`);
    console.log(`Go port:         p50=${goResults.p50_latency_ms}ms, p95=${goResults.p95_latency_ms}ms, p99=${goResults.p99_latency_ms}ms, throughput=${goResults.throughput_req_per_sec} req/s`);
    const speedup = (nodeResults.p50_latency_ms / goResults.p50_latency_ms).toFixed(2);
    const tpRatio = (goResults.throughput_req_per_sec / nodeResults.throughput_req_per_sec).toFixed(2);
    console.log(`Speedup (p50):    ${speedup}x`);
    console.log(`Throughput gain:  ${tpRatio}x`);
    console.log('\nRaw benchmark data saved to raw_bench_node.json and raw_bench_go.json');
  } finally {
    nodeProc.kill();
    goProc.kill();
  }
}

if (require.main === module) {
  main().catch(err => {
    console.error('Benchmark failed:', err);
    process.exit(1);
  });
}

module.exports = { runBenchmark };
