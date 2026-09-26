package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/ratelimit"
)

// Settings write limits are per Monaco user and per API process, on top of the route-class
// limits in httpapi.
const (
	preferencesUpdateBurst    = 10
	preferencesUpdateInterval = 3 * time.Second // 20/minute sustained: a member flicking switches

	accountDeleteBurst    = 3
	accountDeleteInterval = 20 * time.Second
)

// NewPreferencesUpdateLimiter returns the production limiter for PATCH /v1/me/preferences.
func NewPreferencesUpdateLimiter() *ratelimit.Limiter {
	return ratelimit.New(preferencesUpdateBurst, preferencesUpdateInterval)
}

// NewAccountDeleteLimiter returns the production limiter for DELETE /v1/me.
func NewAccountDeleteLimiter() *ratelimit.Limiter {
	return ratelimit.New(accountDeleteBurst, accountDeleteInterval)
}

// ErrAccountDeleted means the Privy login belongs to an account its member deleted.
// POST /v1/auth/session answers it with 410 so the app can sign out and say why.
var ErrAccountDeleted = errors.New("account deleted")

// ErrBalanceUnavailable means the member's account balance could not be read from chain, so
// whether the account holds money is unknown and it cannot be deleted yet.
var ErrBalanceUnavailable = errors.New("account balance unavailable")

// DeletionBlockerKind names why an account cannot be deleted yet.
type DeletionBlockerKind string

const (
	// DeletionBlockerCabalSlice: the member still owns share units in a cabal.
	DeletionBlockerCabalSlice DeletionBlockerKind = "cabal_slice"
	// DeletionBlockerCashOutPending: a cash out from a cabal has not settled yet.
	DeletionBlockerCashOutPending DeletionBlockerKind = "cash_out_pending"
	// DeletionBlockerTransferPending: USDC is on its way into a cabal or out to an address.
	DeletionBlockerTransferPending DeletionBlockerKind = "transfer_pending"
	// DeletionBlockerAccountBalance: USDC sits in the member's account balance.
	DeletionBlockerAccountBalance DeletionBlockerKind = "account_balance"
)

// DeletionBlocker is one thing the member has to move out, or wait for, before the account
// can go. GroupID and GroupName are set for the cabal kinds.
type DeletionBlocker struct {
	Kind      DeletionBlockerKind
	GroupID   string
	GroupName string
	// ValueMicros is what the blocker is worth in USDC micros. ValueKnown is false when a
	// slice could not be priced right now; the slice still blocks.
	ValueMicros int64
	ValueKnown  bool
}

// DeletionCheck is the answer to "can this account be deleted now?".
type DeletionCheck struct {
	Blockers []DeletionBlocker
}

// CanDelete is true when nothing is left to move out.
func (c DeletionCheck) CanDelete() bool { return len(c.Blockers) == 0 }

// AccountDeletionBlockedError is DELETE /v1/me refused because money is still in the account.
type AccountDeletionBlockedError struct {
	Check DeletionCheck
}

func (e *AccountDeletionBlockedError) Error() string {
	return fmt.Sprintf("account deletion blocked: %d blockers", len(e.Check.Blockers))
}

// DeletedAccount is the result of DELETE /v1/me.
type DeletedAccount struct {
	UserID    string
	DeletedAt time.Time
}

// SliceValuer prices a member's slice of one cabal, the way Home prices it.
type SliceValuer interface {
	MemberSliceMicros(ctx context.Context, userID, groupID string) (int64, error)
}

// AccountService owns the signed-in member's settings and account: preferences, the
// deletion check and deleting the account.
type AccountService struct {
	store              *postgres.Store
	privy              privy.Client
	slices             SliceValuer
	preferencesLimiter *ratelimit.Limiter
	deleteLimiter      *ratelimit.Limiter
	now                func() time.Time
}

// NewAccountService wires account dependencies. slices prices cabal slices for the deletion
// check; HomeService is the production implementation.
func NewAccountService(store *postgres.Store, privyClient privy.Client, slices SliceValuer) *AccountService {
	return &AccountService{
		store:  store,
		privy:  privyClient,
		slices: slices,
		now:    time.Now,
	}
}

// WithPreferencesLimiter rate-limits preference writes per user. Nil disables limiting.
func (s *AccountService) WithPreferencesLimiter(limiter *ratelimit.Limiter) *AccountService {
	s.preferencesLimiter = limiter
	return s
}

// WithDeleteLimiter rate-limits account deletion per user. Nil disables limiting.
func (s *AccountService) WithDeleteLimiter(limiter *ratelimit.Limiter) *AccountService {
	s.deleteLimiter = limiter
	return s
}

// SetClock overrides time.Now for tests.
func (s *AccountService) SetClock(now func() time.Time) {
	if now == nil {
		s.now = time.Now
		return
	}
	s.now = now
}

// GetPreferences returns the member's preferences with every key present.
func (s *AccountService) GetPreferences(ctx context.Context, accessToken string) (Preferences, error) {
	user, err := s.authenticate(ctx, accessToken)
	if err != nil {
		return Preferences{}, err
	}
	raw, found, err := s.store.GetUserPreferences(ctx, user.ID)
	if err != nil {
		return Preferences{}, err
	}
	if !found {
		return Preferences{}, ErrUserNotFound
	}
	return PreferencesFromStored(raw), nil
}

