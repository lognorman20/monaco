// Cloudflare Pages Function for /join/<code>. Every invite link is the same static page
// (join/index.html), which reads the code from location.pathname and fetches the cabal's
// preview in the browser, so this only hands back that page for any code. The Vercel
// equivalent is the /join/:code rewrite in vercel.json.
//
// A function rather than a `_redirects` 200 rule: it is testable under node (see
// test/invite.test.js), and it sets the page's headers itself, because `_headers` rules do
// not apply to a function's response.
export const JOIN_PAGE_HEADERS = {
  "content-type": "text/html; charset=utf-8",
  // Short: the page is the same for every code and changes only on deploy.
  "cache-control": "public, max-age=300",
  "x-content-type-options": "nosniff",
  "referrer-policy": "strict-origin-when-cross-origin",
  "x-frame-options": "DENY",
  "permissions-policy": "camera=(), microphone=(), geolocation=()",
  "x-robots-tag": "noindex",
};

export async function onRequest({ request, env }) {
  if (request.method !== "GET" && request.method !== "HEAD") {
    return new Response("Method not allowed", { status: 405, headers: { allow: "GET, HEAD" } });
  }
  const page = await env.ASSETS.fetch(new URL("/join/", request.url));
  if (!page.ok) return page;
  const headers = new Headers(page.headers);
  for (const [name, value] of Object.entries(JOIN_PAGE_HEADERS)) headers.set(name, value);
  return new Response(request.method === "HEAD" ? null : page.body, { status: 200, headers });
}
