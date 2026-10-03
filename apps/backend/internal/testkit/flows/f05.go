package flows

import (
	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/events"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
	"github.com/monaco/monaco/apps/backend/internal/testkit/fakes"
	"github.com/monaco/monaco/apps/backend/internal/testkit/scenario"
)

const depositWallet = "5kwEmpcR8Txq1b4bDazRm9j4cx8Qo2aiE53rYA1dCDDP"

func F05CreditDepositOK(s *scenario.Scenario) {
	user := seedDepositWallet(s)
	s.Given(
		scenario.AsSeededUser("member", user.ID),
		scenario.FakeUpstream(fakes.Step{
			Route: "/rpc/getSignaturesForAddress", Action: fakes.ActionSucceed,
			Fixture: "/rpc/getSignaturesForAddress", Times: 100, Reset: true,
		}),
		scenario.FakeUpstream(fakes.Step{
			Route: "/rpc/getTransaction", Action: fakes.ActionSucceed,
			Fixture: "/rpc/getTransaction", Times: 100, Reset: true,
		}),
	).When(
		scenario.AwaitTick("funding.deposits"),
		scenario.AwaitTick("funding.deposits"),
		expectDeposit(user),
		scenario.EventuallyHint("balance_changed"),
	).Then()
}

func F05CreditDepositRPCUnavailable(s *scenario.Scenario) {
	seedDepositWallet(s)
	s.Given(scenario.FakeUpstream(fakes.Step{
		Route: "/rpc/getSignaturesForAddress", Action: fakes.ActionFail, Status: 503, Times: 100, Reset: true,
	})).When(
		scenario.AwaitTick("funding.deposits"),
		scenario.AwaitTick("funding.deposits"),
		scenario.ExpectTickFailed("funding.deposits", string(errs.CodeRPCUnavailable)),
	).Then(scenario.ExpectEvents(events.TypeDepositCredited, 0))
}

func seedDepositWallet(s *scenario.Scenario) testkit.SeededUser {
	user := testkit.SeedUser(seedT{s}, s.DB(), testkit.UserOpts{WithWallet: true})
	user.Address = chain.SolanaAddress(depositWallet)
	if _, err := s.DB().Exec(s.Context(), `DELETE FROM user_wallets WHERE address = $1`, user.Address); err != nil {
		s.Fatalf("flows: clear deposit wallet: %v", err)
	}
	if _, err := s.DB().Exec(
		s.Context(),
		`UPDATE user_wallets SET address = $1 WHERE user_id = $2`,
		user.Address,
		user.ID.UUID(),
	); err != nil {
		s.Fatalf("flows: update deposit wallet: %v", err)
	}
	if _, err := s.DB().Exec(
		s.Context(),
		`INSERT INTO deposit_cursors (wallet_address, last_signature, cursor_slot, scanned_at)
		VALUES ($1, '', 0, now()) ON CONFLICT (wallet_address) DO NOTHING`,
		user.Address,
	); err != nil {
		s.Fatalf("flows: seed deposit cursor: %v", err)
	}
	return user
}

func expectDeposit(user testkit.SeededUser) scenario.Step {
	return func(s *scenario.Scenario) {
		var count int
		if err := s.DB().QueryRow(
			s.Context(),
			`SELECT count(*) FROM deposits WHERE user_id = $1`,
			user.ID.UUID(),
		).Scan(&count); err != nil {
			s.Fatalf("flows: count deposits: %v", err)
		}
		if count != 1 {
			s.Fatalf("flows: deposits = %d, want 1", count)
		}
	}
}
