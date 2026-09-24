// Cloudflare Pages Functions adapter. The logic lives in lib/waitlist.js; this file
// only turns a Workers Request into the shape handleSignup() expects and back.
import { handleSignup } from "../../lib/waitlist.js";

export async function onRequest({ request, env }) {
  const { status, body } = await handleSignup({
    method: request.method,
    // handleSignup reads headers.origin, headers["user-agent"] and the IP headers by
    // lowercase key. Iterating a Headers object lowercases every name, so a plain
    // object built from it behaves like the plain object Vercel hands us.
    headers: Object.fromEntries(request.headers),
    // Workers does not parse the body. handleSignup takes a string and parses it itself.
    body: await request.text(),
    env,
    fetch: globalThis.fetch,
  });
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "Cache-Control": "no-store" },
  });
}
