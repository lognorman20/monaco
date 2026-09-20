import { createServer } from "node:http"
import { createDynamicClient } from "./dynamic.js"
import { createRelayer } from "./relayer.js"
import { handle } from "./routes.js"

const secret = process.env.SIGNER_SHARED_SECRET ?? ""
const port = Number(process.env.SIGNER_PORT ?? "8081")
const relayerKey = process.env.RELAYER_PRIVATE_KEY ?? ""
if (!secret) {
  console.error("SIGNER_SHARED_SECRET is required")
  process.exit(1)
}
if (!relayerKey) {
  console.error("RELAYER_PRIVATE_KEY is required")
  process.exit(1)
}

const relayer = createRelayer(relayerKey)

const server = createServer((req, res) => {
  void handle(req, res, {
    secret,
    dynamic: createDynamicClient(),
    relayer,
    relayerAddress: relayer.address,
  })
})

server.listen(port, "127.0.0.1", () => {
  console.log(`signer listening on 127.0.0.1:${port} relayer=${relayer.address}`)
})
