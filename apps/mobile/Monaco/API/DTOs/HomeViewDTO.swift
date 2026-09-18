import Foundation
import MonacoCore

// The shared package owns the wire schema; aliases preserve native call sites.
typealias HomeViewDTO = MonacoCore.HomeViewDTO
typealias HomeGroupBoardRowDTO = MonacoCore.HomeGroupBoardRowDTO
typealias HomePeopleBoardRowDTO = MonacoCore.HomePeopleBoardRowDTO

struct UserSharedGroupsResponse: Codable, Equatable {
    let groups: [HomeGroupBoardRowDTO]
}
