package chain

import (
	"context"
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/app"
	platform "github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
)

type Solana interface {
	SignatureStatuses(context.Context, []platform.Signature) ([]solana.Status, error)
	InboundTransfersForMint(
		context.Context,
		platform.Signature,
		platform.SolanaAddress,
		platform.SolanaAddress,
	) ([]solana.Transfer, error)
	BlockhashValid(context.Context, string) (bool, error)
}

type Reader struct {
	solana Solana
}

var (
	_ app.ChainReader = Reader{}
	_ Solana          = (*solana.Client)(nil)
)

func NewReader(s Solana) Reader { return Reader{solana: s} }

func (r Reader) BlockhashValid(ctx context.Context, signed []byte) (bool, error) {
	hash, err := platform.RecentBlockhash(signed)
	if err != nil {
		return false, err
	}
	return r.solana.BlockhashValid(ctx, hash)
}

func (r Reader) SignatureStatuses(ctx context.Context, sigs []platform.Signature) ([]app.SigStatus, error) {
	statuses, err := r.solana.SignatureStatuses(ctx, sigs)
	if err != nil {
		return nil, err
	}
	out := make([]app.SigStatus, len(statuses))
	for i, status := range statuses {
		state, ok := map[solana.State]app.SigState{
			solana.StateFinalized:  app.SigFinalized,
			solana.StateProcessing: app.SigProcessing,
			solana.StateNotFound:   app.SigNotFound,
		}[status.State]
		if !ok {
			return nil, errs.New(errs.CodeDecodeFailed, "trading.ChainReader.SignatureStatuses",
				slog.Int("state", int(status.State)))
		}
		out[i] = app.SigStatus{State: state, Failed: status.Failed, BlockHeight: status.BlockHeight}
	}
	return out, nil
}

func (r Reader) InboundAmount(
	ctx context.Context,
	signature platform.Signature,
	owner platform.SolanaAddress,
	mint platform.Mint,
) (money.BaseUnits, error) {
	transfers, err := r.solana.InboundTransfersForMint(ctx, signature, owner, mint.Address)
	if err != nil {
		return money.BaseUnits{}, err
	}
	total := money.NewBaseUnits(0, mint.Decimals)
	for _, transfer := range transfers {
		total, err = total.Add(transfer.Net)
		if err != nil {
			return money.BaseUnits{}, errs.Wrap(err, errs.CodeDecodeFailed, "trading.ChainReader.InboundAmount",
				slog.String("signature", string(signature)), slog.String("mint", string(mint.Address)))
		}
	}
	return total, nil
}
