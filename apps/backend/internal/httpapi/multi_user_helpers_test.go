package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// multiUserApp wires every HTTP surface a club member touches against one real test
// database and one set of in-process fakes (Privy, Pyth, Jupiter, xStocks). Nothing
// here talks to a live provider, and member_wallets rows only land in the isolated
// `*_test` database, where TestIsolation removes them on cleanup.
type multiUserApp struct {
	Auth      *AuthHandlers
	Groups    *GroupHandlers
	Home      *HomeHandlers
	Deposits  *DepositHandlers
	Proposals *ProposalHandlers
	Quotes    *QuoteHandlers
	Catalog   *CatalogHandlers

	DepositService *app.DepositService
	Store          *postgres.Store
	Privy          wallets.Client
	Pyth           marks.Client
	Jupiter        dex.Client
	XStocks        b20.Catalog
	ISO            *postgres.TestIsolation
}

// clubMember is one authenticated Privy session with its Monaco user id and the
// Privy member wallet that backs its account balance.
type clubMember struct {
	Name   string
	UserID string
	Token  auth.AccessToken
	Wallet string
}

// club is a group created over HTTP.
type club struct {
	ID              string
	Name            string
	TreasuryAddress string
}

func newMultiUserApp(t *testing.T) *multiUserApp {
	t.Helper()

	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	pythClient := chainlink.NewFakeClient()
	jupiterClient := dex.NewFakeClient()
	resolver := b20.NewFakeCatalog()
	catalog := b20.NewFakeCatalog()
	symbols := app.NewSymbolResolver(catalog)

	deposits := app.NewDepositService(store, authHandlers.Verifier, privyClient, pythClient, symbols)
	home := app.NewHomeService(store, authHandlers.Verifier, privyClient, pythClient, deposits, symbols)
	buy := app.NewBuyService(jupiterClient, resolver)
	governance := app.NewGovernanceService(store, authHandlers.Verifier, privyClient)
	governance.SetBuyService(buy)
	governance.SetHomeService(home)

	return &multiUserApp{
		Auth: authHandlers,
		Groups: &GroupHandlers{
			Groups:     app.NewGroupService(store, authHandlers.Verifier, privyClient),
			Governance: governance,
			Home:       home,
		},
		Home:           &HomeHandlers{Home: home},
		Deposits:       &DepositHandlers{Deposits: deposits},
		Proposals:      &ProposalHandlers{Store: store, Auth: authHandlers.Verifier, Wallets: privyClient, Governance: governance},
		Quotes:         &QuoteHandlers{Store: store, Auth: authHandlers.Verifier, Wallets: privyClient, Buy: buy},
		Catalog:        &CatalogHandlers{Store: store, Auth: authHandlers.Verifier, Wallets: privyClient, Catalog: catalog},
		DepositService: deposits,
		Store:          store,
		Privy:          privyClient,
		Pyth:           pythClient,
		Jupiter:        jupiterClient,
		XStocks:        resolver,
		ISO:            iso,
	}
}

func (a *multiUserApp) signIn(t *testing.T, label, displayName string) clubMember {
	t.Helper()
	session, token := seedAuthenticatedUser(t, a.ISO, a.Auth, a.Privy, label, displayName)
	return clubMember{Name: displayName, UserID: session.UserID, Token: token, Wallet: session.MemberWalletAddress}
}

// call runs one handler with an optional bearer token, JSON body, and path values
// given as alternating key/value pairs.
func (a *multiUserApp) call(t *testing.T, handler http.HandlerFunc, method, target string, token auth.AccessToken, body string, pathValues ...string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	for i := 0; i+1 < len(pathValues); i += 2 {
		req.SetPathValue(pathValues[i], pathValues[i+1])
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status = %d, want %d; body = %s", what, rec.Code, want, rec.Body.String())
	}
}

