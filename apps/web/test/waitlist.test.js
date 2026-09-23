import { test } from "node:test";
import assert from "node:assert/strict";
import { handleSignup, checkHealth, normalizeEmail, clientIp } from "../lib/waitlist.js";

const env = { SUPABASE_URL: "https://db.test", SUPABASE_SERVICE_ROLE_KEY: "svc", IP_HASH_SALT: "salt" };
const quiet = { info() {}, warn() {}, error() {} };
const headers = { origin: "https://trymonaco.xyz", "x-forwarded-for": "1.2.3.4, 10.0.0.1", "user-agent": "test" };

// Fake Supabase: records calls, answers the count lookup and the insert.
function fakeFetch({ count = 0, countStatus = 206, insertStatus = 201, throws = false } = {}) {
  const calls = [];
  const fn = async (url, init) => {
    calls.push({ url, init });
    if (throws) throw new TypeError("fetch failed");
    if (init.method === "GET") {
      return new Response(null, { status: countStatus, headers: { "content-range": `0-0/${count}` } });
    }
    return new Response(null, { status: insertStatus });
  };
  fn.calls = calls;
  return fn;
}

const run = (over = {}) =>
  handleSignup({ method: "POST", headers, body: { email: "You@Email.com" }, env, fetch: fakeFetch(), log: quiet, ...over });

test("accepts a valid email and stores it lowercased with a hashed IP", async () => {
  const fetch = fakeFetch();
  const r = await run({ fetch, body: { email: "  You@Email.com ", source: "x" } });
  assert.equal(r.status, 200);
  const insert = fetch.calls.find((c) => c.init.method === "POST");
  const row = JSON.parse(insert.init.body);
  assert.equal(row.email, "you@email.com");
  assert.equal(row.source, "x");
  assert.match(row.ip_hash, /^[0-9a-f]{64}$/);
  assert.notEqual(row.ip_hash, "1.2.3.4");
  assert.match(insert.url, /on_conflict=email/);
});

test("duplicate email returns the same success response", async () => {
  // ignore-duplicates makes PostgREST answer 201/200 with no row; response must not differ.
  const r = await run({ fetch: fakeFetch({ insertStatus: 200 }) });
  assert.deepEqual(r, { status: 200, body: { ok: true } });
});

test("rejects malformed emails without touching the database", async () => {
  for (const email of ["", "nope", "a@b", "a b@c.com", 42, null, "x".repeat(250) + "@e.com"]) {
    const fetch = fakeFetch();
    const r = await run({ fetch, body: { email } });
    assert.equal(r.status, 400, `email ${JSON.stringify(email)}`);
    assert.equal(fetch.calls.length, 0);
  }
});

test("rejects malformed JSON bodies", async () => {
  assert.equal((await run({ body: "{not json" })).status, 400);
  assert.equal((await run({ body: null })).status, 400);
  assert.equal((await run({ body: '{"email":"ok@ok.com"}' })).status, 200);
});

test("honeypot submissions look successful but are not stored", async () => {
  const fetch = fakeFetch();
  const r = await run({ fetch, body: { email: "bot@spam.com", company: "Acme" } });
  assert.equal(r.status, 200);
  assert.equal(fetch.calls.length, 0);
});

test("only POST is allowed", async () => {
  assert.equal((await run({ method: "GET" })).status, 405);
});

test("rejects foreign origins, allows no origin and configured origins", async () => {
  assert.equal((await run({ headers: { ...headers, origin: "https://evil.example" } })).status, 403);
  const { origin, ...noOrigin } = headers;
  assert.equal((await run({ headers: noOrigin })).status, 200);
  const local = { ...env, ALLOWED_ORIGINS: "http://localhost:3000" };
  assert.equal((await run({ env: local, headers: { ...headers, origin: "http://localhost:3000" } })).status, 200);
  assert.equal((await run({ env: local })).status, 403);
});

test("rate limits after five signups per IP per hour", async () => {
  const fetch = fakeFetch({ count: 5 });
  const r = await run({ fetch });
  assert.equal(r.status, 429);
  assert.equal(fetch.calls.filter((c) => c.init.method === "POST").length, 0);
  assert.equal((await run({ fetch: fakeFetch({ count: 4 }) })).status, 200);
});

test("database errors and network failures return 502, not a crash", async () => {
  assert.equal((await run({ fetch: fakeFetch({ insertStatus: 500 }) })).status, 502);
  assert.equal((await run({ fetch: fakeFetch({ countStatus: 401 }) })).status, 502);
  assert.equal((await run({ fetch: fakeFetch({ throws: true }) })).status, 502);
});

test("missing configuration is a 500 with a clear message", async () => {
  const r = await run({ env: { ...env, IP_HASH_SALT: "" } });
  assert.equal(r.status, 500);
});

test("helpers", () => {
  assert.equal(normalizeEmail(" A@B.co "), "a@b.co");
  assert.equal(clientIp({ "x-forwarded-for": "9.9.9.9, 1.1.1.1" }), "9.9.9.9");
  assert.equal(clientIp({ "x-real-ip": "8.8.8.8" }), "8.8.8.8");
  assert.equal(clientIp({}), "unknown");
});

test("health reports each dependency", async () => {
  const ok = await checkHealth({ env, fetch: async () => new Response("[]", { status: 200 }) });
  assert.equal(ok.status, 200);
  assert.equal(ok.body.dependencies.supabase, "ok");
  const down = await checkHealth({ env, fetch: async () => { throw new Error("x"); } });
  assert.equal(down.status, 503);
  assert.equal(down.body.dependencies.supabase, "unreachable");
  const unconf = await checkHealth({ env: {}, fetch: async () => new Response("[]") });
  assert.equal(unconf.status, 503);
  assert.equal(unconf.body.dependencies.supabase, "unconfigured");
});
