import Foundation
import MonacoCore

// The cabal-view DTOs live in MonacoCore, next to the client that decodes them and
// the tests that pin their shape. This file used to declare a second, identical
// copy of each, and because the app module wins name resolution over an imported
// one, the copy silently shadowed the real thing: fields added to the MonacoCore
// DTOs simply never reached the app. That is exactly how the market DTOs drifted,
// and it happened here too — a holdings row could not see the day change the
// backend had started sending.
//
// These aliases keep the names the app already uses while there is only one
// definition behind them.

typealias PotRowDTO = MonacoCore.PotRowDTO
typealias MemberSliceDTO = MonacoCore.MemberSliceDTO
typealias LeaderboardRowDTO = MonacoCore.LeaderboardRowDTO
typealias GroupAgentDTO = MonacoCore.GroupAgentDTO
typealias GroupViewDTO = MonacoCore.GroupViewDTO