func (a *multiUserApp) createClub(t *testing.T, owner clubMember, label string) club {
	t.Helper()
	name := fmt.Sprintf("%s %s", label, a.ISO.Suffix())
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	rec := a.call(t, a.Groups.CreateGroupHandler, http.MethodPost, "/v1/groups", owner.Token, string(body))
	requireStatus(t, rec, http.StatusOK, owner.Name+" POST /v1/groups")
	created := decodeBody[createGroupResponse](t, rec)
	trackCreatedGroup(a.ISO, created.GroupID)
	return club{ID: created.GroupID, Name: name, TreasuryAddress: created.TreasuryAddress}
}

func (a *multiUserApp) join(t *testing.T, member clubMember, c club) {
	t.Helper()
	rec := a.call(t, a.Groups.JoinGroupHandler, http.MethodPost, "/v1/groups/"+c.ID+"/join", member.Token, `{}`, "id", c.ID)
	requireStatus(t, rec, http.StatusNoContent, member.Name+" POST /v1/groups/{id}/join")
}

// topUpBalance adds USDC to the member's Privy wallet, which is their account balance
// (GET /v1/me/balance), as an external transfer in would.
func (a *multiUserApp) topUpBalance(t *testing.T, member clubMember, usdcMicros int64) {
	t.Helper()
	current, err := a.Privy.MemberUSDCBalance(context.Background(), member.Wallet)
	if err != nil {
		t.Fatalf("MemberUSDCBalance: %v", err)
	}
	wallets.SetMemberUSDCBalance(a.Privy, member.Wallet, current+usdcMicros)
}

// openFundIntent tops up the member's account balance and moves it toward the club
// over HTTP (POST /v1/groups/{id}/fund), leaving a pending deposit for the sweep.
func (a *multiUserApp) openFundIntent(t *testing.T, member clubMember, c club, usdcMicros int64) createDepositResponse {
	t.Helper()
	a.topUpBalance(t, member, usdcMicros)
	rec := a.call(t, a.Deposits.FundGroupHandler, http.MethodPost, "/v1/groups/"+c.ID+"/fund", member.Token,
		`{"amount":`+strconv.FormatInt(usdcMicros, 10)+`}`, "id", c.ID)
	requireStatus(t, rec, http.StatusOK, member.Name+" POST /v1/groups/{id}/fund")
	return decodeBody[createDepositResponse](t, rec)
}

// landSweepInTreasury moves USDC from the member wallet to the treasury the way a
// broadcast sweep would, before the deposit is confirmed.
func (a *multiUserApp) landSweepInTreasury(t *testing.T, member clubMember, c club, usdcMicros int64) {
	t.Helper()
	ctx := context.Background()
	memberBalance, err := a.Privy.MemberUSDCBalance(ctx, member.Wallet)
	if err != nil {
		t.Fatalf("MemberUSDCBalance: %v", err)
	}
	wallets.SetMemberUSDCBalance(a.Privy, member.Wallet, memberBalance-usdcMicros)
	treasury, err := a.Privy.TreasuryUSDCBalance(ctx, c.TreasuryAddress)
	if err != nil {
		t.Fatalf("TreasuryUSDCBalance: %v", err)
	}
	wallets.SetTreasuryUSDCBalance(a.Privy, c.TreasuryAddress, treasury+usdcMicros)
}

// confirmSweep applies the credit step the sweep poller runs after on-chain
// confirmation (DepositService.ObserveSweep), without polling FAKE* wallets.
func (a *multiUserApp) confirmSweep(t *testing.T, member clubMember, c club, intent createDepositResponse) {
	t.Helper()
	result, err := a.DepositService.ObserveSweep(context.Background(), app.ObservedSweep{
		TxHash:      fmt.Sprintf("sig-%s-%s", a.ISO.Suffix(), intent.DepositID),
		FromAddress: intent.FromAddress,
		ToAddress:   c.TreasuryAddress,
		Amount:      intent.Amount,
		DepositID:   intent.DepositID,
		UserID:      member.UserID,
		GroupID:     c.ID,
	})
	if err != nil {
		t.Fatalf("%s ObserveSweep: %v", member.Name, err)
	}
	if !result.Credited {
		t.Fatalf("%s ObserveSweep: expected a new credit", member.Name)
	}
}

