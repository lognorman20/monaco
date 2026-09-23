package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/catalog"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const tSpaceXMint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"

func tesseraSpaceXAsset() xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:       "tSpaceX",
		Name:         "T-SpaceX",
		SolanaMint:   tSpaceXMint,
		Kind:         xstocks.AssetKindPreIPO,
		Source:       xstocks.AssetSourceTessera,
		Issuer:       "tessera",
		UnderlyingID: "spacex",
		Decimals:     9,
		Sector:       "Aerospace",
	}.Normalize()
}

type tesseraHTTPFixtures struct {
	Assets     *AssetsHandlers
	Quotes     *QuoteHandlers
	Proposals  *ProposalHandlers
	Groups     *GroupHandlers
	Auth       *AuthHandlers
	Privy      privy.Client
	Jupiter    jupiter.Client
	Price      jupiter.PriceClient
	Composite  *catalog.Composite
	ISO        *postgres.TestIsolation
}

func integrationTesseraHTTPFixtures(t *testing.T) tesseraHTTPFixtures {
	t.Helper()
	authHandlers, privyClient, db, iso := integrationApp(t)
	store := postgres.NewStore(db)
	jupiterClient := jupiter.NewFakeClient()
	resolver := xstocks.NewFakeResolver()
	xstocks.RegisterSolanaMint(resolver, "tSpaceX", tSpaceXMint)

	xs := xstocks.NewFakeCatalogSearcher()
	tessera := catalog.NewFakeSource(tesseraSpaceXAsset())
	prober := xstocks.NewFakeRoutabilityProber(true)
	xstocks.SetRoutable(prober, tSpaceXMint, true)
	composite := catalog.NewComposite(xs, tessera, prober)

	priceClient := jupiter.NewFakePriceClient()
	assets := &AssetsHandlers{
		Store:   store,
		Privy:   privyClient,
		Catalog: composite,
		Jupiter: jupiterClient,
		Price:   priceClient,
	}
	buy := app.NewBuyService(jupiterClient, resolver)
	buy.SetMintCatalog(composite)
	quoteHandlers := &QuoteHandlers{
		Store: store,
		Privy: privyClient,
		Buy:   buy,
		Price: priceClient,
	}
	symbols := app.NewSymbolResolver(composite)
	deposits := app.NewDepositService(store, privyClient, nil, symbols)
	home := app.NewHomeService(store, privyClient, nil, deposits, symbols)
	governance := app.NewGovernanceService(store, privyClient)
	governance.SetBuyService(buy)
	governance.SetHomeService(home)
	governance.SetPriceClient(priceClient)
	groupHandlers := &GroupHandlers{
		Groups:     app.NewGroupService(store, privyClient),
		Governance: governance,
		Home:       home,
		Catalog:    composite,
		Price:      priceClient,
	}
	proposalHandlers := &ProposalHandlers{
		Store:      store,
		Privy:      privyClient,
		Governance: governance,
		Catalog:    composite,
	}
	return tesseraHTTPFixtures{
		Assets:    assets,
		Quotes:    quoteHandlers,
		Proposals: proposalHandlers,
		Groups:    groupHandlers,
		Auth:      authHandlers,
		Privy:     privyClient,
		Jupiter:   jupiterClient,
		Price:     priceClient,
		Composite: composite,
		ISO:       iso,
	}
}

func registerTSpaceXPrice(client jupiter.PriceClient, dexMicros int64, refUsd float64) {
	now := time.Now().UTC()
	jupiter.RegisterPrice(client, tSpaceXMint, jupiter.TokenPrice{
		PriceUsdcMicros: dexMicros,
		StockData: &jupiter.StockData{
			Price:     refUsd,
			Mcap:      2_030_000_000_000,
			UpdatedAt: now,
		},
	})
}

func TestGET_assets_kindPreIpo_returnsOnlyTessera(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token := seedAssetsToken(t, fx.ISO, fx.Auth, fx.Privy)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?kind=pre_ipo", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.Assets.ListAssetsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload listAssetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Assets) != 1 || payload.Assets[0].Symbol != "tSpaceX" {
		t.Fatalf("assets = %+v, want single tSpaceX", payload.Assets)
	}
}

