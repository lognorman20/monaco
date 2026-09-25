import CoreImage
import CoreImage.CIFilterBuiltins
import SwiftUI

/// The invite link as a QR code: modules read out of Core Image's `CIQRCodeGenerator`, then
/// drawn as squares so the code is crisp at any size and exactly the ink and paper colours,
/// with no scaled bitmap and no colour-space round trip.
struct QRMatrix: Equatable {
    /// Modules per side, quiet zone excluded.
    let size: Int
    /// Row-major; true is a dark module.
    let modules: [Bool]

    func isDark(row: Int, column: Int) -> Bool {
        modules[row * size + column]
    }
}

enum InviteQRCode {
    private static let context = CIContext(options: [.cacheIntermediates: false])

    /// The QR matrix for `text` at error correction level M, cropped to its dark modules (the
    /// view draws its own quiet zone). Nil only if Core Image produced nothing.
    static func matrix(for text: String) -> QRMatrix? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        filter.correctionLevel = "M"
        guard let image = filter.outputImage else { return nil }

        let extent = image.extent.integral
        let width = Int(extent.width)
        let height = Int(extent.height)
        guard width > 0, width == height else { return nil }

        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        context.render(
            image,
            toBitmap: &pixels,
            rowBytes: width * 4,
            bounds: extent,
            format: .RGBA8,
            colorSpace: CGColorSpace(name: CGColorSpace.sRGB)
        )
        // Core Image's origin is bottom-left; row 0 of the bitmap is the top row.
        var dark = [Bool](repeating: false, count: width * height)
        for index in 0..<(width * height) {
            dark[index] = pixels[index * 4] < 128
        }

        // Crop to the dark modules' bounding box, so any margin the generator adds is gone.
        var top = height, bottom = -1, left = width, right = -1
        for row in 0..<height {
            for column in 0..<width where dark[row * width + column] {
                top = min(top, row); bottom = max(bottom, row)
                left = min(left, column); right = max(right, column)
            }
        }
        guard bottom >= top, right >= left, bottom - top == right - left else { return nil }
        let size = bottom - top + 1
        var modules = [Bool](repeating: false, count: size * size)
        for row in 0..<size {
            for column in 0..<size {
                modules[row * size + column] = dark[(top + row) * width + left + column]
            }
        }
        return QRMatrix(size: size, modules: modules)
    }
}

/// The invite's QR code: ink modules on paper, the same in light and dark so every camera
/// reads it. The paper tile is the quiet zone.
struct InviteQRCodeView: View {
    let link: URL

    /// Always the light scheme's ink and paper: a QR code must stay dark-on-light.
    static let ink = Color(hex: 0x0F291C)
    static let paper = Color(hex: 0xF8F5EE)

    @State private var matrix: QRMatrix?
    @Environment(\.displayScale) private var displayScale

    var body: some View {
        Canvas { context, size in
            guard let matrix else { return }
            // Whole device pixels per module, centred: no module is a hair wider than the next.
            let pixelsPerModule = floor(min(size.width, size.height) * displayScale / CGFloat(matrix.size))
            let module = pixelsPerModule / displayScale
            let side = module * CGFloat(matrix.size)
            let origin = CGPoint(
                x: ((size.width - side) / 2 * displayScale).rounded() / displayScale,
                y: ((size.height - side) / 2 * displayScale).rounded() / displayScale
            )
            var path = Path()
            for row in 0..<matrix.size {
                for column in 0..<matrix.size where matrix.isDark(row: row, column: column) {
                    path.addRect(CGRect(
                        x: origin.x + CGFloat(column) * module,
                        y: origin.y + CGFloat(row) * module,
                        width: module,
                        height: module
                    ))
                }
            }
            context.fill(path, with: .color(Self.ink))
        }
        .aspectRatio(1, contentMode: .fit)
        .padding(MonacoTheme.Space.sm)
        .background(
            Self.paper,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile, style: .continuous)
        )
        .task(id: link) {
            matrix = InviteQRCode.matrix(for: link.absoluteString)
        }
        .accessibilityElement()
        .accessibilityLabel(CabalInviteCopy.qrLabel)
        .accessibilityAddTraits(.isImage)
    }
}