// deposit runs the full credited path: account balance funded, POST /fund, USDC
// swept into the treasury, sweep confirmed.
func (a *multiUserApp) deposit(t *testing.T, member clubMember, c club, usdcMicros int64) createDepositResponse {
	t.Helper()
	intent := a.openFundIntent(t, member, c, usdcMicros)
	a.landSweepInTreasury(t, member, c, usdcMicros)
	a.confirmSweep(t, member, c, intent)
	return intent
}

// buyAAPLxAtMark records a confirmed treasury buy of whole AAPLx shares (what vote-pass
// execution persists) and marks the position per share through the fake Pyth client.
func (a *multiUserApp) buyAAPLxAtMark(t *testing.T, c club, usdcSpent, aaplShares, markUsdcPerShare int64) {
	t.Helper()
	ctx := context.Background()
	aaplAtomics := aaplShares * b20.TokenAtomicScale
	if _, _, err := a.Store.ConfirmBuyTransaction(ctx, postgres.ConfirmBuyTransactionParams{
		GroupID:          c.ID,
		Amount:           usdcSpent,
		InputToken:       dex.USDCAddress(),
		OutputToken:      "0xb200000000000000000000c2e324d24d7eecd1fb",
		TxHash:           fmt.Sprintf("sig-%s-buy-%s", a.ISO.Suffix(), c.ID),
		ExecuteRequestID: fmt.Sprintf("req-%s-buy-%s", a.ISO.Suffix(), c.ID),
		CostBasisPrice:   usdcSpent,
		CostBasisAmount:  aaplAtomics,
	}); err != nil {
		t.Fatalf("ConfirmBuyTransaction: %v", err)
	}
	before, err := a.Privy.TreasuryUSDCBalance(ctx, c.TreasuryAddress)
	if err != nil {
		t.Fatalf("TreasuryUSDCBalance: %v", err)
	}
	remaining := before - usdcSpent
	wallets.SetTreasuryUSDCBalance(a.Privy, c.TreasuryAddress, remaining)
	chainlink.RegisterMarkedPot(a.Pyth, marks.TreasuryRef{GroupID: c.ID, Address: c.TreasuryAddress}, marks.NavInput{
		TreasuryUsdc: remaining,
		Holdings: []marks.MarkedHolding{{
			Symbol:    "AAPLx",
			Token:     "0xb200000000000000000000c2e324d24d7eecd1fb",
			Units:     aaplAtomics,
			MarkUsdc:  markUsdcPerShare,
			CostBasis: usdcSpent,
		}},
	})
}

// registerRoutableAAPLx makes POST /quotes and POST /proposals routable for usdcMicros.
func (a *multiUserApp) registerRoutableAAPLx(usdcMicros int64) {
	b20.RegisterTokenAddress(a.XStocks, "AAPLx", "0xb200000000000000000000c2e324d24d7eecd1fb")
	dex.RegisterQuoteBuy(a.Jupiter, "0xb200000000000000000000c2e324d24d7eecd1fb", usdcMicros, dex.BuyQuote{
		Routable:    true,
		InputToken:  dex.USDCAddress(),
		OutputToken: "0xb200000000000000000000c2e324d24d7eecd1fb",
		InAmount:    strconv.FormatInt(usdcMicros, 10),
		OutAmount:   strconv.FormatInt(usdcMicros/2, 10),
	})
}

func (a *multiUserApp) propose(t *testing.T, member clubMember, c club, usdcMicros int64) string {
	t.Helper()
	rec := a.call(t, a.Proposals.CreateProposalHandler, http.MethodPost, "/v1/groups/"+c.ID+"/proposals", member.Token,
		`{"symbol":"AAPLx","usdc":`+strconv.FormatInt(usdcMicros, 10)+`}`, "id", c.ID)
	requireStatus(t, rec, http.StatusOK, member.Name+" POST /v1/groups/{id}/proposals")
	return decodeBody[createProposalResponse](t, rec).ProposalID
}

