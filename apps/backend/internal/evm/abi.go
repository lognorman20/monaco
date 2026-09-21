package evm

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const erc20ABIJSON = `[{"constant":false,"inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"type":"function"},{"constant":false,"inputs":[{"name":"spender","type":"address"},{"name":"value","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"type":"function"},{"constant":false,"inputs":[{"name":"from","type":"address"},{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"validAfter","type":"uint256"},{"name":"validBefore","type":"uint256"},{"name":"nonce","type":"bytes32"},{"name":"v","type":"uint8"},{"name":"r","type":"bytes32"},{"name":"s","type":"bytes32"}],"name":"transferWithAuthorization","outputs":[],"type":"function"}]`

const multicall3ABIJSON = `[{"inputs":[{"components":[{"name":"target","type":"address"},{"name":"allowFailure","type":"bool"},{"name":"callData","type":"bytes"}],"name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"name":"success","type":"bool"},{"name":"returnData","type":"bytes"}],"name":"returnData","type":"tuple[]"}],"stateMutability":"payable","type":"function"}]`

var erc20ABI abi.ABI
var multicall3ABI abi.ABI

var latestRoundDataSelector = []byte{0xfe, 0xaf, 0x96, 0x8c}
var getRoundDataSelector []byte

func init() {
	parsed, err := abi.JSON(strings.NewReader(erc20ABIJSON))
	if err != nil {
		panic(err)
	}
	erc20ABI = parsed
	mc, err := abi.JSON(strings.NewReader(multicall3ABIJSON))
	if err != nil {
		panic(err)
	}
	multicall3ABI = mc
	getRoundDataSelector = crypto.Keccak256([]byte("getRoundData(uint80)"))[:4]
}

type multicall3Call struct {
	Target       common.Address
	AllowFailure bool
	CallData     []byte
}

type multicall3Result struct {
	Success    bool
	ReturnData []byte
}

func encodeAggregate3(feeds []string) ([]byte, error) {
	datas := make([][]byte, len(feeds))
	for i := range feeds {
		datas[i] = latestRoundDataSelector
	}
	return encodeAggregate3Calls(feeds, datas)
}

func encodeGetRoundData(roundID *big.Int) []byte {
	word := make([]byte, 32)
	if roundID != nil {
		roundID.FillBytes(word)
	}
	return append(append([]byte{}, getRoundDataSelector...), word...)
}

func encodeAggregate3Calls(targets []string, datas [][]byte) ([]byte, error) {
	calls := make([]multicall3Call, 0, len(targets))
	for i, target := range targets {
		data := datas[i]
		calls = append(calls, multicall3Call{
			Target:       common.HexToAddress(target),
			AllowFailure: true,
			CallData:     data,
		})
	}
	return multicall3ABI.Pack("aggregate3", calls)
}

func decodeAggregate3(raw []byte) ([]multicall3Result, error) {
	values, err := multicall3ABI.Unpack("aggregate3", raw)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("aggregate3: unexpected outputs")
	}
	converted, ok := abi.ConvertType(values[0], new([]multicall3Result)).(*[]multicall3Result)
	if !ok || converted == nil {
		return nil, fmt.Errorf("aggregate3: decode")
	}
	return *converted, nil
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

// ERC20TransferLog builds a Transfer log for tests and receipt decoding.
func ERC20TransferLog(token, from, to string, amount *big.Int) Log {
	if amount == nil {
		amount = big.NewInt(0)
	}
	fromPad := common.BytesToHash(common.HexToAddress(from).Bytes()).Hex()
	toPad := common.BytesToHash(common.HexToAddress(to).Bytes()).Hex()
	data := common.LeftPadBytes(amount.Bytes(), 32)
	return Log{
		Address: token,
		Topics:  []string{transferEventSig, fromPad, toPad},
		Data:    data,
	}
}

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
