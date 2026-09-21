import { createServer, request as httpRequest, type IncomingMessage } from "node:http"
import { describe, expect, it } from "vitest"
import { handle } from "../src/routes.js"
import { createTestRelayer } from "../src/relayer.js"
import type { DynamicClient } from "../src/dynamic.js"

function fakeDynamic(): DynamicClient {
  return {
    async createWalletAccount() {
      return {
        walletMetadata: { id: "w1" },
        externalServerKeyShares: { share: "s1" },
        accountAddress: "0xABC0000000000000000000000000000000000001",
      }
    },
    async signTypedData() {
      return "0x" + "ab".repeat(65)
    },
    async sendTransaction() {
      return "0x" + "cd".repeat(32)
    },
  }
}

function request(port: number, method: string, path: string, headers: Record<string, string>, body?: unknown): Promise<{ status: number; json: any }> {
  return new Promise((resolve, reject) => {
    const req = httpRequest(
      { hostname: "127.0.0.1", port, path, method, headers: { "content-type": "application/json", ...headers } },
      (res) => {
        const chunks: Buffer[] = []
        res.on("data", (c) => chunks.push(c as Buffer))
        res.on("end", () => {
          const text = Buffer.concat(chunks).toString("utf8")
          resolve({ status: res.statusCode ?? 0, json: text ? JSON.parse(text) : {} })
        })
      },
    )
    req.on("error", reject)
    if (body) req.write(JSON.stringify(body))
    req.end()
  })
}

describe("signer routes", () => {
  it("rejects requests without the shared secret", async () => {
    const server = createServer((req: IncomingMessage, res) => {
      void handle(req, res, {
        secret: "s3cret",
        dynamic: fakeDynamic(),
        relayer: createTestRelayer(),
        relayerAddress: "0xrelayer",
      })
    })
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()))
    const addr = server.address()
    if (!addr || typeof addr === "string") throw new Error("addr")
    const got = await request(addr.port, "GET", "/healthz", {})
    expect(got.status).toBe(401)
    server.close()
  })

  it("creates a wallet and returns lowercase address + metadata + shares", async () => {
    const server = createServer((req, res) => {
      void handle(req, res, {
        secret: "s3cret",
        dynamic: fakeDynamic(),
        relayer: createTestRelayer(),
        relayerAddress: "0xrelayer",
      })
    })
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()))
    const addr = server.address()
    if (!addr || typeof addr === "string") throw new Error("addr")
    const got = await request(addr.port, "POST", "/v1/wallets", { "x-signer-secret": "s3cret" }, {})
    expect(got.status).toBe(200)
    expect(got.json.address).toBe("0xabc0000000000000000000000000000000000001")
    expect(got.json.walletId).toBe("w1")
    expect(got.json.metadata).toEqual({ id: "w1" })
    expect(got.json.keyShares).toEqual({ share: "s1" })
    server.close()
  })

  it("signs typed data with stored shares", async () => {
    const server = createServer((req, res) => {
      void handle(req, res, {
        secret: "s3cret",
        dynamic: fakeDynamic(),
        relayer: createTestRelayer(),
        relayerAddress: "0xrelayer",
      })
    })
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()))
    const addr = server.address()
    if (!addr || typeof addr === "string") throw new Error("addr")
    const got = await request(addr.port, "POST", "/v1/wallets/sign-typed-data", { "x-signer-secret": "s3cret" }, { metadata: {}, keyShares: {}, typedData: {} })
    expect(got.status).toBe(200)
    expect(String(got.json.signature).startsWith("0x")).toBe(true)
    server.close()
  })

  it("returns 500 when wallet send throws instead of crashing", async () => {
    const server = createServer((req, res) => {
      void handle(req, res, {
        secret: "s3cret",
        dynamic: {
          ...fakeDynamic(),
          async sendTransaction() {
            throw new Error("gas required exceeds allowance (0)")
          },
        },
        relayer: createTestRelayer(),
        relayerAddress: "0xrelayer",
      })
    })
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()))
    const addr = server.address()
    if (!addr || typeof addr === "string") throw new Error("addr")
    const got = await request(addr.port, "POST", "/v1/wallets/send", { "x-signer-secret": "s3cret" }, {
      metadata: {},
      keyShares: {},
      to: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
      data: "0x",
      valueWei: "0",
    })
    expect(got.status).toBe(500)
    expect(String(got.json.error)).toContain("gas required exceeds allowance")
    server.close()
  })

  it("serialises relayer sends", async () => {
    const relayer = createTestRelayer()
    const hashes = await Promise.all([relayer.send({ to: "0xa" }), relayer.send({ to: "0xb" })])
    expect(hashes[0]).not.toEqual(hashes[1])
    expect(hashes[0].startsWith("0x")).toBe(true)
  })
})
