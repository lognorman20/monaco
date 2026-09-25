// The invite page's logic: parse the code out of /join/<code>, read the cabal's public
// preview from the Monaco API, and turn it into the lines the page shows.
//
// Pure and browser-safe (no DOM, no node: imports), so join/index.html loads it as a module
// and test/invite.test.js runs it under node. Keep the code rules in step with the API
// (apps/backend/internal/app/invites.go) and the app (InviteLink.swift).

// No 0, O, 1 or I: a code survives being read aloud or copied off a screenshot.
export const ALPHABET = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ";
export const CODE_LENGTH = 8;
export const INVITE_ORIGIN = "https://trymonaco.xyz";
// Placeholder until the App Store listing exists.
export const APP_STORE_URL = "https://apps.apple.com/app/monaco";
export const PREVIEW_TIMEOUT_MS = 6000;

// The app's CabalTint fills (MonacoTheme.swift, light scheme), keyed by the API's tint names.
export const TINTS = {
  pine: "#0E6E6A",
  ochre: "#8F6410",
  plum: "#7A3A66",
  indigo: "#2F5788",
  moss: "#4E5817",
};

// Uppercases, drops spaces and dashes, and checks the alphabet. Anything else is null.
export function normalizeCode(raw) {
  if (typeof raw !== "string") return null;
  const code = raw.trim().toUpperCase().replace(/[\s-]/g, "");
  if (code.length !== CODE_LENGTH) return null;
  for (const ch of code) {
    if (!ALPHABET.includes(ch)) return null;
  }
  return code;
}

// "/join/K7QM4XPD" and "/join/k7qm4xpd/" give "K7QM4XPD". Any other path gives null.
export function codeFromPath(pathname) {
  const match = /^\/join\/([^/]+)\/?$/.exec(typeof pathname === "string" ? pathname : "");
  if (!match) return null;
  let segment;
  try {
    segment = decodeURIComponent(match[1]);
  } catch {
    return null;
  }
  return normalizeCode(segment);
}

// Two groups of four, the way the app shows it: "K7QM 4XPD".
export function displayCode(code) {
  return `${code.slice(0, 4)} ${code.slice(4)}`;
}

export function deepLink(code) {
  return `monaco://join/${code}`;
}

export function inviteLink(code) {
  return `${INVITE_ORIGIN}/join/${code}`;
}

// The preview route for code under base, or null when base is not an absolute http(s) URL.
// An empty base is how the page is deployed before the API has a public address.
export function previewURL(base, code) {
  if (typeof base !== "string" || !/^https?:\/\/[^/]/i.test(base.trim())) return null;
  return `${base.trim().replace(/\/+$/, "")}/v1/invites/${encodeURIComponent(code)}`;
}

// Validates the API's answer field by field. Anything the page would print or load has to
// have the right shape; a preview without a usable name is no preview at all.
export function parsePreview(body) {
  if (!body || typeof body !== "object") return null;
  const name = typeof body.name === "string" ? body.name.trim() : "";
  if (!name) return null;
  const memberCount = Number.isInteger(body.memberCount) && body.memberCount >= 0 ? body.memberCount : null;
  const pot = typeof body.potValueUsd === "string" && /^-?\d+(\.\d+)?$/.test(body.potValueUsd) ? body.potValueUsd : null;
  const picture = typeof body.pictureUrl === "string" && /^https:\/\//i.test(body.pictureUrl) ? body.pictureUrl : null;
  return {
    code: typeof body.code === "string" ? body.code : null,
    groupId: typeof body.groupId === "string" ? body.groupId : null,
    name,
    memberCount,
    tint: Object.hasOwn(TINTS, body.tint) ? body.tint : "pine",
    pictureUrl: picture,
    joinPolicy: body.joinPolicy === "request" ? "request" : "open",
    potValueUsd: pot,
  };
}

