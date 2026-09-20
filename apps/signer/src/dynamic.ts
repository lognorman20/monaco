import { ThresholdSignatureScheme } from "@dynamic-labs-wallet/core"
import { DynamicEvmWalletClient } from "@dynamic-labs-wallet/node-evm"
import { base } from "viem/chains"

export type DynamicWallet = {
  walletMetadata: unknown
  externalServerKeyShares: unknown
  accountAddress: string
}

export type DynamicClient = {
  createWalletAccount: () => Promise<DynamicWallet>
  signTypedData: (args: {
    walletMetadata: unknown
    typedData: unknown
    password: string
    externalServerKeyShares: unknown
  }) => Promise<string>
  sendTransaction: (args: {
    walletMetadata: unknown
    to: string
    data: string
    value: bigint
    externalServerKeyShares: unknown
  }) => Promise<string>
}

export function createDynamicClient(): DynamicClient {
  const environmentId = process.env.DYNAMIC_ENVIRONMENT_ID ?? ""
  const apiToken = process.env.DYNAMIC_API_TOKEN ?? ""
  const password = process.env.DYNAMIC_WALLET_PASSWORD ?? ""
  const rpcUrl = process.env.BASE_RPC_URL ?? "https://mainnet.base.org"
  if (!environmentId || !apiToken) {
    return {
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
  }

  const client = new DynamicEvmWalletClient({ environmentId })
  let ready: Promise<void> | null = null
  const ensureAuth = () => {
    if (!ready) {
      ready = client.authenticateApiToken(apiToken).then(() => undefined)
    }
    return ready
  }

  return {
    async createWalletAccount() {
      await ensureAuth()
      const created = await client.createWalletAccount({
        thresholdSignatureScheme: ThresholdSignatureScheme.TWO_OF_TWO,
        password,
        backUpToDynamic: true,
      })
      const meta = created.walletMetadata as { accountAddress?: string }
      const accountAddress = String(meta?.accountAddress ?? "").toLowerCase()
      return {
        walletMetadata: created.walletMetadata,
        externalServerKeyShares: created.externalServerKeyShares,
        accountAddress,
      }
    },
    async signTypedData(args) {
      await ensureAuth()
      return client.signTypedData({
        walletMetadata: args.walletMetadata as never,
        typedData: args.typedData as never,
        password: args.password || password,
        externalServerKeyShares: args.externalServerKeyShares as never,
      })
    },
    async sendTransaction(args) {
      await ensureAuth()
      const walletClient = await client.getWalletClient({
        chainId: base.id,
        rpcUrl,
        walletMetadata: args.walletMetadata as never,
        externalServerKeyShares: args.externalServerKeyShares as never,
        password: password || undefined,
      })
      const hash = await walletClient.sendTransaction({
        to: args.to as `0x${string}`,
        data: (args.data || "0x") as `0x${string}`,
        value: args.value,
      })
      return hash
    },
  }
}
