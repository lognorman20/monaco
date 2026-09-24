import { handleSignup } from "../lib/waitlist.js";

export default async function handler(req, res) {
  const { status, body } = await handleSignup({
    method: req.method,
    headers: req.headers,
    body: req.body,
    env: process.env,
    fetch: globalThis.fetch,
  });
  res.setHeader("Cache-Control", "no-store");
  res.status(status).json(body);
}
