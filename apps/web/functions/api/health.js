// Cloudflare Pages Functions adapter. See functions/api/waitlist.js.
import { checkHealth } from "../../lib/waitlist.js";

export async function onRequest({ env }) {
  const { status, body } = await checkHealth({ env, fetch: globalThis.fetch });
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "Cache-Control": "no-store" },
  });
}