// UpdatePreferences merges a validated patch into the member's preferences and returns the
// whole document. Every authenticated attempt, valid or not, spends one request from the
// per-user limiter, like PATCH /v1/me.
func (s *AccountService) UpdatePreferences(ctx context.Context, accessToken string, patch []byte) (Preferences, error) {
	user, err := s.authenticate(ctx, accessToken)
	if err != nil {
		return Preferences{}, err
	}
	if err := allowWrite(s.preferencesLimiter, user.ID); err != nil {
		return Preferences{}, err
	}
	canonical, err := ValidatePreferencesPatch(patch)
	if err != nil {
		return Preferences{}, err
	}
	raw, found, err := s.store.MergeUserPreferences(ctx, user.ID, canonical)
	if err != nil {
		return Preferences{}, err
	}
	if !found {
		return Preferences{}, ErrUserNotFound
	}
	slog.Info("preferences updated", "user_id", user.ID, "patch", string(canonical))
	return PreferencesFromStored(raw), nil
}

// DeletionCheck lists what the member has to move out before the account can be deleted:
// every cabal they still hold a slice in, cash outs and transfers still on their way, and
// the account balance when it is above zero.
func (s *AccountService) DeletionCheck(ctx context.Context, accessToken string) (DeletionCheck, error) {
	user, err := s.authenticate(ctx, accessToken)
	if err != nil {
		return DeletionCheck{}, err
	}
	holdings, err := s.store.ListAccountHoldings(ctx, user.ID)
	if err != nil {
		return DeletionCheck{}, err
	}
	available, err := s.availableBalance(ctx, user.ID, holdings.PendingMicros)
	if err != nil {
		return DeletionCheck{}, err
	}
	return s.deletionCheckFrom(ctx, user.ID, holdings, available), nil
}

// DeleteAccount deletes the signed-in member's account when nothing of theirs is left in it.
//
// The check runs again inside one transaction under the member funds lock, the lock a fund
// request takes to reserve balance, so money cannot arrive in a slice between the check and
// the delete. The row is then anonymised in the same transaction: see
// postgres.AnonymiseDeletedUserTx for what goes and what stays.
//
// Deleting is idempotent: a second call for an account already deleted answers with the
// original time and changes nothing.
func (s *AccountService) DeleteAccount(ctx context.Context, accessToken string) (DeletedAccount, error) {
	identity, err := s.verify(ctx, accessToken)
	if err != nil {
		return DeletedAccount{}, err
	}
	account, found, err := s.store.GetAccountByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return DeletedAccount{}, err
	}
	if !found {
		return DeletedAccount{}, ErrUserNotFound
	}
	if account.DeletedAt.Valid {
		slog.Info("account delete repeated", "user_id", account.ID)
		return DeletedAccount{UserID: account.ID, DeletedAt: account.DeletedAt.Time.UTC()}, nil
	}
	if err := allowWrite(s.deleteLimiter, account.ID); err != nil {
		return DeletedAccount{}, err
	}

	memberAddress, err := s.memberAddress(ctx, account.ID)
	if err != nil {
		return DeletedAccount{}, err
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return DeletedAccount{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := s.store.LockMemberFundsTx(ctx, tx, account.ID); err != nil {
		return DeletedAccount{}, err
	}
	holdings, err := s.store.ListAccountHoldingsTx(ctx, tx, account.ID)
	if err != nil {
		return DeletedAccount{}, err
	}
	available, err := s.availableBalanceAt(ctx, memberAddress, holdings.PendingMicros)
	if err != nil {
		return DeletedAccount{}, err
	}
	if holdingsBlock(holdings, available) {
		// Release the funds lock before pricing slices, which reads the chain.
		_ = tx.Rollback()
		check := s.deletionCheckFrom(ctx, account.ID, holdings, available)
		slog.Info("account delete blocked", "user_id", account.ID, "blockers", len(check.Blockers))
		return DeletedAccount{}, &AccountDeletionBlockedError{Check: check}
	}

	deletedAt := s.now().UTC()
	cabalsLeft, updated, err := s.store.AnonymiseDeletedUserTx(ctx, tx, account.ID, deletedAt)
	if err != nil {
		return DeletedAccount{}, err
	}
	if !updated {
		// Another request deleted it first; answer with what that one wrote.
		_ = tx.Rollback()
		again, found, err := s.store.GetAccountByPrivyUserID(ctx, identity.PrivyUserID)
		if err != nil {
			return DeletedAccount{}, err
		}
		if !found || !again.DeletedAt.Valid {
			return DeletedAccount{}, ErrUserNotFound
		}
		return DeletedAccount{UserID: again.ID, DeletedAt: again.DeletedAt.Time.UTC()}, nil
	}
	if err := tx.Commit(); err != nil {
		return DeletedAccount{}, fmt.Errorf("commit account delete: %w", err)
	}
	committed = true

	slog.Info("account deleted", "user_id", account.ID, "cabals_left", cabalsLeft)
	return DeletedAccount{UserID: account.ID, DeletedAt: deletedAt}, nil
}

func (s *AccountService) verify(ctx context.Context, accessToken string) (privy.Identity, error) {
	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return privy.Identity{}, privy.ErrInvalidToken
		}
		return privy.Identity{}, fmt.Errorf("verify session: %w", err)
	}
	return identity, nil
}

