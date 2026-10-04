package solana

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
)

const lamportDecimals = 9

type State uint8

const (
	StateNotFound State = iota + 1
	StateProcessing
	StateFinalized
)

type Status struct {
	Signature   chain.Signature
	State       State
	Failed      bool
	BlockHeight uint64
}

func (s Status) BlockhashExpired(lastValidBlockHeight uint64) bool {
	return s.State == StateNotFound && s.BlockHeight > lastValidBlockHeight
}

type SignatureInfo struct {
	Signature chain.Signature
	Slot      uint64
	Failed    bool
	BlockTime time.Time
}

type SignaturesOpts struct {
	Before, Until  chain.Signature
	Limit          int
	MinContextSlot uint64
}

type TokenAccountState struct {
	Address chain.SolanaAddress
	Exists  bool
	Program chain.SolanaAddress
	Mint    chain.SolanaAddress
	Owner   chain.SolanaAddress
	State   string
	Amount  money.BaseUnits
}

func commitment(level string) map[string]string { return map[string]string{"commitment": level} }

func (c *Client) SOLBalance(ctx context.Context, addr chain.SolanaAddress) (money.BaseUnits, error) {
	if err := addresses("solana.SOLBalance", addr); err != nil {
		return money.BaseUnits{}, err
	}
	var w struct {
		Value uint64 `json:"value"`
	}
	err := c.call(ctx, "getBalance", []any{addr, commitment("confirmed")}, &w)
	return money.NewBaseUnits(w.Value, lamportDecimals), err
}

type tokenAccountsWire struct {
	Value []struct {
		Account struct {
			Data struct {
				Parsed struct {
					Info struct {
						TokenAmount struct {
							Amount   string `json:"amount"`
							Decimals uint8  `json:"decimals"`
						} `json:"tokenAmount"`
					} `json:"info"`
				} `json:"parsed"`
			} `json:"data"`
		} `json:"account"`
	} `json:"value"`
}

type tokenAccountWire struct {
	Pubkey  chain.SolanaAddress   `json:"pubkey"`
	Account *tokenAccountInfoWire `json:"account"`
}

type tokenAccountInfoWire struct {
	Owner chain.SolanaAddress `json:"owner"`
	Data  struct {
		Parsed struct {
			Info struct {
				Mint        chain.SolanaAddress `json:"mint"`
				Owner       chain.SolanaAddress `json:"owner"`
				State       string              `json:"state"`
				TokenAmount struct {
					Amount   string `json:"amount"`
					Decimals uint8  `json:"decimals"`
				} `json:"tokenAmount"`
			} `json:"info"`
		} `json:"parsed"`
	} `json:"data"`
}

func tokenAccount(addr chain.SolanaAddress, in *tokenAccountInfoWire) (TokenAccountState, error) {
	out := TokenAccountState{Address: addr}
	if in == nil {
		return out, nil
	}
	amount, err := strconv.ParseUint(in.Data.Parsed.Info.TokenAmount.Amount, 10, 64)
	if err != nil {
		return out, errs.Wrap(err, errs.CodeDecodeFailed, "solana.TokenAccount")
	}
	info := in.Data.Parsed.Info
	out.Exists, out.Program, out.Mint, out.Owner, out.State = true, in.Owner, info.Mint, info.Owner, info.State
	out.Amount = money.NewBaseUnits(amount, info.TokenAmount.Decimals)
	return out, nil
}

func (c *Client) Accounts(
	ctx context.Context,
	addrs []chain.SolanaAddress,
	minContextSlot uint64,
) (uint64, []TokenAccountState, error) {
	if len(addrs) > 100 {
		return 0, nil, errs.New(errs.CodeInvalidInput, "solana.Accounts", slog.Int("accounts", len(addrs)))
	}
	if err := addresses("solana.Accounts", addrs...); err != nil {
		return 0, nil, err
	}
	var wire struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value []*tokenAccountInfoWire `json:"value"`
	}
	opts := map[string]any{"encoding": "jsonParsed", "commitment": "finalized"}
	if minContextSlot != 0 {
		opts["minContextSlot"] = minContextSlot
	}
	if err := c.call(ctx, "getMultipleAccounts", []any{addrs, opts}, &wire); err != nil {
		return 0, nil, err
	}
	if len(wire.Value) != len(addrs) {
		return 0, nil, errs.New(errs.CodeDecodeFailed, "solana.Accounts")
	}
	out := make([]TokenAccountState, len(addrs))
	for i := range addrs {
		var err error
		out[i], err = tokenAccount(addrs[i], wire.Value[i])
		if err != nil {
			return 0, nil, err
		}
	}
	return wire.Context.Slot, out, nil
}

func (c *Client) TokenAccounts(
	ctx context.Context,
	owner chain.SolanaAddress,
	mint chain.Mint,
) (uint64, []TokenAccountState, error) {
	if err := addresses("solana.TokenAccounts", owner, mint.Address); err != nil {
		return 0, nil, err
	}
	var wire struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value []tokenAccountWire `json:"value"`
	}
	params := []any{
		owner,
		map[string]any{"mint": mint.Address},
		map[string]any{"encoding": "jsonParsed", "commitment": "finalized"},
	}
	if err := c.call(ctx, "getTokenAccountsByOwner", params, &wire); err != nil {
		return 0, nil, err
	}
	out := make([]TokenAccountState, len(wire.Value))
	for i := range wire.Value {
		var err error
		out[i], err = tokenAccount(wire.Value[i].Pubkey, wire.Value[i].Account)
		if err != nil {
			return 0, nil, err
		}
	}
	return wire.Context.Slot, out, nil
}

