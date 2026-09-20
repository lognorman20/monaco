package evm

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const erc20ABIJSON = `[{"constant":false,"inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"type":"function"},{"constant":false,"inputs":[{"name":"spender","type":"address"},{"name":"value","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"type":"function"},{"constant":false,"inputs":[{"name":"from","type":"address"},{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"validAfter","type":"uint256"},{"name":"validBefore","type":"uint256"},{"name":"nonce","type":"bytes32"},{"name":"v","type":"uint8"},{"name":"r","type":"bytes32"},{"name":"s","type":"bytes32"}],"name":"transferWithAuthorization","outputs":[],"type":"function"}]`

var erc20ABI abi.ABI

func init() {
	parsed, err := abi.JSON(strings.NewReader(erc20ABIJSON))
	if err != nil {
		panic(err)
	}
	erc20ABI = parsed
}

// EncodeTransfer ABI-encodes ERC-20 transfer(to, amount).
func EncodeTransfer(to string, amount *big.Int) ([]byte, error) {
	return erc20ABI.Pack("transfer", common.HexToAddress(to), amount)
}

// EncodeApprove ABI-encodes ERC-20 approve(spender, amount).
func EncodeApprove(spender string, amount *big.Int) ([]byte, error) {
	return erc20ABI.Pack("approve", common.HexToAddress(spender), amount)
}

// EncodeTransferWithAuthorization ABI-encodes USDC transferWithAuthorization.
func EncodeTransferWithAuthorization(from, to string, value, validAfter, validBefore *big.Int, nonce [32]byte, v uint8, r, s [32]byte) ([]byte, error) {
	return erc20ABI.Pack(
		"transferWithAuthorization",
		common.HexToAddress(from),
		common.HexToAddress(to),
		value,
		validAfter,
		validBefore,
		nonce,
		v,
		r,
		s,
	)
}

var transferEventSig = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()

// DecodeERC20TransferLogs sums Transfer events to `to` for `token`.
func DecodeERC20TransferLogs(logs []Log, token, to string) *big.Int {
	tokenAddr := common.HexToAddress(token)
	toAddr := common.HexToAddress(to)
	sum := big.NewInt(0)
	for _, lg := range logs {
		if common.HexToAddress(lg.Address) != tokenAddr {
			continue
		}
		if len(lg.Topics) < 3 || lg.Topics[0] != transferEventSig {
			continue
		}
		recipient := common.HexToAddress(lg.Topics[2])
		if recipient != toAddr {
			continue
		}
		if len(lg.Data) >= 32 {
			amount := new(big.Int).SetBytes(lg.Data[len(lg.Data)-32:])
			sum.Add(sum, amount)
		}
	}
	return sum
}