// Reads the preview. Resolves to { status: "ok", preview }, { status: "not_found" } for a
// dead code, or { status: "unavailable" } when the API cannot be asked or does not answer
// sensibly. It never throws: every outcome still renders a page with the code on it.
export async function fetchPreview({ base, code, fetch, timeoutMs = PREVIEW_TIMEOUT_MS }) {
  const url = previewURL(base, code);
  if (!url || typeof fetch !== "function") return { status: "unavailable" };
  const ctrl = typeof AbortController === "function" ? new AbortController() : null;
  const timer = ctrl ? setTimeout(() => ctrl.abort(), timeoutMs) : null;
  try {
    const res = await fetch(url, { headers: { Accept: "application/json" }, signal: ctrl ? ctrl.signal : undefined });
    if (res.status === 404) return { status: "not_found" };
    if (!res.ok) return { status: "unavailable" };
    const preview = parsePreview(await res.json());
    return preview ? { status: "ok", preview } : { status: "unavailable" };
  } catch {
    return { status: "unavailable" };
  } finally {
    if (timer) clearTimeout(timer);
  }
}

export function tintColor(tint) {
  return TINTS[tint] || TINTS.pine;
}

// Same rule as the app's CabalMark.initials: first letters of the first and last significant
// words, connectors skipped. "Semis or bust" gives "SB", "Rent" gives "R".
const SKIPPED = new Set(["or", "of", "the", "and", "a", "an", "&", "+", "to", "in", "on", "for", "with", "at", "by"]);
export function initials(name) {
  const trimmed = typeof name === "string" ? name.trim() : "";
  const words = trimmed.split(/\s+/).filter((w) => /^[\p{L}\p{N}]/u.test(w));
  const significant = words.filter((w) => !SKIPPED.has(w.toLowerCase()));
  const pool = significant.length ? significant : words;
  if (!pool.length) return trimmed ? Array.from(trimmed)[0] : "";
  const first = Array.from(pool[0])[0];
  if (pool.length === 1) return first.toUpperCase();
  return (first + Array.from(pool[pool.length - 1])[0]).toUpperCase();
}

export function memberLine(count) {
  if (!Number.isInteger(count) || count < 1) return null;
  return count === 1 ? "1 member" : `${count.toLocaleString("en-US")} members`;
}

const USD = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });
export function potLine(potValueUsd) {
  if (typeof potValueUsd !== "string") return null;
  const value = Number(potValueUsd);
  if (!Number.isFinite(value)) return null;
  if (value <= 0) return "Nothing in the pot yet";
  return `${USD.format(value)} in the pot`;
}

export function policyLine(joinPolicy) {
  return joinPolicy === "request"
    ? "The admin approves new members. Ask to join from the app."
    : "Anyone with this link can join.";
}

// Everything the page prints for one outcome of fetchPreview, so the copy is tested here
// rather than in the DOM code.
export function pageModel(code, result) {
  if (!code) {
    return {
      state: "invalid",
      docTitle: "Invite link · Monaco",
      kicker: null,
      title: "This link doesn't look right",
      lines: ["Ask your friend to send the invite again."],
      showCode: false,
    };
  }
  if (result.status === "loading") {
    return {
      state: "loading",
      docTitle: "Join a cabal · Monaco",
      kicker: "You're invited to",
      title: null,
      lines: [],
      showCode: true,
    };
  }
  if (result.status === "not_found") {
    return {
      state: "not_found",
      docTitle: "Invite link · Monaco",
      kicker: null,
      title: "This invite no longer works",
      lines: ["Ask someone in the cabal for a new link."],
      showCode: false,
    };
  }
  if (result.status === "ok") {
    const p = result.preview;
    const facts = [memberLine(p.memberCount), potLine(p.potValueUsd)].filter(Boolean).join(" · ");
    return {
      state: "ok",
      docTitle: `Join ${p.name} · Monaco`,
      kicker: "You're invited to",
      title: p.name,
      lines: [facts, policyLine(p.joinPolicy)].filter(Boolean),
      showCode: true,
      mark: { initials: initials(p.name), color: tintColor(p.tint), pictureUrl: p.pictureUrl },
    };
  }
  return {
    state: "unavailable",
    docTitle: "Join a cabal · Monaco",
    kicker: null,
    title: "You're invited to a cabal",
    lines: ["Open the invite in Monaco to see who's in."],
    showCode: true,
  };
}
