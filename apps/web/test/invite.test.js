// The invite page: code parsing, the preview fetch and every page state it can end in, the
// Cloudflare function that serves /join/<code>, and the host config that makes the links
// universal. No network: fetch and the Pages ASSETS binding are fakes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  ALPHABET, APP_STORE_URL, codeFromPath, deepLink, displayCode, fetchPreview, initials, inviteLink,
  memberLine, normalizeCode, pageModel, parsePreview, potLine, previewURL, tintColor, TINTS,
} from "../lib/invite.js";
import { onRequest as joinPage, JOIN_PAGE_HEADERS } from "../functions/join/[code].js";

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

const PREVIEW = {
  code: "K7QM4XPD",
  groupId: "5b1f0c9e-0005-4c55-9a51-000000000005",
  name: "Sunday Investors",
  memberCount: 9,
  tint: "indigo",
  pictureUrl: null,
  joinPolicy: "open",
  potValueUsd: "1240.50",
};

function fakeFetch({ status = 200, body = PREVIEW, throws = false, badJson = false } = {}) {
  const calls = [];
  const fn = async (url, init) => {
    calls.push({ url, init });
    if (throws) throw new TypeError("Failed to fetch");
    const text = badJson ? "<html>not json</html>" : JSON.stringify(body);
    return new Response(text, { status, headers: { "content-type": "application/json" } });
  };
  fn.calls = calls;
  return fn;
}

// --- codes ---

test("the alphabet has 32 symbols and none a person could misread", () => {
  assert.equal(ALPHABET.length, 32);
  for (const ambiguous of "0O1I") assert.ok(!ALPHABET.includes(ambiguous), ambiguous);
});

test("normalizeCode accepts what people type and rejects the rest", () => {
  assert.equal(normalizeCode("K7QM4XPD"), "K7QM4XPD");
  assert.equal(normalizeCode("k7qm4xpd"), "K7QM4XPD");
  assert.equal(normalizeCode(" K7QM 4XPD "), "K7QM4XPD");
  assert.equal(normalizeCode("k7qm-4xpd"), "K7QM4XPD");
  for (const bad of ["", "K7QM4XP", "K7QM4XPDA", "AB12CD34", "K7QM4XPO", "K7QM4XP0", "K7QM4XPI", "K7QM_4XP", null, 42]) {
    assert.equal(normalizeCode(bad), null, String(bad));
  }
});

test("codeFromPath reads /join/<code> and nothing else", () => {
  assert.equal(codeFromPath("/join/K7QM4XPD"), "K7QM4XPD");
  assert.equal(codeFromPath("/join/k7qm4xpd/"), "K7QM4XPD");
  assert.equal(codeFromPath("/join/K7QM%204XPD"), "K7QM4XPD");
  for (const bad of ["/join/", "/join", "/", "/join/K7QM4XPD/extra", "/joins/K7QM4XPD", "/join/%E0%A4%A", "/join/AB12CD34"]) {
    assert.equal(codeFromPath(bad), null, bad);
  }
});

test("the code's forms: grouped for reading, the deep link and the shared link", () => {
  assert.equal(displayCode("K7QM4XPD"), "K7QM 4XPD");
  assert.equal(deepLink("K7QM4XPD"), "monaco://join/K7QM4XPD");
  assert.equal(inviteLink("K7QM4XPD"), "https://trymonaco.xyz/join/K7QM4XPD");
  assert.equal(APP_STORE_URL, "https://apps.apple.com/app/monaco");
});

// --- preview ---

test("previewURL needs an absolute http(s) base and tolerates a trailing slash", () => {
  assert.equal(previewURL("https://api.trymonaco.xyz", "K7QM4XPD"), "https://api.trymonaco.xyz/v1/invites/K7QM4XPD");
  assert.equal(previewURL("https://api.trymonaco.xyz/", "K7QM4XPD"), "https://api.trymonaco.xyz/v1/invites/K7QM4XPD");
  for (const bad of ["", undefined, null, "/api", "api.trymonaco.xyz", "javascript:alert(1)"]) {
    assert.equal(previewURL(bad, "K7QM4XPD"), null, String(bad));
  }
});

