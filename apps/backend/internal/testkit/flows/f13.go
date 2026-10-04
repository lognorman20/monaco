package flows

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/events"
	"github.com/monaco/monaco/apps/backend/internal/modules/governance"
	tradingsqlc "github.com/monaco/monaco/apps/backend/internal/modules/trading/sqlc"
	"github.com/monaco/monaco/apps/backend/internal/platform/clock"
	"github.com/monaco/monaco/apps/backend/internal/platform/db"
	"github.com/monaco/monaco/apps/backend/internal/platform/faultpoint"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/module"
	"github.com/monaco/monaco/apps/backend/internal/testkit/scenario"
)

func f13Scripts() map[string]Script {
	return map[string]Script{
		"F13WithdrawProposalOK":                 F13WithdrawProposalOK,
		"F13WithdrawProposalUnauthorized":       F13WithdrawProposalUnauthorized,
		"F13WithdrawProposalProposalNotFound":   F13WithdrawProposalProposalNotFound,
		"F13WithdrawProposalNotProposer":        F13WithdrawProposalNotProposer,
		"F13WithdrawProposalProposalClosed":     F13WithdrawProposalProposalClosed,
		"F13WithdrawProposalWithdrawNotAllowed": F13WithdrawProposalWithdrawNotAllowed,
		"F13WithdrawProposalCrashAfterPublish":  F13WithdrawProposalCrashAfterPublish,
		"F13aVoidProposalOK":                    F13aVoidProposalOK,
		"F13aVoidProposalLiveSwapExists":        F13aVoidProposalLiveSwapExists,
	}
}

func F13WithdrawProposalOK(s *scenario.Scenario) {
	p := seedOpenProposal(s, 3)
	s.Given(scenario.AsSeededUser("alice", p.voters[0])).
		When(
			scenario.Post(p.votes, yes),
			scenario.ExpectStatus(http.StatusOK),
			scenario.Delete(p.path),
			scenario.ExpectStatus(http.StatusOK),
			scenario.ExpectJSON("status", "withdrawn"),
			scenario.ExpectJSON("my_ballot", "yes"),
			scenario.ExpectJSON("can_withdraw", false),
			scenario.Replay(),
		).
		Then(
			scenario.ExpectEvents(events.TypeProposalWithdrawn, 1),
			scenario.EventuallyPublished(events.TypeProposalWithdrawn, 1),
		)
}

func F13WithdrawProposalUnauthorized(s *scenario.Scenario) {
	p := seedOpenProposal(s, 1)
	s.Given(scenario.Anonymous()).
		When(scenario.Delete(p.path)).
		Then(scenario.ExpectProblem(errs.CodeUnauthorized), scenario.ExpectEvents(events.TypeProposalWithdrawn, 0))
}

func F13WithdrawProposalProposalNotFound(s *scenario.Scenario) {
	s.Given(scenario.AsUser("alice")).
		When(scenario.Delete("/v1/proposals/" + ids.Real{}.NewV7().String())).
		Then(scenario.ExpectProblem(errs.CodeProposalNotFound))
}

func F13WithdrawProposalNotProposer(s *scenario.Scenario) {
	p := seedOpenProposal(s, 2)
	s.Given(scenario.AsSeededUser("bob", p.voters[1])).
		When(scenario.Delete(p.path)).
		Then(scenario.ExpectProblem(errs.CodeNotProposer), scenario.ExpectEvents(events.TypeProposalWithdrawn, 0))
}

func F13WithdrawProposalProposalClosed(s *scenario.Scenario) {
	p := seedOpenProposal(s, 1)
	s.Given(scenario.AsSeededUser("alice", p.voters[0])).
		When(
			scenario.Delete(p.path),
			scenario.ExpectStatus(http.StatusOK),
			scenario.Delete(p.path),
		).
		Then(scenario.ExpectProblem(errs.CodeProposalClosed), scenario.ExpectEvents(events.TypeProposalWithdrawn, 1))
}

func F13WithdrawProposalWithdrawNotAllowed(s *scenario.Scenario) {
	p := seedOpenProposal(s, 3)
	s.Given(scenario.AsSeededUser("bob", p.voters[1])).
		When(
			scenario.Post(p.votes, no),
			scenario.ExpectStatus(http.StatusOK),
			scenario.AsSeededUser("alice", p.voters[0]),
			scenario.Delete(p.path),
		).
		Then(
			scenario.ExpectProblem(errs.CodeWithdrawNotAllowed),
			scenario.ExpectEvents(events.TypeProposalWithdrawn, 0),
		)
}

