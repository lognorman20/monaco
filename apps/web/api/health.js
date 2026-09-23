import { checkHealth } from "../lib/waitlist.js";

export default async function handler(req, res) {
  const { status, body } = await checkHealth({ env: process.env, fetch: globalThis.fetch });
  res.setHeader("Cache-Control", "no-store");
  res.status(status).json(body);
}