test("a live code previews the cabal", async () => {
  const fetch = fakeFetch();
  const result = await fetchPreview({ base: "https://api.test", code: "K7QM4XPD", fetch });
  assert.equal(result.status, "ok");
  assert.deepEqual(result.preview, PREVIEW);
  assert.equal(fetch.calls[0].url, "https://api.test/v1/invites/K7QM4XPD");
  assert.equal(fetch.calls[0].init.headers.Accept, "application/json");
});

test("a dead code is not_found; everything else that goes wrong is unavailable", async () => {
  const base = "https://api.test";
  const code = "K7QM4XPD";
  assert.equal((await fetchPreview({ base, code, fetch: fakeFetch({ status: 404, body: { error: "invite not found" } }) })).status, "not_found");
  assert.equal((await fetchPreview({ base, code, fetch: fakeFetch({ status: 500, body: { error: "boom" } }) })).status, "unavailable");
  assert.equal((await fetchPreview({ base, code, fetch: fakeFetch({ status: 429, body: { error: "slow" } }) })).status, "unavailable");
  assert.equal((await fetchPreview({ base, code, fetch: fakeFetch({ throws: true }) })).status, "unavailable");
  assert.equal((await fetchPreview({ base, code, fetch: fakeFetch({ badJson: true }) })).status, "unavailable");
  assert.equal((await fetchPreview({ base, code, fetch: fakeFetch({ body: { name: "" } }) })).status, "unavailable");
});

test("no API base means no request at all", async () => {
  const fetch = fakeFetch();
  const result = await fetchPreview({ base: "", code: "K7QM4XPD", fetch });
  assert.equal(result.status, "unavailable");
  assert.equal(fetch.calls.length, 0);
});

test("a preview that never answers times out as unavailable", async () => {
  const hang = (_url, init) => new Promise((_resolve, reject) => {
    init.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
  });
  const result = await fetchPreview({ base: "https://api.test", code: "K7QM4XPD", fetch: hang, timeoutMs: 20 });
  assert.equal(result.status, "unavailable");
});

test("parsePreview keeps only fields of the right shape", () => {
  const parsed = parsePreview({
    ...PREVIEW,
    memberCount: -1,
    tint: "purple",
    pictureUrl: "http://insecure.example/p.png",
    joinPolicy: "weird",
    potValueUsd: "lots",
  });
  assert.equal(parsed.memberCount, null);
  assert.equal(parsed.tint, "pine");
  assert.equal(parsed.pictureUrl, null);
  assert.equal(parsed.joinPolicy, "open");
  assert.equal(parsed.potValueUsd, null);
  assert.equal(parsePreview({ ...PREVIEW, pictureUrl: "https://cdn.test/p.png" }).pictureUrl, "https://cdn.test/p.png");
  assert.equal(parsePreview(null), null);
  assert.equal(parsePreview({ name: "   " }), null);
});

// --- what the page says ---

test("the mark matches the app: tint fills and CabalMark initials", () => {
  assert.equal(tintColor("indigo"), TINTS.indigo);
  assert.equal(tintColor("nope"), TINTS.pine);
  const cases = [
    ["Weekend investors", "WI"], ["Semis or bust", "SB"], ["The Rent Money Club", "RC"],
    ["Bulls & bears", "BB"], ["Rent", "R"], ["The", "T"], ["4B dorm fund", "4F"],
    ["Élan vital", "ÉV"], ["", ""], ["!!!", "!"],
  ];
  for (const [name, want] of cases) assert.equal(initials(name), want, name);
});

test("member and pot lines", () => {
  assert.equal(memberLine(1), "1 member");
  assert.equal(memberLine(9), "9 members");
  assert.equal(memberLine(1200), "1,200 members");
  assert.equal(memberLine(0), null);
  assert.equal(memberLine(null), null);
  assert.equal(potLine("1240.50"), "$1,240.50 in the pot");
  assert.equal(potLine("0.00"), "Nothing in the pot yet");
  assert.equal(potLine(null), null);
});

