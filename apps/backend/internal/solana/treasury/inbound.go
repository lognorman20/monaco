package treasury

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	inboundSignaturePageSize = 100
	inboundMaxPages          = 5
)

// errTransactionNotReady means RPC listed a signature but has not served its transaction yet.
var errTransactionNotReady = errors.New("solana treasury: transaction not ready")

// irrelevantSignatures remembers treasury token account signatures that carried no USDC from
// a given sender, so each poll does not refetch the same unrelated transactions.
var irrelevantSignatures sync.Map // key: signature + "|" + from

type signatureInfo struct {
	Signature          string `json:"signature"`
	Err                any    `json:"err"`
	BlockTime          *int64 `json:"blockTime"`
	ConfirmationStatus string `json:"confirmationStatus"`
}

type parsedInstruction struct {
	Program string `json:"program"`
	Parsed  *struct {
		Type string `json:"type"`
		Info struct {
			Source            string `json:"source"`
			Destination       string `json:"destination"`
			Authority         string `json:"authority"`
			MultisigAuthority string `json:"multisigAuthority"`
			Mint              string `json:"mint"`
			Amount            string `json:"amount"`
			TokenAmount       *struct {
				Amount string `json:"amount"`
			} `json:"tokenAmount"`
		} `json:"info"`
	} `json:"parsed"`
}

type parsedTransaction struct {
	BlockTime *int64 `json:"blockTime"`
	Meta      *struct {
		Err               any `json:"err"`
		InnerInstructions []struct {
			Instructions []parsedInstruction `json:"instructions"`
		} `json:"innerInstructions"`
	} `json:"meta"`
	Transaction struct {
		Message struct {
			Instructions []parsedInstruction `json:"instructions"`
		} `json:"message"`
	} `json:"transaction"`
}

// ListInboundUSDCTransfers scans the treasury's USDC token account for confirmed transfers
// signed by (or drawn from the USDC account of) q.FromAddress. Transfers of any other mint
// never touch that token account, and transfers from any other owner are skipped.
func (c *HTTPClient) ListInboundUSDCTransfers(ctx context.Context, q InboundQuery) ([]InboundTransfer, error) {
	treasuryATA, err := USDCTokenAccount(strings.TrimSpace(q.TreasuryAddress))
	if err != nil {
		return nil, fmt.Errorf("%w: treasury usdc account: %v", ErrAPI, err)
	}
	from := strings.TrimSpace(q.FromAddress)
	fromATA, err := USDCTokenAccount(from)
	if err != nil {
		return nil, fmt.Errorf("%w: sender usdc account: %v", ErrAPI, err)
	}

	var out []InboundTransfer
	before := ""
	for page := 0; page < inboundMaxPages; page++ {
		opts := map[string]any{"limit": inboundSignaturePageSize, "commitment": "confirmed"}
		if before != "" {
			opts["before"] = before
		}
		var sigs []signatureInfo
		if err := c.callSolanaRPC(ctx, "getSignaturesForAddress", []any{treasuryATA, opts}, &sigs); err != nil {
			return nil, err
		}
		reachedSince := false
		for _, sig := range sigs {
			if sig.BlockTime != nil && !q.Since.IsZero() && time.Unix(*sig.BlockTime, 0).Before(q.Since) {
				reachedSince = true
				break
			}
			if sig.Err != nil {
				continue
			}
			switch sig.ConfirmationStatus {
			case "confirmed", "finalized":
			default:
				continue
			}
			cacheKey := sig.Signature + "|" + from
			if _, skip := irrelevantSignatures.Load(cacheKey); skip {
				continue
			}
			transfer, ok, err := c.inboundTransferFromSignature(ctx, sig.Signature, treasuryATA, from, fromATA)
			if errors.Is(err, errTransactionNotReady) {
				continue // not indexed yet; look again next poll
			}
			if err != nil {
				return nil, err
			}
			if !ok {
				irrelevantSignatures.Store(cacheKey, struct{}{})
				continue
			}
			out = append(out, transfer)
		}
		if reachedSince || len(sigs) < inboundSignaturePageSize {
			break
		}
		before = sigs[len(sigs)-1].Signature
	}
	return out, nil
}

func (c *HTTPClient) inboundTransferFromSignature(ctx context.Context, signature, treasuryATA, from, fromATA string) (InboundTransfer, bool, error) {
	var tx *parsedTransaction
	err := c.callSolanaRPC(ctx, "getTransaction", []any{
		signature,
		map[string]any{"encoding": "jsonParsed", "commitment": "confirmed", "maxSupportedTransactionVersion": 0},
	}, &tx)
	if err != nil {
		return InboundTransfer{}, false, err
	}
	if tx == nil || tx.Meta == nil {
		return InboundTransfer{}, false, errTransactionNotReady
	}
	amount, err := inboundUSDCAmount(tx, treasuryATA, from, fromATA)
	if err != nil || amount <= 0 {
		return InboundTransfer{}, false, err
	}
	var blockTime time.Time
	if tx.BlockTime != nil {
		blockTime = time.Unix(*tx.BlockTime, 0).UTC()
	}
	return InboundTransfer{TxSignature: signature, FromAddress: from, Amount: amount, BlockTime: blockTime}, true, nil
}

// inboundUSDCAmount sums SPL token transfers into treasuryATA from the sender in one
// successful transaction.
func inboundUSDCAmount(tx *parsedTransaction, treasuryATA, from, fromATA string) (int64, error) {
	if tx == nil || tx.Meta == nil || tx.Meta.Err != nil {
		return 0, nil
	}
	instructions := append([]parsedInstruction{}, tx.Transaction.Message.Instructions...)
	for _, inner := range tx.Meta.InnerInstructions {
		instructions = append(instructions, inner.Instructions...)
	}
	var total int64
	for _, ix := range instructions {
		if ix.Program != "spl-token" || ix.Parsed == nil {
			continue
		}
		info := ix.Parsed.Info
		raw := info.Amount
		switch ix.Parsed.Type {
		case "transfer":
		case "transferChecked":
			if info.Mint != USDCMint {
				continue
			}
			if info.TokenAmount != nil {
				raw = info.TokenAmount.Amount
			}
		default:
			continue
		}
		if info.Destination != treasuryATA {
			continue
		}
		if info.Authority != from && info.MultisigAuthority != from && info.Source != fromATA {
			continue
		}
		amount, err := parseTokenAmount(raw)
		if err != nil {
			return 0, err
		}
		total += amount
	}
	return total, nil
}
