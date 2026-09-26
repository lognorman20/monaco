package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// portfolioHTTPApp is one member with a 60% slice of a cabal that owns 4 Apple, bought for
// $800 and marked at $250, with $200 left in cash. The cabal's name is a spreadsheet formula,
// which the CSV must keep as text.
type portfolioHTTPApp struct {
	handlers *PortfolioHandlers
	db       *sql.DB
	token    privy.AccessToken
	groupID  string
}

const portfolioHTTPCabalName = "=HYPERLINK(\"http://evil\")"

func newPortfolioHTTPApp(t *testing.T) portfolioHTTPApp {
	t.Helper()
	ctx := context.Background()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	pythClient := pyth.NewFakeClient()
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple", SolanaMint: jupiter.AAPLxMint})
	symbols := app.NewSymbolResolver(catalog)
	deposits := app.NewDepositService(store, privyClient, pythClient, symbols)
	home := app.NewHomeService(store, privyClient, pythClient, deposits, symbols)

	ada, token := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "portfolio-ada", "Ada")
	ben, _ := seedAuthenticatedUser(t, iso, authHandlers, privyClient, "portfolio-ben", "Ben")
	group, err := app.NewGroupService(store, privyClient).CreateGroup(ctx, string(token), portfolioHTTPCabalName)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	trackCreatedGroup(iso, group.GroupID)

	mustExec(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, group.GroupID, ben.UserID)
	mustExec(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 600000000, 600000000), ($3, $2, 400000000, 400000000)`,
		ada.UserID, group.GroupID, ben.UserID)
	mustExec(t, db, `INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature, created_at)
		VALUES ($1, $2, 600000000, 'ada', 'confirmed', $3, now() - interval '2 days')`,
		ada.UserID, group.GroupID, "sig-"+iso.Suffix()+"-ada-fund")
	mustExec(t, db, `INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature, created_at)
		VALUES ($1, $2, 400000000, 'ben', 'confirmed', $3, now() - interval '3 days')`,
		ben.UserID, group.GroupID, "sig-"+iso.Suffix()+"-ben-fund")
	mustExec(t, db, `INSERT INTO transactions (group_id, amount, action, input_mint, output_mint, status, tx_signature,
		  execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at, created_at)
		VALUES ($1, 800000000, 'buy', $2, $3, 'confirmed', $4, $5, 800000000, 400000000, now(), now() - interval '1 day')`,
		group.GroupID, jupiter.USDCMint, jupiter.AAPLxMint, "sig-"+iso.Suffix()+"-buy", "req-"+iso.Suffix()+"-buy")

	privy.SetTreasuryUSDCBalance(privyClient, group.TreasuryAddress, 200_000_000)
	pyth.RegisterMarkedPot(pythClient, pyth.TreasuryRef{GroupID: group.GroupID}, pyth.NavInput{Holdings: []pyth.MarkedHolding{
		{Symbol: "AAPLx", Mint: jupiter.AAPLxMint, MarkUsdc: 250_000_000, Source: pyth.MarkSourcePyth},
	}})

	return portfolioHTTPApp{
		handlers: &PortfolioHandlers{Portfolio: app.NewPortfolioService(home)},
		db:       db,
		token:    token,
		groupID:  group.GroupID,
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func (a portfolioHTTPApp) serve(t *testing.T, handler http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+string(a.token))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestGET_mePortfolio_returnsTheSliceByStockAndCabal(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newPortfolioHTTPApp(t)

	// Act
	rec := a.serve(t, a.handlers.GetPortfolioHandler, "/v1/me/portfolio")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload portfolioResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 60% of a $1,200 pot: $600 of Apple and $120 of cash.
	if payload.TotalUsd != "720.00" || payload.CashUsd != "120.00" || payload.DollarPnL != "+120.00" {
		t.Errorf("total/cash/pnl = %s/%s/%s, want 720.00/120.00/+120.00", payload.TotalUsd, payload.CashUsd, payload.DollarPnL)
	}
	if payload.AccountBalanceUsd == nil || *payload.AccountBalanceUsd != "0.00" {
		t.Errorf("accountBalanceUsd = %v, want 0.00", payload.AccountBalanceUsd)
	}
	if len(payload.Holdings) != 1 {
		t.Fatalf("holdings = %d, want 1", len(payload.Holdings))
	}
	apple := payload.Holdings[0]
	if apple.Symbol != "AAPLx" || apple.Name != "Apple" || apple.ValueUsd != "600.00" || apple.ShareOfTotal != "0.833333" {
		t.Errorf("apple = %+v", apple)
	}
	if len(apple.Cabals) != 1 || apple.Cabals[0].Quantity != "2.4" || apple.Cabals[0].DollarPnL != "+120.00" ||
		apple.Cabals[0].Tint != app.CabalTintName(a.groupID) {
		t.Errorf("apple cabals = %+v", apple.Cabals)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestGET_meTransactions_listsAPageWithACursor(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newPortfolioHTTPApp(t)

	// Act
	first := a.serve(t, a.handlers.ListHistoryHandler, "/v1/me/transactions?limit=1")

	// Assert
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", first.Code, first.Body.String())
	}
	var page historyPageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Kind != "buy" || page.NextCursor == nil {
		t.Fatalf("first page = %+v, want the buy and a cursor", page)
	}
	if page.Items[0].AmountUsd == nil || *page.Items[0].AmountUsd != "480.00" || page.Items[0].TransactionID == nil {
		t.Errorf("buy row = %+v, want $480.00 and its transaction id", page.Items[0])
	}

	second := a.serve(t, a.handlers.ListHistoryHandler, "/v1/me/transactions?limit=1&cursor="+*page.NextCursor)
	var next historyPageResponse
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].Kind != "fund" || next.NextCursor != nil {
		t.Errorf("second page = %+v, want Ada's fund and no cursor", next)
	}
}

func TestGET_meTransactions_rejectsABadQuery(t *testing.T) {
	t.Parallel()
	a := newPortfolioHTTPApp(t)
	for _, target := range []string{
		"/v1/me/transactions?type=deposits",
		"/v1/me/transactions?limit=zero",
		"/v1/me/transactions?cursor=not-ours",
	} {
		rec := a.serve(t, a.handlers.ListHistoryHandler, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body = %s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestGET_portfolioRoutes_missingAuth_return401(t *testing.T) {
	t.Parallel()
	handlers := &PortfolioHandlers{}
	for target, handler := range map[string]http.HandlerFunc{
		"/v1/me/portfolio":               handlers.GetPortfolioHandler,
		"/v1/me/transactions":            handlers.ListHistoryHandler,
		"/v1/me/transactions/export.csv": handlers.ExportHistoryCSVHandler,
	} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", target, rec.Code)
		}
	}
}

func TestGET_meTransactionsCSV_isADownloadOfEveryRow(t *testing.T) {
	t.Parallel()
	// Arrange
	a := newPortfolioHTTPApp(t)

	// Act
	rec := a.serve(t, a.handlers.ExportHistoryCSVHandler, "/v1/me/transactions/export.csv")

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="monaco-history.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", got)
	}
	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %d, want header + buy + fund:\n%s", len(records), rec.Body.String())
	}
	if strings.Join(records[0], ",") != "date,kind,status,cabal,stock,amount_usd,quantity,id" {
		t.Errorf("header = %v", records[0])
	}
	buy := records[1]
	if buy[1] != "buy" || buy[2] != "done" || buy[4] != "AAPLx" || buy[5] != "480.00" || buy[6] != "2.4" {
		t.Errorf("buy row = %v", buy)
	}
	if buy[3] != "'"+portfolioHTTPCabalName {
		t.Errorf("cabal cell = %q, want the formula neutralised with a leading quote", buy[3])
	}
	fund := records[2]
	if fund[1] != "fund" || fund[5] != "600.00" || fund[4] != "" || fund[6] != "" {
		t.Errorf("fund row = %v", fund)
	}
}

func TestGET_meTransactionsCSV_honoursTheFilter(t *testing.T) {
	t.Parallel()
	a := newPortfolioHTTPApp(t)

	rec := a.serve(t, a.handlers.ExportHistoryCSVHandler, "/v1/me/transactions/export.csv?type=money_in")

	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 2 || records[1][1] != "fund" {
		t.Errorf("money_in export = %v, want the header and the fund", records)
	}
}

func TestCSVText_neutralisesFormulaPrefixes(t *testing.T) {
	cases := map[string]string{
		"Sunday Investors": "Sunday Investors",
		"=SUM(A1)":         "'=SUM(A1)",
		"+1":               "'+1",
		"-1":               "'-1",
		"@cmd":             "'@cmd",
		"":                 "",
	}
	for in, want := range cases {
		if got := csvText(in); got != want {
			t.Errorf("csvText(%q) = %q, want %q", in, got, want)
		}
	}
}
