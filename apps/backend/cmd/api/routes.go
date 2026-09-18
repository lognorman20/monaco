package main

import (
	"net/http"

	"github.com/monaco/monaco/apps/backend/internal/httpapi"
)

// routeHandlers supplies HTTP dependencies without starting runtime workers.
type routeHandlers struct {
	Auth         *httpapi.AuthHandlers
	Me           *httpapi.MeHandlers
	Home         *httpapi.HomeHandlers
	GroupsTab    *httpapi.GroupsTabHandlers
	Groups       *httpapi.GroupHandlers
	Deposits     *httpapi.DepositHandlers
	Transactions *httpapi.TransactionHandlers
	Catalog      *httpapi.CatalogHandlers
	Assets       *httpapi.AssetsHandlers
	Quotes       *httpapi.QuoteHandlers
	Proposals    *httpapi.ProposalHandlers
}

// newAPIMux registers production routes and returns the same patterns for startup logging.
func newAPIMux(h routeHandlers) (*http.ServeMux, []string) {
	mux := http.NewServeMux()
	var patterns []string
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, handler)
		patterns = append(patterns, pattern)
	}
	register("GET /health", httpapi.HealthHandler)
	register("POST /v1/auth/session", h.Auth.SessionHandler)
	register("GET /v1/me", h.Me.MeHandler)
	register("PATCH /v1/me", h.Me.PatchMeHandler)
	register("GET /v1/home", h.Home.HomeHandler)
	register("GET /v1/home/dashboard", h.Home.HomeDashboardHandler)
	register("GET /v1/home/pnl-series", h.Home.HomePnLSeriesHandler)
	register("GET /v1/home/missed-proposals", h.Home.HomeMissedProposalsHandler)
	register("GET /v1/users/{id}/groups", h.Home.UserSharedGroupsHandler)
	register("POST /v1/groups", h.Groups.CreateGroupHandler)
	register("GET /v1/groups/search", h.GroupsTab.SearchGroupsHandler)
	register("GET /v1/groups/leaderboard", h.GroupsTab.GroupLeaderboardHandler)
	register("POST /v1/groups/{id}/join", h.Groups.JoinGroupHandler)
	register("POST /v1/groups/{id}/leave", h.Groups.LeaveGroupHandler)
	register("GET /v1/groups/{id}/join-requests", h.Groups.ListJoinRequestsHandler)
	register("POST /v1/groups/{id}/join-requests/{requestId}/approve", h.Groups.ApproveJoinRequestHandler)
	register("POST /v1/groups/{id}/join-requests/{requestId}/deny", h.Groups.DenyJoinRequestHandler)
	register("GET /v1/groups/{id}", h.Groups.GetGroupHandler)
	register("GET /v1/groups/{id}/view", h.Groups.GetGroupViewHandler)
	register("GET /v1/groups/{id}/pnl-history", h.GroupsTab.GroupPnLHistoryHandler)
	register("GET /v1/groups/{id}/activity", h.Groups.ListGroupActivityHandler)
	register("POST /v1/groups/{id}/deposits", h.Deposits.CreateDepositHandler)
	register("GET /v1/groups/{id}/share-units", h.Deposits.GetMemberShareUnitsHandler)
	register("GET /v1/groups/{id}/treasury/usdc", h.Deposits.GetTreasuryUsdcBalanceHandler)
	register("GET /v1/deposits/{id}", h.Deposits.GetDepositHandler)
	register("GET /v1/transactions/{id}", h.Transactions.GetTransactionHandler)
	register("POST /v1/transactions/{id}/retry", h.Transactions.RetryTransactionHandler)
	register("GET /v1/groups/{id}/treasury/tokens", h.Transactions.GetTreasuryTokenBalancesHandler)
	register("GET /v1/groups/{id}/cost-basis/{symbol}", h.Transactions.GetCostBasisBySymbolHandler)
	register("GET /v1/groups/{id}/assets", h.Catalog.SearchAssetsHandler)
	register("GET /v1/assets", h.Assets.ListAssetsHandler)
	register("GET /v1/assets/popular", h.Assets.PopularAssetsHandler)
	register("GET /v1/assets/{symbol}", h.Assets.GetAssetHandler)
	register("GET /v1/assets/{symbol}/chart", h.Assets.GetAssetChartHandler)
	register("POST /v1/groups/{id}/quotes", h.Quotes.QuoteHandler)
	register("GET /v1/groups/{id}/proposals", h.Proposals.ListGroupProposalsHandler)
	register("POST /v1/groups/{id}/proposals", h.Proposals.CreateProposalHandler)
	register("GET /v1/proposals/{id}", h.Proposals.GetProposalDetailHandler)
	register("POST /v1/proposals/{id}/votes", h.Proposals.CastVoteHandler)
	return mux, patterns
}
