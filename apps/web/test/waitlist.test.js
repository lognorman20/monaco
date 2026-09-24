import { test } from "node:test";
import assert from "node:assert/strict";
import { handleSignup, checkHealth, normalizeEmail, clientIp } from "../lib/waitlist.js";

const env = { SUPABASE_URL: "https://db.test", SUPABASE_ANON_KEY: "anon", IP_HASH_SALT: "salt" };
const quiet = { info() {}, warn() {}, error() {} };
const headers = { origin: "https://trymonaco.xyz", "x-forwarded-for": "1.2.3.4, 10.0.0.1", "user-agent": "test" };

// Fake PostgREST: records calls and answers RPCs with a fixed result.
function fakeFetch({ result = "ok", status = 200, throws = false } = {}) {
  const calls = [];
  const fn = async (url, init) => {
    calls.push({ url, init });
    if (throws) throw new TypeError("fetch failed");
    return new Response(JSON.stringify(result), { status, headers: { "content-type": "application/json" } });
  };
  fn.calls = calls;
  return fn;
}

const run = (over = {}) =>
  handleSignup({ method: "POST", headers, body: { email: "You@Email.com" }, env, fetch: fakeFetch(), log: quiet, ...over });

test("valid email calls join_waitlist with the anon key and a hashed IP", async () => {
  const fetch = fakeFetch();
  const r = await run({ fetch, body: { email: "  You@Email.com ", source: "x" } });
  assert.equal(r.status, 200);
  assert.equal(fetch.calls.length, 1);
  const { url, init } = fetch.calls[0];
  assert.equal(url, "https://db.test/rest/v1/rpc/join_waitlist");
  assert.equal(init.headers.apikey, "anon");
  const args = JSON.parse(init.body);
  assert.equal(args.p_email, "you@email.com");
  assert.equal(args.p_secret, undefined);
  assert.equal(args.p_source, "x");
  assert.match(args.p_ip_hash, /^[0-9a-f]{64}$/);
  assert.ok(!init.body.includes("1.2.3.4"));
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

test("maps database results to HTTP responses", async () => {
  assert.equal((await run({ fetch: fakeFetch({ result: "rate_limited" }) })).status, 429);
  assert.equal((await run({ fetch: fakeFetch({ result: "invalid_email" }) })).status, 400);
  assert.equal((await run({ fetch: fakeFetch({ result: "busy" }) })).status, 503);
  assert.equal((await run({ fetch: fakeFetch({ result: "invalid_request" }) })).status, 500);
  assert.equal((await run({ fetch: fakeFetch({ result: "something new" }) })).status, 500);
});

test("database errors and network failures return 502, not a crash", async () => {
  assert.equal((await run({ fetch: fakeFetch({ status: 500, result: { message: "boom" } }) })).status, 502);
  assert.equal((await run({ fetch: fakeFetch({ status: 404, result: {} }) })).status, 502);
  assert.equal((await run({ fetch: fakeFetch({ throws: true }) })).status, 502);
});

test("missing configuration is a 500 and never calls the database", async () => {
  for (const key of ["SUPABASE_URL", "SUPABASE_ANON_KEY", "IP_HASH_SALT"]) {
    const fetch = fakeFetch();
    const r = await run({ fetch, env: { ...env, [key]: "" } });
    assert.equal(r.status, 500, key);
    assert.equal(fetch.calls.length, 0);
  }
});

test("helpers", () => {
  assert.equal(normalizeEmail(" A@B.co "), "a@b.co");
  assert.equal(clientIp({ "x-forwarded-for": "9.9.9.9, 1.1.1.1" }), "9.9.9.9");
  assert.equal(clientIp({ "x-real-ip": "8.8.8.8" }), "8.8.8.8");
  assert.equal(clientIp({}), "unknown");
});

test("health reports each dependency", async () => {
  const ok = await checkHealth({ env, fetch: fakeFetch({ result: true }) });
  assert.equal(ok.status, 200);
  assert.equal(ok.body.dependencies.supabase, "ok");
  const noTable = await checkHealth({ env, fetch: fakeFetch({ result: false }) });
  assert.equal(noTable.status, 503);
  const down = await checkHealth({ env, fetch: fakeFetch({ throws: true }) });
  assert.equal(down.body.dependencies.supabase, "unreachable");
  const unconf = await checkHealth({ env: {}, fetch: fakeFetch() });
  assert.equal(unconf.status, 503);
  assert.equal(unconf.body.dependencies.ipHashSalt, "missing");
});

test("an optional twitter handle is passed through, trimmed, de-@'d, and capped", async () => {
  const fetch = fakeFetch();
  await run({ fetch, body: { email: "you@email.com", twitter: "  @adalovelace  " } });
  assert.equal(JSON.parse(fetch.calls[0].init.body).p_twitter, "adalovelace");

  const long = fakeFetch();
  await run({ fetch: long, body: { email: "you@email.com", twitter: "a".repeat(200) } });
  assert.equal(JSON.parse(long.calls[0].init.body).p_twitter.length, 15);
});

test("no twitter handle is absent, not an empty string, and never blocks the signup", async () => {
  for (const twitter of [undefined, "", "   ", "@", 42, null, {}]) {
    const fetch = fakeFetch();
    const r = await run({ fetch, body: { email: "you@email.com", twitter } });
    assert.equal(r.status, 200, `twitter ${JSON.stringify(twitter)} should still sign up`);
    assert.equal(JSON.parse(fetch.calls[0].init.body).p_twitter, null);
  }
});

test("a twitter handle cannot smuggle control characters into an email we later send", async () => {
  const fetch = fakeFetch();
  await run({ fetch, body: { email: "you@email.com", twitter: "ada\r\nBcc: someone@else.com" } });
  const sent = JSON.parse(fetch.calls[0].init.body).p_twitter;
  assert.equal(sent, "adaBcc: someone");
  assert.ok(!/[\r\n]/.test(sent));
});

test("the signup log records that a twitter handle was given, never the handle", async () => {
  const lines = [];
  await run({ body: { email: "you@email.com", twitter: "adalovelace" }, log: { ...quiet, info: (l) => lines.push(l) } });
  const line = lines.find((l) => l.includes("waitlist_signup"));
  assert.ok(line.includes('"hasTwitter":true'));
  assert.ok(!line.includes("adalovelace"));
});
