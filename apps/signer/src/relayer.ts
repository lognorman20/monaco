type RelayerSend = { to: string; data?: string; valueWei?: string }

export function createRelayer(privateKey: string) {
  let queue: Promise<string> = Promise.resolve("")
  return {
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