func (a *multiUserApp) vote(t *testing.T, member clubMember, proposalID, choice string) *httptest.ResponseRecorder {
	t.Helper()
	return a.call(t, a.Proposals.CastVoteHandler, http.MethodPost, "/v1/proposals/"+proposalID+"/votes", member.Token,
		`{"choice":"`+choice+`"}`, "id", proposalID)
}

func (a *multiUserApp) proposalDetail(t *testing.T, member clubMember, proposalID string) proposalDetailResponse {
	t.Helper()
	rec := a.call(t, a.Proposals.GetProposalDetailHandler, http.MethodGet, "/v1/proposals/"+proposalID, member.Token, "", "id", proposalID)
	requireStatus(t, rec, http.StatusOK, member.Name+" GET /v1/proposals/{id}")
	return decodeBody[proposalDetailResponse](t, rec)
}

func (a *multiUserApp) groupView(t *testing.T, member clubMember, c club) groupViewResponse {
	t.Helper()
	rec := a.call(t, a.Groups.GetGroupViewHandler, http.MethodGet, "/v1/groups/"+c.ID+"/view", member.Token, "", "id", c.ID)
	requireStatus(t, rec, http.StatusOK, member.Name+" GET /v1/groups/{id}/view")
	return decodeBody[groupViewResponse](t, rec)
}

func (a *multiUserApp) home(t *testing.T, member clubMember) homeResponse {
	t.Helper()
	rec := a.call(t, a.Home.HomeHandler, http.MethodGet, "/v1/home", member.Token, "")
	requireStatus(t, rec, http.StatusOK, member.Name+" GET /v1/home")
	return decodeBody[homeResponse](t, rec)
}

func (a *multiUserApp) dashboard(t *testing.T, member clubMember) homeDashboardResponse {
	t.Helper()
	rec := a.call(t, a.Home.HomeDashboardHandler, http.MethodGet, "/v1/home/dashboard?leaderboardRange=ALL", member.Token, "")
	requireStatus(t, rec, http.StatusOK, member.Name+" GET /v1/home/dashboard")
	return decodeBody[homeDashboardResponse](t, rec)
}

func findViewMember(t *testing.T, view groupViewResponse, member clubMember) groupViewMemberRowResponse {
	t.Helper()
	for _, row := range view.Members {
		if row.UserID == member.UserID {
			return row
		}
	}
	t.Fatalf("%s (%s) missing from member board (%d rows)", member.Name, member.UserID, len(view.Members))
	return groupViewMemberRowResponse{}
}

func findPerson(t *testing.T, people []homePeopleBoardRowResponse, member clubMember) homePeopleBoardRowResponse {
	t.Helper()
	for _, row := range people {
		if row.UserID == member.UserID {
			return row
		}
	}
	t.Fatalf("%s (%s) missing from people board (%d rows)", member.Name, member.UserID, len(people))
	return homePeopleBoardRowResponse{}
}

func requireNoPerson(t *testing.T, people []homePeopleBoardRowResponse, member clubMember, board string) {
	t.Helper()
	for _, row := range people {
		if row.UserID == member.UserID {
			t.Fatalf("%s: %s must not appear, got %+v", board, member.Name, row)
		}
	}
}

func findMyGroup(t *testing.T, rows []homeMyGroupRowResponse, c club) homeMyGroupRowResponse {
	t.Helper()
	for _, row := range rows {
		if row.GroupID == c.ID {
			return row
		}
	}
	t.Fatalf("club %q missing from dashboard myGroups (%d rows)", c.ID, len(rows))
	return homeMyGroupRowResponse{}
}

func requirePercent(t *testing.T, what string, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("%s percentReturn = %q, want nil", what, *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Fatalf("%s percentReturn = %v, want %s", what, derefOrNil(got), want)
	}
}

func derefOrNil(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}
