import { createServer } from "node:http"
import { createRelayer } from "./relayer.js"
import { handle } from "./routes.js"
import type { DynamicClient } from "./dynamic.js"

const secret = process.env.SIGNER_SHARED_SECRET ?? ""
const port = Number(process.env.SIGNER_PORT ?? "8081")
const relayerKey = process.env.RELAYER_PRIVATE_KEY ?? "0x00"
const relayerAddress = "0x0000000000000000000000000000000000000000"

const dynamic: DynamicClient = {
  async createWalletAccount() {
    throw new Error("Dynamic not configured")
  },
  async signTypedData() {
    throw new Error("Dynamic not configured")
  },
  async sendTransaction() {
    throw new Error("Dynamic not configured")
  },
}

const server = createServer((req, res) => {
  void handle(req, res, {
    secret,
    dynamic,
    relayer: createRelayer(relayerKey),
    relayerAddress,
  })
})

server.listen(port, "127.0.0.1", () => {
  console.log(`signer listening on 127.0.0.1:${port}`)
})
