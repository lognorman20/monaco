package evm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TypedData is JSON-serialisable EIP-712 typed data for USDC transferWithAuthorization.
type TypedData struct {
	Types       map[string][]TypedField `json:"types"`
	PrimaryType string                  `json:"primaryType"`
	Domain      TypedDomain             `json:"domain"`
	Message     map[string]interface{}  `json:"message"`
}

type TypedField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TypedDomain struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	ChainID           int64  `json:"chainId"`
	VerifyingContract string `json:"verifyingContract"`
}

// AuthorizationNonce returns keccak256("monaco:" + intentID).
func AuthorizationNonce(intentID string) [32]byte {
	return crypto.Keccak256Hash([]byte("monaco:" + intentID))
}

// TransferAuthorizationTypedData builds EIP-712 typed data for USDC on Base.
func TransferAuthorizationTypedData(from, to string, value *big.Int, validBefore int64, nonce [32]byte) TypedData {
	from = common.HexToAddress(from).Hex()
	to = common.HexToAddress(to).Hex()
	return TypedData{
		Types: map[string][]TypedField{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferWithAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Domain: TypedDomain{
			Name:              "USD Coin",
			Version:           "2",
			ChainID:           ChainID,
			VerifyingContract: USDCAddress,
		},
		Message: map[string]interface{}{
			"from":        from,
			"to":          to,
			"value":       value.String(),
			"validAfter":  "0",
			"validBefore": big.NewInt(validBefore).String(),
			"nonce":       "0x" + common.Bytes2Hex(nonce[:]),
		},
	}
}