test("every page state keeps the words and hides what cannot work", () => {
  const ok = pageModel("K7QM4XPD", { status: "ok", preview: PREVIEW });
  assert.equal(ok.title, "Sunday Investors");
  assert.equal(ok.docTitle, "Join Sunday Investors · Monaco");
  assert.deepEqual(ok.lines, ["9 members · $1,240.50 in the pot", "Anyone with this link can join."]);
  assert.deepEqual(ok.mark, { initials: "SI", color: TINTS.indigo, pictureUrl: null });
  assert.equal(ok.showCode, true);

  const request = pageModel("K7QM4XPD", { status: "ok", preview: { ...PREVIEW, joinPolicy: "request" } });
  assert.match(request.lines[1], /admin approves/);

  const loading = pageModel("K7QM4XPD", { status: "loading" });
  assert.equal(loading.title, null);
  assert.equal(loading.showCode, true);

  const offline = pageModel("K7QM4XPD", { status: "unavailable" });
  assert.equal(offline.showCode, true, "the code and buttons still show when the API is down");

  const dead = pageModel("K7QM4XPD", { status: "not_found" });
  assert.equal(dead.showCode, false);
  assert.match(dead.title, /no longer works/);

  const invalid = pageModel(null);
  assert.equal(invalid.showCode, false);
  assert.match(invalid.title, /doesn't look right/);

  const banned = /\b(wallet|group|club|nav|p&l|treasury|mint)\b/i;
  for (const model of [ok, request, loading, offline, dead, invalid]) {
    for (const line of [model.title, model.kicker, ...model.lines].filter(Boolean)) {
      assert.ok(!banned.test(line), `copy uses a banned word: ${line}`);
    }
  }
});

// --- Cloudflare function ---

function fakeAssets(status = 200) {
  const calls = [];
  return {
    calls,
    async fetch(url) {
      calls.push(String(url));
      return new Response(status === 200 ? "<!doctype html><title>join</title>" : "missing", {
        status,
        headers: { "content-type": "text/html" },
      });
    },
  };
}

test("/join/<code> serves the static join page with its headers", async () => {
  const ASSETS = fakeAssets();
  const res = await joinPage({ request: new Request("https://trymonaco.xyz/join/K7QM4XPD"), env: { ASSETS } });
  assert.equal(res.status, 200);
  assert.deepEqual(ASSETS.calls, ["https://trymonaco.xyz/join/"]);
  assert.match(await res.text(), /<title>join<\/title>/);
  for (const [name, value] of Object.entries(JOIN_PAGE_HEADERS)) assert.equal(res.headers.get(name), value, name);
});

test("the function answers HEAD without a body and refuses writes", async () => {
  const head = await joinPage({ request: new Request("https://trymonaco.xyz/join/K7QM4XPD", { method: "HEAD" }), env: { ASSETS: fakeAssets() } });
  assert.equal(head.status, 200);
  assert.equal(await head.text(), "");
  const post = await joinPage({ request: new Request("https://trymonaco.xyz/join/K7QM4XPD", { method: "POST" }), env: { ASSETS: fakeAssets() } });
  assert.equal(post.status, 405);
});

test("a missing page is passed through rather than dressed up as a 200", async () => {
  const res = await joinPage({ request: new Request("https://trymonaco.xyz/join/K7QM4XPD"), env: { ASSETS: fakeAssets(404) } });
  assert.equal(res.status, 404);
});

// --- host config ---

test("apple-app-site-association opens /join/* in the app", () => {
  const aasa = JSON.parse(read(".well-known/apple-app-site-association"));
  const [detail] = aasa.applinks.details;
  assert.deepEqual(detail.appIDs, ["JSF53DFS29.com.monaco.app"]);
  assert.equal(detail.appID, "JSF53DFS29.com.monaco.app");
  assert.deepEqual(detail.paths, ["/join/*"]);
  assert.equal(detail.components[0]["/"], "/join/*");
});

test("both hosts serve the association file as JSON and /join/<code> as the page", () => {
  assert.match(read("_headers"), /\/\.well-known\/apple-app-site-association\n\s+Content-Type: application\/json/);
  const vercel = JSON.parse(read("vercel.json"));
  assert.deepEqual(vercel.rewrites, [{ source: "/join/:code", destination: "/join/index.html" }]);
  const aasaRule = vercel.headers.find((rule) => rule.source === "/.well-known/apple-app-site-association");
  assert.ok(aasaRule.headers.some((h) => h.key === "Content-Type" && h.value === "application/json"));
});

test("the join page loads the shared logic and the API base", () => {
  const html = read("join/index.html");
  assert.match(html, /from "\/lib\/invite\.js"/);
  assert.match(html, /<script src="\/config\.js"><\/script>/);
  assert.match(read("config.js"), /window\.MONACO_API_BASE = window\.MONACO_API_BASE \|\| ""/);
});
