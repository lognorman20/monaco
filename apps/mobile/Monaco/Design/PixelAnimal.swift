import SwiftUI

/// The default face. A member without a photo is one of eight pixel animals, chosen by a
/// stable hash of their id, so they are the same animal on every screen and every device
/// and two members in a cabal rarely share one. Drawn at 16 by 16 and scaled without
/// smoothing, so the pixels stay pixels down to the 22pt faces on a proposal card.
///
/// The set is deliberately gentle: nothing that reads as a mascot, a meme or a threat.
enum PixelAnimal: String, CaseIterable, Sendable {
    case fox, owl, penguin, rabbit, cat, frog, bear, whale

    var imageName: String { "avatar-\(rawValue)" }

    /// Spoken as the alternative to a photo: "Fox".
    var spokenName: String { rawValue.capitalized }

    /// FNV-1a over the seed's bytes: cheap, stable across runs and devices, and unlike
    /// `hashValue` not randomised per process.
    static func forSeed(_ seed: String) -> PixelAnimal {
        var hash: UInt32 = 2_166_136_261
        for byte in seed.utf8 {
            hash ^= UInt32(byte)
            hash = hash &* 16_777_619
        }
        let all = allCases
        return all[Int(hash % UInt32(all.count))]
    }
}
