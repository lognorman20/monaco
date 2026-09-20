package evm

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// NormalizeAddress lowercases and validates a 0x-prefixed 20-byte hex address.
func NormalizeAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if !common.IsHexAddress(addr) {
		return "", fmt.Errorf("invalid evm address %q", addr)
	}
	return strings.ToLower(common.HexToAddress(addr).Hex()), nil
}