func TestGET_assets_kindInvalid_returns400(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token := seedAssetsToken(t, fx.ISO, fx.Auth, fx.Privy)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets?kind=etf", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.Assets.ListAssetsHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGET_asset_tSpaceX_hasKindDecimalsSectorReferenceMarkAlwaysOpen(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token := seedAssetsToken(t, fx.ISO, fx.Auth, fx.Privy)
	registerTSpaceXPrice(fx.Price, 562_000_000, 774)
	jupiter.RegisterQuoteBuy(fx.Jupiter, tSpaceXMint, app.CatalogRoutabilityProbeMicros, jupiter.BuyQuote{Routable: true, OutAmount: "1000000000"})
	jupiter.RegisterSellQuote(fx.Jupiter, tSpaceXMint, jupiter.AtomicScale(9), jupiter.SellQuote{Routable: true, InAmount: "1000000000", OutAmount: "500000000"})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/tSpaceX", nil)
	req.SetPathValue("symbol", "tSpaceX")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.Assets.GetAssetHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload assetDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Kind != "pre_ipo" || payload.TokenDecimals != 9 || payload.Sector != "Aerospace" || !payload.AlwaysOpen {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.ReferenceMarkUsdcMicros == nil || *payload.ReferenceMarkUsdcMicros != 774_000_000 {
		t.Fatalf("referenceMarkUsdcMicros = %v", payload.ReferenceMarkUsdcMicros)
	}
}

func TestGET_asset_T_SpaceX_alias_returnsCanonicalSymbol(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token := seedAssetsToken(t, fx.ISO, fx.Auth, fx.Privy)

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/T-SpaceX", nil)
	req.SetPathValue("symbol", "T-SpaceX")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.Assets.GetAssetHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload assetDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Symbol != "tSpaceX" {
		t.Fatalf("symbol = %q, want tSpaceX", payload.Symbol)
	}
}

func TestPOST_quotes_preIpo_returnsTokenDecimals9(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token, groupID, _ := createGroupForQuotes(t, fx.ISO, fx.Groups, fx.Auth, fx.Privy)
	jupiter.RegisterQuoteBuy(fx.Jupiter, tSpaceXMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: tSpaceXMint,
		InAmount:   "5000000",
		OutAmount:  "5000000000",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/quotes", strings.NewReader(`{"symbol":"tSpaceX","usdc":5000000}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	rec := httptest.NewRecorder()
	fx.Quotes.QuoteHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload quoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TokenDecimals != 9 || payload.Kind != "pre_ipo" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestCreateProposal_storesPremiumBpsOnce(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token, groupID, _ := createGroupForQuotes(t, fx.ISO, fx.Groups, fx.Auth, fx.Privy)
	registerTSpaceXPrice(fx.Price, 500_000_000, 774)
	jupiter.RegisterQuoteBuy(fx.Jupiter, tSpaceXMint, 5_000_000, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: tSpaceXMint,
		InAmount:   "5000000",
		OutAmount:  "5000000000",
	})

	createReq := httptest.NewRequest(http.MethodPost, "/v1/groups/"+groupID+"/proposals", strings.NewReader(`{"symbol":"tSpaceX","usdc":5000000}`))
	createReq.SetPathValue("id", groupID)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+string(token))
	createRec := httptest.NewRecorder()
	fx.Proposals.CreateProposalHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	registerTSpaceXPrice(fx.Price, 900_000_000, 774)

	listReq := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID+"/proposals?tab=open", nil)
	listReq.SetPathValue("id", groupID)
	listReq.Header.Set("Authorization", "Bearer "+string(token))
	listRec := httptest.NewRecorder()
	fx.Proposals.ListGroupProposalsHandler(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	var list listGroupProposalsResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Proposals) != 1 || list.Proposals[0].PremiumBps == nil {
		t.Fatalf("proposals = %+v, want stored premium", list.Proposals)
	}
	stored := *list.Proposals[0].PremiumBps
	if stored >= 0 {
		t.Fatalf("premiumBps = %d, want negative (dex below reference)", stored)
	}
}

func TestAssetDetail_sellProbe_usesOneWholeToken(t *testing.T) {
	t.Parallel()
	fx := integrationTesseraHTTPFixtures(t)
	token := seedAssetsToken(t, fx.ISO, fx.Auth, fx.Privy)
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xstocks.CatalogAssetFromXStockNode("AAPLx", "Apple", jupiter.AAPLxMint))
	prober := xstocks.NewFakeRoutabilityProber(true)
	xstocks.SetRoutable(prober, jupiter.AAPLxMint, true)
	fx.Assets.Catalog = catalog.NewComposite(xs, catalog.NewFakeSource(tesseraSpaceXAsset()), prober)

	jupiter.RegisterQuoteBuy(fx.Jupiter, jupiter.AAPLxMint, app.CatalogRoutabilityProbeMicros, jupiter.BuyQuote{Routable: true, OutAmount: "100000000"})
	jupiter.RegisterSellQuote(fx.Jupiter, jupiter.AAPLxMint, jupiter.AtomicScale(8), jupiter.SellQuote{Routable: true, InAmount: "100000000", OutAmount: "1"})

	req := httptest.NewRequest(http.MethodGet, "/v1/assets/AAPLx", nil)
	req.SetPathValue("symbol", "AAPLx")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.Assets.GetAssetHandler(rec, req)
	var stock assetDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &stock); err != nil {
		t.Fatal(err)
	}
	if stock.Liquidity.SellProbeInAmount != "100000000" {
		t.Fatalf("stock sell probe in = %q, want 100000000", stock.Liquidity.SellProbeInAmount)
	}

	jupiter.RegisterQuoteBuy(fx.Jupiter, tSpaceXMint, app.CatalogRoutabilityProbeMicros, jupiter.BuyQuote{Routable: true, OutAmount: "1000000000"})
	jupiter.RegisterSellQuote(fx.Jupiter, tSpaceXMint, jupiter.AtomicScale(9), jupiter.SellQuote{Routable: true, InAmount: "1000000000", OutAmount: "1"})
	req = httptest.NewRequest(http.MethodGet, "/v1/assets/tSpaceX", nil)
	req.SetPathValue("symbol", "tSpaceX")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	fx.Assets.GetAssetHandler(rec, req)
	var preIPO assetDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &preIPO); err != nil {
		t.Fatal(err)
	}
	if preIPO.Liquidity.SellProbeInAmount != "1000000000" {
		t.Fatalf("pre_ipo sell probe in = %q, want 1000000000", preIPO.Liquidity.SellProbeInAmount)
	}
}
