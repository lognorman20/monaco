export type DynamicWallet = {
  walletMetadata: unknown
  externalServerKeyShares: unknown
  accountAddress: string
}

export type DynamicClient = {
  createWalletAccount: () => Promise<DynamicWallet>
  signTypedData: (args: { walletMetadata: unknown; typedData: unknown; password: string; externalServerKeyShares: unknown }) => Promise<string>
  sendTransaction: (args: { walletMetadata: unknown; to: string; data: string; value: bigint; externalServerKeyShares: unknown }) => Promise<string>
}

export function createDynamicClient(): DynamicClient {
  throw new Error("live Dynamic client is configured at process start")
}