func (c *Client) TokenBalance(
	ctx context.Context,
	owner chain.SolanaAddress,
	mint chain.Mint,
) (money.BaseUnits, error) {
	const op = "solana.TokenBalance"
	total := money.NewBaseUnits(0, mint.Decimals)
	if err := addresses(op, owner, mint.Address); err != nil {
		return total, err
	}
	var w tokenAccountsWire
	opts := map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"}
	if err := c.call(
		ctx,
		"getTokenAccountsByOwner",
		[]any{owner, map[string]any{"mint": mint.Address}, opts},
		&w,
	); err != nil {
		return total, err
	}
	for _, acct := range w.Value {
		amt := acct.Account.Data.Parsed.Info.TokenAmount
		v, err := strconv.ParseUint(amt.Amount, 10, 64)
		if err != nil {
			return total, errs.Wrap(err, errs.CodeDecodeFailed, op, slog.String("amount", amt.Amount))
		}
		if total, err = total.Add(money.NewBaseUnits(v, amt.Decimals)); err != nil {
			return money.NewBaseUnits(0, mint.Decimals), errs.Wrap(err, errs.CodeDecodeFailed, op)
		}
	}
	return total, nil
}

type statusWire struct {
	Err                any    `json:"err"`
	ConfirmationStatus string `json:"confirmationStatus"`
}

func (c *Client) SignatureStatuses(ctx context.Context, sigs []chain.Signature) ([]Status, error) {
	const op = "solana.SignatureStatuses"
	if len(sigs) == 0 {
		return nil, nil
	}
	if len(sigs) > maxStatuses {
		return nil, errs.New(errs.CodeInvalidInput, op, slog.Int("signatures", len(sigs)))
	}
	var height uint64
	if err := c.call(ctx, "getBlockHeight", []any{commitment("finalized")}, &height); err != nil {
		return nil, err
	}
	var w struct {
		Value []*statusWire `json:"value"`
	}
	opts := map[string]bool{"searchTransactionHistory": true}
	if err := c.call(ctx, "getSignatureStatuses", []any{sigs, opts}, &w); err != nil {
		return nil, err
	}
	if len(w.Value) != len(sigs) {
		return nil, errs.New(
			errs.CodeDecodeFailed,
			op,
			slog.Int("statuses", len(w.Value)),
			slog.Int("signatures", len(sigs)),
		)
	}
	out := make([]Status, len(sigs))
	for i, s := range w.Value {
		out[i] = Status{Signature: sigs[i], State: StateNotFound, BlockHeight: height}
		if s == nil {
			continue
		}
		out[i].State, out[i].Failed = StateProcessing, s.Err != nil
		if s.ConfirmationStatus == "finalized" {
			out[i].State = StateFinalized
		}
	}
	return out, nil
}

func (c *Client) BlockhashValid(ctx context.Context, hash string) (bool, error) {
	const op = "solana.BlockhashValid"
	if raw, ok := chain.DecodeBase58(hash); !ok || len(raw) != 32 {
		return false, errs.New(errs.CodeInvalidAddress, op, slog.String("blockhash", hash))
	}
	var wire struct {
		Value bool `json:"value"`
	}
	if err := c.call(ctx, "isBlockhashValid", []any{hash, commitment("confirmed")}, &wire); err != nil {
		return false, err
	}
	return wire.Value, nil
}

func (c *Client) SignaturesFor(
	ctx context.Context,
	addr chain.SolanaAddress,
	in SignaturesOpts,
) ([]SignatureInfo, error) {
	if err := addresses("solana.SignaturesFor", addr); err != nil {
		return nil, err
	}
	if in.Limit < 1 || in.Limit > 1000 {
		return nil, errs.New(errs.CodeInvalidInput, "solana.SignaturesFor", slog.Int("limit", in.Limit))
	}
	opts := map[string]any{"limit": in.Limit, "commitment": "finalized"}
	if in.Before != "" {
		opts["before"] = in.Before
	}
	if in.Until != "" {
		opts["until"] = in.Until
	}
	if in.MinContextSlot != 0 {
		opts["minContextSlot"] = in.MinContextSlot
	}
	var w []struct {
		Signature chain.Signature `json:"signature"`
		Slot      uint64          `json:"slot"`
		Err       any             `json:"err"`
		BlockTime *int64          `json:"blockTime"`
	}
	if err := c.call(ctx, "getSignaturesForAddress", []any{addr, opts}, &w); err != nil {
		return nil, err
	}
	out := make([]SignatureInfo, len(w))
	for i, s := range w {
		out[i] = SignatureInfo{Signature: s.Signature, Slot: s.Slot, Failed: s.Err != nil}
		if s.BlockTime != nil {
			out[i].BlockTime = time.Unix(*s.BlockTime, 0).UTC()
		}
	}
	return out, nil
}
