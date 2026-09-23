// Waitlist signup: validation, abuse limits, and the Supabase write.
// Pure logic with injected fetch/env/clock so every unhappy path is testable.

import { createHash } from "node:crypto";

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;
const MAX_EMAIL = 254;
const MAX_SOURCE = 64;
const RATE_LIMIT_PER_HOUR = 5;
const DEFAULT_ORIGINS = ["https://trymonaco.xyz", "https://www.trymonaco.xyz"];

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
  return {
    apikey: env.SUPABASE_SERVICE_ROLE_KEY,
    Authorization: `Bearer ${env.SUPABASE_SERVICE_ROLE_KEY}`,
    ...extra,
  };
}

async function recentSignups({ fetch, env, ipHash, since }) {
  const url = `${env.SUPABASE_URL}/rest/v1/waitlist?select=id&ip_hash=eq.${ipHash}&created_at=gte.${encodeURIComponent(since.toISOString())}`;
  const res = await fetch(url, {
    method: "GET",
    headers: supabaseHeaders(env, { Prefer: "count=exact", Range: "0-0" }),
  });
  if (!res.ok && res.status !== 416) throw new Error(`rate-limit lookup failed: ${res.status}`);
  const range = res.headers.get("content-range") || "*/0";
  const total = Number(range.split("/")[1]);
  return Number.isFinite(total) ? total : 0;
}

async function insertSignup({ fetch, env, row }) {
  const res = await fetch(`${env.SUPABASE_URL}/rest/v1/waitlist?on_conflict=email`, {
    method: "POST",
    headers: supabaseHeaders(env, {
      "Content-Type": "application/json",
      Prefer: "resolution=ignore-duplicates,return=minimal",
    }),
    body: JSON.stringify(row),
  });
  if (!res.ok) throw new Error(`insert failed: ${res.status}`);
}

// Returns { status, body } for the HTTP layer to send.
export async function handleSignup({ method, headers, body, env, fetch, now = () => new Date(), log = console }) {
  if (method !== "POST") return { status: 405, body: { error: "Use POST." } };

  const origin = headers.origin;
  if (origin && !allowedOrigins(env).includes(origin)) {
    return { status: 403, body: { error: "Signups are only accepted from trymonaco.xyz." } };
  }

  if (!env.SUPABASE_URL || !env.SUPABASE_SERVICE_ROLE_KEY || !env.IP_HASH_SALT) {
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

  const source = typeof payload.source === "string" ? payload.source.trim().slice(0, MAX_SOURCE) : "";
  const ua = typeof headers["user-agent"] === "string" ? headers["user-agent"].slice(0, 256) : null;
  const ipHash = hashIp(clientIp(headers), env.IP_HASH_SALT);

  try {
    const since = new Date(now().getTime() - 60 * 60 * 1000);
    const count = await recentSignups({ fetch, env, ipHash, since });
    if (count >= RATE_LIMIT_PER_HOUR) {
      log.warn(JSON.stringify({ event: "waitlist_rate_limited", ipHash: ipHash.slice(0, 12) }));
      return { status: 429, body: { error: "Too many signups from this network. Try again in an hour." } };
    }
    await insertSignup({ fetch, env, row: { email, source: source || null, ip_hash: ipHash, user_agent: ua } });
  } catch (err) {
    log.error(JSON.stringify({ event: "waitlist_store_failed", message: String(err && err.message || err) }));
    return { status: 502, body: { error: "The waitlist is having trouble right now. Try again in a minute." } };
  }

  log.info(JSON.stringify({ event: "waitlist_signup", source: source || null }));
  // Same response for new and existing emails, so the endpoint doesn't reveal who signed up.
  return { status: 200, body: { ok: true } };
}

export async function checkHealth({ env, fetch }) {
  const deps = { supabase: "unconfigured" };
  if (env.SUPABASE_URL && env.SUPABASE_SERVICE_ROLE_KEY) {
    try {
      const res = await fetch(`${env.SUPABASE_URL}/rest/v1/waitlist?select=id&limit=1`, {
        headers: supabaseHeaders(env),
        signal: AbortSignal.timeout(3000),
      });
      deps.supabase = res.ok ? "ok" : `error ${res.status}`;
    } catch (err) {
      deps.supabase = "unreachable";
    }
  }
  const ok = deps.supabase === "ok" && Boolean(env.IP_HASH_SALT);
  return { status: ok ? 200 : 503, body: { status: ok ? "ok" : "degraded", dependencies: { ...deps, ipHashSalt: env.IP_HASH_SALT ? "ok" : "missing" } } };
}
