import type { IncomingMessage, ServerResponse } from "node:http"
import type { DynamicClient } from "./dynamic.js"
import { createRelayer } from "./relayer.js"

type Deps = {
  secret: string
  dynamic: DynamicClient
  relayer: ReturnType<typeof createRelayer>
  relayerAddress: string
}

async function readBody(req: IncomingMessage): Promise<any> {
  const chunks: Buffer[] = []
  for await (const c of req) chunks.push(c as Buffer)
  if (chunks.length === 0) return {}
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}")
}

function send(res: ServerResponse, status: number, body: unknown) {
  res.statusCode = status
  res.setHeader("content-type", "application/json")
  res.end(JSON.stringify(body))
}

export async function handle(req: IncomingMessage, res: ServerResponse, deps: Deps) {
  try {
    await handleRequest(req, res, deps)
  } catch (err) {
    const message = err instanceof Error ? err.message : "signer error"
    console.error("signer request failed", err)
    if (!res.headersSent) {
      send(res, 500, { error: message })
    }
  }
}

async function handleRequest(req: IncomingMessage, res: ServerResponse, deps: Deps) {
  const secret = req.headers["x-signer-secret"]
  if (secret !== deps.secret) {
    send(res, 401, { error: "unauthorized" })
    return
  }
  const url = req.url ?? "/"
  if (req.method === "GET" && url === "/healthz") {
    send(res, 200, { relayerAddress: deps.relayerAddress, chainId: 8453 })
    return
  }
  if (req.method === "POST" && url === "/v1/wallets") {
	const w = await deps.dynamic.createWalletAccount()
    const address = String(w.accountAddress).toLowerCase()
    const meta = w.walletMetadata as { id?: string } | undefined
    const walletId = typeof meta?.id === "string" && meta.id !== "" ? meta.id : address
    send(res, 200, {
      walletId,
      address,
      metadata: w.walletMetadata,
      keyShares: w.externalServerKeyShares,
    })
    return
  }
  const body = await readBody(req)
  if (req.method === "POST" && url === "/v1/wallets/sign-typed-data") {
    const sig = await deps.dynamic.signTypedData({
      walletMetadata: body.metadata,
      typedData: body.typedData,
      password: process.env.DYNAMIC_WALLET_PASSWORD ?? "",
      externalServerKeyShares: body.keyShares,
    })
    send(res, 200, { signature: sig })
    return
  }
  if (req.method === "POST" && url === "/v1/wallets/send") {
    const hash = await deps.dynamic.sendTransaction({
      walletMetadata: body.metadata,
      to: body.to,
      data: body.data ?? "0x",
      value: BigInt(body.valueWei ?? "0"),
      externalServerKeyShares: body.keyShares,
    })
    send(res, 200, { txHash: hash })
    return
  }
  if (req.method === "POST" && url === "/v1/relayer/send") {
    const hash = await deps.relayer.send({ to: body.to, data: body.data, valueWei: body.valueWei })
    send(res, 200, { txHash: hash })
    return
  }
  send(res, 404, { error: "not found" })
}
