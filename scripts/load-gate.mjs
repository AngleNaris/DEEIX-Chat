import http from "node:http";
import { performance } from "node:perf_hooks";

function required(name) {
  const value = process.env[name];
  if (!value?.trim()) throw new Error(`${name} is required`);
  return value;
}

function positiveInteger(name, fallback) {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error(`${name} must be a positive integer`);
  return value;
}

const baseURL = new URL(process.env.DEEIX_LOAD_BASE_URL ?? "http://127.0.0.1:18080");
if (!["http:", "https:"].includes(baseURL.protocol)) throw new Error("DEEIX_LOAD_BASE_URL must use HTTP or HTTPS");

const concurrency = positiveInteger("DEEIX_LOAD_CONCURRENCY", 100);
const total = positiveInteger("DEEIX_LOAD_TOTAL_REQUESTS", 10_000);
const p95Limit = positiveInteger("DEEIX_LOAD_P95_LIMIT_MS", 500);
const p99Limit = positiveInteger("DEEIX_LOAD_P99_LIMIT_MS", 1_000);
const users = [
  [required("DEEIX_LOAD_USER_A"), required("DEEIX_LOAD_PASSWORD_A")],
  [required("DEEIX_LOAD_USER_B"), required("DEEIX_LOAD_PASSWORD_B")],
];
const agent = new http.Agent({ keepAlive: true, maxSockets: concurrency });

function request(method, pathname, body, token) {
  return new Promise((resolve, reject) => {
    const payload = body ? JSON.stringify(body) : undefined;
    const started = performance.now();
    const req = http.request(
      new URL(pathname, baseURL),
      {
        method,
        agent,
        headers: {
          ...(payload ? { "content-type": "application/json", "content-length": Buffer.byteLength(payload) } : {}),
          ...(token ? { authorization: `Bearer ${token}` } : {}),
        },
      },
      (res) => {
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () =>
          resolve({
            status: res.statusCode ?? 0,
            duration: performance.now() - started,
            body: Buffer.concat(chunks).toString("utf8"),
          }),
        );
      },
    );
    req.setTimeout(10_000, () => req.destroy(new Error("request timeout")));
    req.on("error", reject);
    if (payload) req.write(payload);
    req.end();
  });
}

async function login(username, password) {
  const response = await request("POST", "/api/v1/auth/login", { username, password });
  if (response.status !== 200) throw new Error(`login failed for ${username}: HTTP ${response.status}`);
  const token = JSON.parse(response.body).data?.accessToken;
  if (!token) throw new Error(`login returned no token for ${username}`);
  return token;
}

function percentile(sorted, fraction) {
  if (sorted.length === 0) return null;
  return Number(sorted[Math.ceil(sorted.length * fraction) - 1].toFixed(2));
}

try {
  const tokens = await Promise.all(users.map(([username, password]) => login(username, password)));
  const paths = [
    "/healthz",
    "/api/v1/version",
    "/api/v1/memories/profile",
    "/api/v1/credentials",
    "/api/v1/conversation-agent-groups",
  ];
  const durations = [];
  const statusCounts = new Map();
  const sampleFailures = [];
  let errors = 0;
  let cursor = 0;
  const started = performance.now();

  await Promise.all(
    Array.from({ length: concurrency }, async () => {
      while (true) {
        const index = cursor++;
        if (index >= total) return;
        const pathname = paths[index % paths.length];
        try {
          const response = await request("GET", pathname, undefined, index % paths.length < 2 ? undefined : tokens[index % tokens.length]);
          durations.push(response.duration);
          statusCounts.set(response.status, (statusCounts.get(response.status) ?? 0) + 1);
          if (response.status < 200 || response.status >= 300) {
            errors++;
            if (sampleFailures.length < 10) sampleFailures.push(`${pathname}: HTTP ${response.status}`);
          }
        } catch (error) {
          errors++;
          if (sampleFailures.length < 10) sampleFailures.push(`${pathname}: ${error.message}`);
        }
      }
    }),
  );

  const elapsedSeconds = (performance.now() - started) / 1_000;
  durations.sort((a, b) => a - b);
  const result = {
    concurrency,
    requests: total,
    completed: durations.length,
    errors,
    qps: Number((durations.length / elapsedSeconds).toFixed(2)),
    p50Ms: percentile(durations, 0.5),
    p95Ms: percentile(durations, 0.95),
    p99Ms: percentile(durations, 0.99),
    maxMs: durations.length === 0 ? null : Number(durations.at(-1).toFixed(2)),
    statusCounts: Object.fromEntries([...statusCounts].sort(([a], [b]) => a - b)),
    sampleFailures,
  };
  console.log(JSON.stringify(result));
  if (result.completed !== total || errors !== 0 || result.p95Ms === null || result.p95Ms > p95Limit || result.p99Ms > p99Limit) {
    process.exitCode = 1;
  }
} finally {
  agent.destroy();
}
