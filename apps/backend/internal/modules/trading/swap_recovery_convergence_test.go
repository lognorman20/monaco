//go:build faultpoints

package trading_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monaco/monaco/apps/backend/internal/modules/trading/app"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/domain"
	platform "github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/platform/faultpoint"
	"github.com/monaco/monaco/apps/backend/internal/platform/money"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
)

type recoveryReader struct {
	statuses []app.SigStatus
	inbound  money.BaseUnits
	valid    bool
}

func (r recoveryReader) SignatureStatuses(context.Context, []platform.Signature) ([]app.SigStatus, error) {
	return r.statuses, nil
}

func (r recoveryReader) BlockhashValid(context.Context, []byte) (bool, error) { return r.valid, nil }

func (r recoveryReader) InboundAmount(
	context.Context,
	platform.Signature,
	platform.SolanaAddress,
	platform.Mint,
) (money.BaseUnits, error) {
	return r.inbound, nil
}

func (e *layerEnv) recoverSwap(t *testing.T, r recoveryReader) uuid.UUID {
	t.Helper()
	e.clk.Advance(app.SwapSweepAge + time.Second)
	p := app.NewSwapSweeper(e.uow, e.pool, e.clk, r, e.hints)
	if _, err := p.Tick(actorContext(t.Context())); err != nil {
		t.Fatal(err)
	}
	rows, err := e.pool.Query(t.Context(), `SELECT id FROM swaps ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("recovery found no swap")
	}
	var id uuid.UUID
	if err := rows.Scan(&id); err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("recovery found more than one swap")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return id
}

func crashLayer(t *testing.T, e *layerEnv, point faultpoint.Name, req app.SwapRequest) {
	t.Helper()
	testkit.CrashAt(t, point, func(ctx context.Context) error {
		_, err := e.layer().Run(actorContext(ctx), req, nil)
		return err
	})
}

func assertRecovered(t *testing.T, e *layerEnv, id uuid.UUID, status domain.Status, failure domain.FailureCode) {
	t.Helper()
	got, _, _, _ := e.row(t, id)
	if got != string(status) || e.terminalEvents(t, id) != 1 {
		t.Fatalf("swap = %s with %d terminal events, want %s with one", got, e.terminalEvents(t, id), status)
	}
	if status != domain.StatusFailed {
		return
	}
	payload := e.payload(t, id, "trade.failed")
	if payload["failure_code"] != string(failure) {
		t.Fatalf("failure_code = %v, want %s", payload["failure_code"], failure)
	}
}

func TestSwapRecovery_CrashAfterCreateFailsAndFreesTheSource(t *testing.T) {
	t.Parallel()
	e := newLayerEnv(t)
	req := e.request(e.source())
	crashLayer(t, e, faultpoint.AfterCreate, req)
	id := e.recoverSwap(t, recoveryReader{})
	assertRecovered(t, e, id, domain.StatusFailed, domain.FailureNeverSubmitted)

	e.jup.SetExecute("req-1", jupiter.ExecuteResult{Status: jupiter.StatusPending})
	retry, err := e.run(t, req)
	if err != nil || retry.ID.UUID() == id || retry.Status != domain.StatusSubmitted {
		t.Fatalf("retry = %+v, %v", retry, err)
	}
}

func TestSwapRecovery_CrashAfterSignExpiresTheSignedTransaction(t *testing.T) {
	t.Parallel()
	e := newLayerEnv(t)
	crashLayer(t, e, faultpoint.AfterSign, e.request(e.source()))
	id := e.recoverSwap(t, recoveryReader{statuses: []app.SigStatus{{State: app.SigNotFound}}})
	assertRecovered(t, e, id, domain.StatusFailed, domain.FailureBlockhashExpired)
}

func TestSwapRecovery_CrashAfterExecuteConfirmsTheOnChainFill(t *testing.T) {
	t.Parallel()
	e := newLayerEnv(t)
	e.jup.SetExecute("req-1", jupiter.ExecuteResult{Status: jupiter.StatusSuccess, OutAmount: 105_000_000})
	crashLayer(t, e, faultpoint.AfterExecute, e.request(e.source()))
	id := e.recoverSwap(t, recoveryReader{
		statuses: []app.SigStatus{{State: app.SigFinalized}}, inbound: money.NewBaseUnits(105_000_000, 8),
	})
	assertRecovered(t, e, id, domain.StatusConfirmed, "")
	if payload := e.payload(t, id, "trade.confirmed"); payload["out_amount"] != "105000000" {
		t.Fatalf("out_amount = %v", payload["out_amount"])
	}
}

func TestSwapRecovery_CrashBeforeCommitRestartsTheTerminalSweep(t *testing.T) {
	t.Parallel()
	e := newLayerEnv(t)
	e.jup.SetExecute("req-1", jupiter.ExecuteResult{Status: jupiter.StatusSuccess, OutAmount: 105_000_000})
	req := e.request(e.source())
	crashed := func() (got any) {
		defer func() { got = recover() }()
		_, _ = e.layer().Run(
			faultpoint.ArmedAfter(actorContext(t.Context()), faultpoint.BeforeCommit, 2),
			req,
			nil,
		)
		return nil
	}()
	if crashed != (faultpoint.Crash{Name: faultpoint.BeforeCommit}) {
		t.Fatalf("crash = %v", crashed)
	}
	if _, err := e.run(t, req); err != nil {
		t.Fatal(err)
	}
	r := recoveryReader{
		statuses: []app.SigStatus{{State: app.SigFinalized}}, inbound: money.NewBaseUnits(105_000_000, 8),
	}
	id := e.recoverSwap(t, r)
	assertRecovered(t, e, id, domain.StatusConfirmed, "")
}
