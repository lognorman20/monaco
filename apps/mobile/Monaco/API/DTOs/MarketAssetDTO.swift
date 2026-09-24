import Foundation
import MonacoCore

// The market DTOs live in MonacoCore, next to the client that decodes them and the
// tests that pin their shape. This file used to declare a second, identical copy of
// every one of them, and because the app module wins name resolution over an
// imported one, the copy silently shadowed the real thing: work done on the
// MonacoCore DTOs simply never reached the app, and the two drifted apart with
// nothing to notice.
//
// These aliases keep the names the app already uses while there is only one
// definition behind them. The two response envelopes keep their app-side spelling
// so no call site has to change.

typealias MarketAssetDTO = MonacoCore.MarketAssetDTO
typealias AssetLiquidityDTO = MonacoCore.AssetLiquidityDTO
typealias AssetDetailDTO = MonacoCore.AssetDetailDTO
typealias AssetStatsDTO = MonacoCore.AssetStatsDTO
typealias StockVsTokenDTO = MonacoCore.StockVsTokenDTO
typealias ReferenceQuoteDTO = MonacoCore.ReferenceQuoteDTO
typealias AssetChartPointDTO = MonacoCore.AssetChartPointDTO
typealias AssetChartDTO = MonacoCore.AssetChartDTO
typealias AssetChartRange = MonacoCore.AssetChartRange
typealias MarketStatusDTO = MonacoCore.MarketStatusDTO
typealias AssetSocialDTO = MonacoCore.AssetSocialDTO
typealias AssetHoldingDTO = MonacoCore.AssetHoldingDTO
typealias AssetProposalDTO = MonacoCore.AssetProposalDTO
typealias AssetVoterDTO = MonacoCore.AssetVoterDTO
typealias AssetActivityDTO = MonacoCore.AssetActivityDTO
typealias MarketSession = MonacoCore.MarketSession

typealias HeldAssetDTO = MonacoCore.HeldAssetDTO
typealias HeldAssetCabalDTO = MonacoCore.HeldAssetCabalDTO
typealias VotableAssetDTO = MonacoCore.VotableAssetDTO

typealias ListMarketAssetsResponse = MonacoCore.ListMarketAssetsResponseDTO
typealias PopularAssetsResponse = MonacoCore.PopularAssetsResponseDTO
typealias HeldAssetsResponse = MonacoCore.HeldAssetsResponseDTO
