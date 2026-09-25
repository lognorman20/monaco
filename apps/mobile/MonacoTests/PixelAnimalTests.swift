import Testing
import UIKit
@testable import Monaco

/// A member without a photo is one of eight pixel animals, the same one every time.
struct PixelAnimalTests {
    @Test func theSameSeedIsAlwaysTheSameAnimal() {
        #expect(PixelAnimal.forSeed("user-42") == PixelAnimal.forSeed("user-42"))
        #expect(PixelAnimal.forSeed("Logan Norman") == PixelAnimal.forSeed("Logan Norman"))
    }

    /// FNV-1a over a thousand ids lands on every animal; a set with a dead animal in it
    /// would mean the hash was folding badly.
    @Test func aCrowdWearsEveryAnimal() {
        let seen = Set((0..<1000).map { PixelAnimal.forSeed("member-\($0)") })
        #expect(seen.count == PixelAnimal.allCases.count)
    }

    /// Every animal is an image in the catalog, or a member would get an empty circle.
    @Test func everyAnimalIsInTheCatalog() {
        for animal in PixelAnimal.allCases {
            #expect(UIImage(named: animal.imageName) != nil, "\(animal.imageName) is missing")
        }
    }

    @Test func spokenNamesAreWords() {
        #expect(PixelAnimal.fox.spokenName == "Fox")
        #expect(PixelAnimal.penguin.spokenName == "Penguin")
        #expect(PixelAnimal.fox.withArticle == "a fox")
        #expect(PixelAnimal.owl.withArticle == "an owl")
    }

    /// A picked animal is uploaded as the profile photo, so every one must encode, and
    /// well under the 2 MB the server accepts.
    @Test func everyAnimalUploadsAsASmallPNG() {
        for animal in PixelAnimal.allCases {
            let data = UIImage(named: animal.imageName)?.pngData()
            #expect((data?.count ?? 0) > 0, "\(animal.imageName) has no PNG")
            #expect((data?.count ?? .max) < 512 * 1024, "\(animal.imageName) is too big to upload")
        }
    }
}