func (s *AccountService) authenticate(ctx context.Context, accessToken string) (postgres.User, error) {
	identity, err := s.verify(ctx, accessToken)
	if err != nil {
		return postgres.User{}, err
	}
	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return postgres.User{}, err
	}
	if !found {
		return postgres.User{}, ErrUserNotFound
	}
	return user, nil
}

// memberAddress is the member's deposit address, or "" when no member wallet was ever made
// (then there is nothing on chain to hold).
func (s *AccountService) memberAddress(ctx context.Context, userID string) (string, error) {
	wallet, found, err := s.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return wallet.SolanaAddress, nil
}

func (s *AccountService) availableBalance(ctx context.Context, userID string, pendingMicros int64) (int64, error) {
	address, err := s.memberAddress(ctx, userID)
	if err != nil {
		return 0, err
	}
	return s.availableBalanceAt(ctx, address, pendingMicros)
}

// availableBalanceAt is the account balance exactly as GET /v1/me/balance computes it:
// member-wallet USDC on chain minus funds and withdrawals still in flight, floored at zero.
func (s *AccountService) availableBalanceAt(ctx context.Context, memberAddress string, pendingMicros int64) (int64, error) {
	if memberAddress == "" {
		return 0, nil
	}
	chain, err := s.privy.MemberUSDCBalance(ctx, memberAddress)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrBalanceUnavailable, err)
	}
	available := chain - pendingMicros
	if available < 0 {
		available = 0
	}
	return available, nil
}

func holdingsBlock(holdings postgres.AccountHoldings, availableMicros int64) bool {
	return len(holdings.Slices) > 0 ||
		len(holdings.CashOuts) > 0 ||
		holdings.PendingMicros > 0 ||
		availableMicros > 0
}

// deletionCheckFrom turns what the database and the chain said into blockers, pricing each
// slice. Slices come first (they are what the member acts on), then money on its way, then
// the balance, which is where cashing out a slice lands.
func (s *AccountService) deletionCheckFrom(ctx context.Context, userID string, holdings postgres.AccountHoldings, availableMicros int64) DeletionCheck {
	blockers := make([]DeletionBlocker, 0, len(holdings.Slices)+len(holdings.CashOuts)+2)
	blockers = append(blockers, s.priceSlices(ctx, userID, holdings.Slices)...)
	for _, cashOut := range holdings.CashOuts {
		blockers = append(blockers, DeletionBlocker{
			Kind:        DeletionBlockerCashOutPending,
			GroupID:     cashOut.GroupID,
			GroupName:   cashOut.GroupName,
			ValueMicros: cashOut.SliceMicros,
			ValueKnown:  true,
		})
	}
	if holdings.PendingMicros > 0 {
		blockers = append(blockers, DeletionBlocker{
			Kind:        DeletionBlockerTransferPending,
			ValueMicros: holdings.PendingMicros,
			ValueKnown:  true,
		})
	}
	if availableMicros > 0 {
		blockers = append(blockers, DeletionBlocker{
			Kind:        DeletionBlockerAccountBalance,
			ValueMicros: availableMicros,
			ValueKnown:  true,
		})
	}
	return DeletionCheck{Blockers: blockers}
}

// priceSlices values every slice at once. A slice that cannot be priced still blocks, with
// its value unknown rather than guessed.
func (s *AccountService) priceSlices(ctx context.Context, userID string, slices []postgres.SliceHolding) []DeletionBlocker {
	blockers := make([]DeletionBlocker, len(slices))
	var wg sync.WaitGroup
	for i, slice := range slices {
		blockers[i] = DeletionBlocker{
			Kind:      DeletionBlockerCabalSlice,
			GroupID:   slice.GroupID,
			GroupName: slice.GroupName,
		}
		if s.slices == nil {
			continue
		}
		wg.Add(1)
		go func(i int, groupID string) {
			defer wg.Done()
			value, err := s.slices.MemberSliceMicros(ctx, userID, groupID)
			if err != nil {
				slog.Warn("deletion check slice unpriced", "user_id", userID, "group_id", groupID, "err", err)
				return
			}
			blockers[i].ValueMicros = value
			blockers[i].ValueKnown = true
		}(i, slice.GroupID)
	}
	wg.Wait()
	return blockers
}

// MemberSliceMicros is the member's slice of groupID in USDC micros, priced the way the
// Home dashboard prices "your cabals".
func (h *HomeService) MemberSliceMicros(ctx context.Context, userID, groupID string) (int64, error) {
	position, found, err := h.viewerGroupPosition(ctx, userID, groupID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, ErrGroupNotFound
	}
	return position.EquityMicro, nil
}
