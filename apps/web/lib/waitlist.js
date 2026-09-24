// Waitlist signup: validation and origin checks here; the rate limit and write
// happen atomically in the join_waitlist() database function.
// Pure logic with injected fetch/env/clock so every unhappy path is testable.

import { createHash } from "node:crypto";

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;
const MAX_EMAIL = 254;
const MAX_TWITTER = 15; // X/Twitter's own handle length limit.
const MAX_SOURCE = 64;
const DEFAULT_ORIGINS = [
  "https://monacolabs.xyz", "https://www.monacolabs.xyz",
  "https://trymonaco.xyz", "https://www.trymonaco.xyz",
];

export function allowedOrigins(env) {
  const raw = (env.ALLOWED_ORIGINS || "").trim();
  return raw ? raw.split(",").map((s) => s.trim()).filter(Boolean) : DEFAULT_ORIGINS;
}

export function normalizeEmail(value) {
  if (typeof value !== "string") return null;
  const email = value.trim().toLowerCase();
  if (email.length === 0 || email.length > MAX_EMAIL || !EMAIL.test(email)) return null;
  return email;
}

export function clientIp(headers) {
  const fwd = headers["x-forwarded-for"];
  const first = (Array.isArray(fwd) ? fwd[0] : fwd || "").split(",")[0].trim();
  return first || headers["x-real-ip"] || "unknown";
}

export function hashIp(ip, salt) {
  return createHash("sha256").update(`${salt}:${ip}`).digest("hex");
}

function supabaseHeaders(env, extra = {}) {
  // Anon (publishable) key only. It can call join_waitlist() and nothing else.
  return {
    apikey: env.SUPABASE_ANON_KEY,
    Authorization: `Bearer ${env.SUPABASE_ANON_KEY}`,
    "Content-Type": "application/json",
    ...extra,
  };
}

async function rpc({ fetch, env, name, args, timeoutMs = 5000 }) {
  const res = await fetch(`${env.SUPABASE_URL}/rest/v1/rpc/${name}`, {
    method: "POST",
    headers: supabaseHeaders(env),
    body: JSON.stringify(args),
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (!res.ok) throw new Error(`rpc ${name} failed: ${res.status}`);
  return res.json();
}

// Returns { status, body } for the HTTP layer to send.
export async function handleSignup({ method, headers, body, env, fetch, log = console }) {
  if (method !== "POST") return { status: 405, body: { error: "Use POST." } };

  const origin = headers.origin;
  if (origin && !allowedOrigins(env).includes(origin)) {
    return { status: 403, body: { error: "Signups are only accepted from monacolabs.xyz." } };
  }

  if (!env.SUPABASE_URL || !env.SUPABASE_ANON_KEY || !env.IP_HASH_SALT) {
    log.error(JSON.stringify({ event: "waitlist_misconfigured" }));
    return { status: 500, body: { error: "Waitlist is not configured." } };
  }

  let payload = body;
  if (typeof payload === "string") {
    try { payload = JSON.parse(payload); } catch { payload = null; }
  }
  if (!payload || typeof payload !== "object") return { status: 400, body: { error: "Send JSON with an email field." } };

  // Honeypot: bots fill the hidden field. Pretend success so they don't adapt.
  if (typeof payload.company === "string" && payload.company.trim() !== "") {
    log.info(JSON.stringify({ event: "waitlist_honeypot" }));
    return { status: 200, body: { ok: true } };
  }

  const email = normalizeEmail(payload.email);
  if (!email) return { status: 400, body: { error: "Enter a valid email, like you@email.com." } };

  // Optional. Leading @ stripped since we add it back for display. Control characters are
  // stripped so a handle cannot smuggle newlines into an email we later send; the database
  // enforces the same rule for callers that skip this page.
  const twitter = typeof payload.twitter === "string"
    ? payload.twitter.replace(/[\u0000-\u001F\u007F]/g, "").trim().replace(/^@+/, "").slice(0, MAX_TWITTER)
    : "";
  const source = typeof payload.source === "string" ? payload.source.trim().slice(0, MAX_SOURCE) : "";
  const ua = typeof headers["user-agent"] === "string" ? headers["user-agent"].slice(0, 256) : null;
  const ipHash = hashIp(clientIp(headers), env.IP_HASH_SALT);

  let result;
  try {
    result = await rpc({
      fetch, env, name: "join_waitlist",
      args: { p_email: email, p_twitter: twitter || null, p_source: source || null, p_ip_hash: ipHash, p_user_agent: ua },
    });
  } catch (err) {
    log.error(JSON.stringify({ event: "waitlist_store_failed", message: String(err && err.message || err) }));
    return { status: 502, body: { error: "The waitlist is having trouble right now. Try again in a minute." } };
  }

  if (result === "rate_limited") {
    log.warn(JSON.stringify({ event: "waitlist_rate_limited", ipHash: ipHash.slice(0, 12) }));
    return { status: 429, body: { error: "Too many signups from this network. Try again in an hour." } };
  }
  if (result === "busy") {
    log.warn(JSON.stringify({ event: "waitlist_global_limit" }));
    return { status: 503, body: { error: "Lots of people are signing up right now. Try again in a minute." } };
  }
  if (result === "invalid_email") return { status: 400, body: { error: "Enter a valid email, like you@email.com." } };
  if (result !== "ok") {
    // invalid_request or anything unexpected: our request or the database is wrong, not the visitor.
    log.error(JSON.stringify({ event: "waitlist_rejected_by_db", result }));
    return { status: 500, body: { error: "The waitlist is having trouble right now. Try again in a minute." } };
  }

  log.info(JSON.stringify({ event: "waitlist_signup", source: source || null, hasTwitter: Boolean(twitter) }));
  // Same response for new and existing emails, so the endpoint doesn't reveal who signed up.
  return { status: 200, body: { ok: true } };
}

export async function checkHealth({ env, fetch }) {
  const deps = {
    supabase: "unconfigured",
    ipHashSalt: env.IP_HASH_SALT ? "ok" : "missing",
  };
  if (env.SUPABASE_URL && env.SUPABASE_ANON_KEY) {
    try {
      const ready = await rpc({ fetch, env, name: "waitlist_ready", args: {}, timeoutMs: 3000 });
      deps.supabase = ready === true ? "ok" : "waitlist table missing";
    } catch (err) {
      deps.supabase = "unreachable";
    }
  }
  const ok = Object.values(deps).every((v) => v === "ok");
  return { status: ok ? 200 : 503, body: { status: ok ? "ok" : "degraded", dependencies: deps } };
}
