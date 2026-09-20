import { createWalletClient, http, type Hex } from "viem"
import { privateKeyToAccount } from "viem/accounts"
import { base } from "viem/chains"

export type RelayerSend = { to: string; data?: string; valueWei?: string }

function normalizePrivateKey(raw: string): Hex {
  const trimmed = raw.trim()
  const hex = trimmed.startsWith("0x") ? trimmed : `0x${trimmed}`
  return hex as Hex
}

export function relayerAccount(privateKey: string) {
  return privateKeyToAccount(normalizePrivateKey(privateKey))
}

export function createRelayer(privateKey: string) {
  const rpcUrl = process.env.BASE_RPC_URL ?? "https://mainnet.base.org"
  const account = relayerAccount(privateKey)
  const wallet = createWalletClient({
    account,
    chain: base,
    transport: http(rpcUrl),
  })
  let queue: Promise<string> = Promise.resolve("")
  return {
    address: account.address.toLowerCase(),
    send(req: RelayerSend): Promise<string> {
      const next = queue.then(async () => {
        return wallet.sendTransaction({
          to: req.to as `0x${string}`,
          data: (req.data || "0x") as Hex,
          value: BigInt(req.valueWei ?? "0"),
        })
      })
      queue = next.catch(() => "")
      return next
    },
  }
}


export function createTestRelayer() {
  let queue: Promise<string> = Promise.resolve("")
  return {
    address: "0x0000000000000000000000000000000000000001",
    send(req: RelayerSend): Promise<string> {
      const next = queue.then(async () => {
        const n = BigInt("0x" + Buffer.from(req.to + (req.data ?? "")).toString("hex").slice(0, 16) || "1")
        return `0x${n.toString(16).padStart(64, "0")}`
      })
      queue = next.catch(() => "")
      return next
    },
  }
}