func F13WithdrawProposalCrashAfterPublish(s *scenario.Scenario) {
	p := seedOpenProposal(s, 1)
	s.Given(scenario.AsSeededUser("alice", p.voters[0]), scenario.HoldRelay()).
		When(
			scenario.Delete(p.path),
			scenario.ExpectStatus(http.StatusOK),
			scenario.ExpectJSON("status", "withdrawn"),
			scenario.PublishCrashingAt(faultpoint.AfterPublish),
		).
		Then(
			scenario.ExpectEvents(events.TypeProposalWithdrawn, 1),
			scenario.EventuallyPublished(events.TypeProposalWithdrawn, 1),
		)
}

const voidReason = "test void"

func voidFromOps(p openProposal, want errs.Code) scenario.Step {
	return func(s *scenario.Scenario) {
		pool, clk := s.DB(), clock.Real{}
		deps := module.Deps{Clock: clk, IDs: ids.Real{}, Pool: pool, UoW: db.New(pool, ids.Real{}, clk)}
		err := governance.New(deps).VoidFromOps(s.Context(), p.id, voidReason)
		got := errs.Code("")
		if err != nil {
			got = errs.CodeOf(err)
		}
		if got != want {
			s.Fatalf("flows: void %s = %v, want %q", p.id, err, want)
		}
	}
}

func voidedEvents(p openProposal, n int) scenario.Step {
	return func(s *scenario.Scenario) {
		var got int
		var reasons []string
		err := s.DB().QueryRow(s.Context(), `SELECT count(*), coalesce(array_agg(payload->>'actor_type' || ':' ||
			(payload->>'reason')), '{}') FROM events WHERE type = $1 AND aggregate_id = $2`,
			string(events.TypeProposalVoided), p.id.UUID()).Scan(&got, &reasons)
		if err != nil {
			s.Fatalf("flows: count proposal.voided: %v", err)
		}
		for _, r := range reasons {
			if r != "system:"+voidReason {
				s.Fatalf("flows: proposal.voided actor and reason %q, want system:%s", r, voidReason)
			}
		}
		if got != n {
			s.Fatalf("flows: %d proposal.voided events for %s, want %d", got, p.id, n)
		}
	}
}

func liveSwap(p openProposal) scenario.Step {
	return func(s *scenario.Scenario) {
		q, id, now := tradingsqlc.New(s.DB()), ids.Real{}.NewV7(), time.Now().UTC()
		err := q.InsertCreated(s.Context(), tradingsqlc.InsertCreatedParams{
			ID: id, SourceKind: "proposal", SourceID: p.id.UUID(), CabalID: ids.Real{}.NewV7(),
			TreasuryAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin", Action: "buy", Symbol: "AAPLx",
			InMint: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", OutMint: aaplxMint, OutDecimals: 8,
			InAmount: 5_000_000, QuoteOutAmount: pgtype.Int8{Int64: 21_000_000, Valid: true}, SlippageBps: 100,
			SourceBatchSize: 1, CreatedAt: now,
		})
		if err != nil {
			s.Fatalf("flows: insert swap: %v", err)
		}
		n, err := q.MarkSubmitted(s.Context(), tradingsqlc.MarkSubmittedParams{
			ID: id, ExecuteRequestID: "req-" + id.String(), SignedTx: []byte{1}, TxSignature: "sig-" + id.String(),
			SubmittedAt: now,
		})
		if err != nil || n != 1 {
			s.Fatalf("flows: submit swap: %d rows, %v", n, err)
		}
	}
}

func F13aVoidProposalOK(s *scenario.Scenario) {
	open, passed := seedOpenProposal(s, 3), seedOpenProposal(s, 1)
	s.Given(scenario.AsSeededUser("alice", passed.voters[0])).
		When(
			scenario.Post(passed.votes, yes),
			scenario.ExpectJSON("status", "passed"),
			voidFromOps(open, ""),
			voidFromOps(passed, ""),
			scenario.Get(passed.path),
			scenario.ExpectStatus(http.StatusOK),
			scenario.ExpectJSON("status", "voided"),
			scenario.AsSeededUser("bob", open.voters[0]),
			scenario.Get(open.path),
			scenario.ExpectJSON("status", "voided"),
			voidFromOps(open, errs.CodeProposalClosed),
		).
		Then(voidedEvents(open, 1), voidedEvents(passed, 1))
}

func F13aVoidProposalLiveSwapExists(s *scenario.Scenario) {
	p := seedOpenProposal(s, 1)
	s.Given(scenario.AsSeededUser("alice", p.voters[0])).
		When(
			scenario.Post(p.votes, yes),
			scenario.ExpectJSON("status", "passed"),
			liveSwap(p),
			voidFromOps(p, errs.CodeLiveSwapExists),
			scenario.Get(p.path),
			scenario.ExpectJSON("status", "passed"),
		).
		Then(voidedEvents(p, 0))
}
