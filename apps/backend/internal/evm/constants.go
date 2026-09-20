package evm

import "math/big"

const ChainID = 8453

const USDCAddress = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"

const USDCDecimals = 6

var (
	TreasuryGasFloorWei = big.NewInt(300_000_000_000_000)   // 0.0003 ETH
	TreasuryGasTopUpWei = big.NewInt(1_000_000_000_000_000) // 0.001 ETH
	FeePayerMinWei      = big.NewInt(2_000_000_000_000_000) // 0.002 ETH
)
