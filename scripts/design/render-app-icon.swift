#!/usr/bin/env swift
// Renders the Monaco brand mark into the app's image assets (CoreGraphics, no dependencies):
//   AppIcon-Light.png / AppIcon-Dark.png / AppIcon-Tinted.png  (1024², AppIcon.appiconset)
//   LaunchMark{,-Dark}@{1,2,3}x.png                             (96pt mark, LaunchMark.imageset)
//
// The artwork is not drawn here. It is rasterised from the brand's own vector — the four-person
// cluster in apps/web/assets/mark.svg, the same file the website ships — so the icon and the site
// cannot drift apart. The parser below handles exactly the subset that file uses: one <path> with
// absolute M / L / C / Z commands in a "0 0 100 100" viewBox, filled non-zero.
//
// Colours are the brand's: cream mark #F2EBE1 on the deep forest field #0F291C.
//
// Usage: swift scripts/design/render-app-icon.swift [assets-dir] [mark.svg]
//        (defaults: apps/mobile/Monaco/Assets.xcassets, apps/web/assets/mark.svg)
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

struct RGB { let r, g, b: CGFloat }
func hex(_ v: UInt32) -> RGB {
    RGB(r: CGFloat((v >> 16) & 0xFF) / 255, g: CGFloat((v >> 8) & 0xFF) / 255, b: CGFloat(v & 0xFF) / 255)
}

/// MonacoTheme's brand values. `field` is the logo's own background and `brandFill` in light mode.
let field = hex(0x0F291C)
/// A touch deeper for the dark variant, so the icon reads as a dark-mode icon next to its sibling.
let fieldDark = hex(0x0A1C13)
/// The cream in mark.svg's own `fill`.
let cream = hex(0xF2EBE1)
let black = hex(0x000000)
let white = hex(0xFFFFFF)

/// The mark occupies this fraction of the icon's width, centred. Apple's grid wants the artwork
/// clear of the squircle mask; at 0.72 the cluster still resolves at 40pt (see --preview).
let iconMarkScale: CGFloat = 0.72

// MARK: - SVG path

/// Parses the `d` attribute of the single `<path>` in an SVG into a CGPath.
/// Absolute M / L / C / Z only, with implicit command repetition, which is all mark.svg uses.
func parsePath(_ d: String) -> CGPath {
    let path = CGMutablePath()
    var numbers: [CGFloat] = []
    var command: Character = " "
    var start = CGPoint.zero
    var current = CGPoint.zero
    var scanner = d.startIndex

    func flush() {
        switch command {
        case "M":
            var i = 0
            while i + 1 < numbers.count {
                let p = CGPoint(x: numbers[i], y: numbers[i + 1])
                if i == 0 { path.move(to: p); start = p } else { path.addLine(to: p) }
                current = p
                i += 2
            }
        case "L":
            var i = 0
            while i + 1 < numbers.count {
                current = CGPoint(x: numbers[i], y: numbers[i + 1])
                path.addLine(to: current)
                i += 2
            }
        case "C":
            var i = 0
            while i + 5 < numbers.count {
                current = CGPoint(x: numbers[i + 4], y: numbers[i + 5])
                path.addCurve(
                    to: current,
                    control1: CGPoint(x: numbers[i], y: numbers[i + 1]),
                    control2: CGPoint(x: numbers[i + 2], y: numbers[i + 3])
                )
                i += 6
            }
        case "Z", "z":
            path.closeSubpath()
            current = start
        case " ":
            break
        default:
            fatalError("render-app-icon: unsupported SVG path command '\(command)'")
        }
        numbers.removeAll(keepingCapacity: true)
    }

    while scanner < d.endIndex {
        let ch = d[scanner]
        if ch.isLetter {
            flush()
            command = ch
            scanner = d.index(after: scanner)
        } else if ch.isNumber || ch == "-" || ch == "+" || ch == "." {
            var text = String(ch)
            scanner = d.index(after: scanner)
            while scanner < d.endIndex, d[scanner].isNumber || d[scanner] == "." || d[scanner] == "e"
                || ((d[scanner] == "-" || d[scanner] == "+") && (text.last == "e" || text.last == "E")) {
                text.append(d[scanner])
                scanner = d.index(after: scanner)
            }
            guard let value = Double(text) else { fatalError("render-app-icon: bad number '\(text)'") }
            numbers.append(CGFloat(value))
        } else {
            scanner = d.index(after: scanner)
        }
    }
    flush()
    return path
}

func loadMark(_ url: URL) -> CGPath {
    guard let svg = try? String(contentsOf: url, encoding: .utf8) else {
        fatalError("render-app-icon: cannot read \(url.path)")
    }
    guard let open = svg.range(of: " d=\""),
          let close = svg.range(of: "\"", range: open.upperBound..<svg.endIndex) else {
        fatalError("render-app-icon: no path data in \(url.path)")
    }
    return parsePath(String(svg[open.upperBound..<close.lowerBound]))
}

// MARK: - Drawing

func context(_ w: Int, _ h: Int) -> CGContext {
    let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpace(name: CGColorSpace.sRGB)!,
                        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    // Top-left origin, so SVG's y-down coordinates draw the right way up.
    ctx.translateBy(x: 0, y: CGFloat(h))
    ctx.scaleBy(x: 1, y: -1)
    ctx.setShouldAntialias(true)
    ctx.interpolationQuality = .high
    return ctx
}

func fill(_ ctx: CGContext, _ c: RGB) { ctx.setFillColor(red: c.r, green: c.g, blue: c.b, alpha: 1) }

/// Draws `mark` centred inside `frame`, preserving its aspect ratio.
func drawMark(_ ctx: CGContext, _ mark: CGPath, frame: CGRect, colour: RGB) {
    let bounds = mark.boundingBox
    let scale = min(frame.width / bounds.width, frame.height / bounds.height)
    var transform = CGAffineTransform.identity
        .translatedBy(x: frame.midX - bounds.midX * scale, y: frame.midY - bounds.midY * scale)
        .scaledBy(x: scale, y: scale)
    guard let scaled = mark.copy(using: &transform) else { fatalError("render-app-icon: transform failed") }
    ctx.addPath(scaled)
    fill(ctx, colour)
    ctx.fillPath()
}

func write(_ ctx: CGContext, _ url: URL) {
    let image = ctx.makeImage()!
    let dest = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, image, nil)
    guard CGImageDestinationFinalize(dest) else { fatalError("could not write \(url.path)") }
    print("wrote \(url.path)")
}

/// A full-bleed square icon: opaque field, no alpha anywhere, mark centred.
func icon(_ mark: CGPath, side: Int = 1024, background: RGB, colour: RGB, to url: URL) {
    let ctx = context(side, side)
    fill(ctx, background)
    ctx.fill(CGRect(x: 0, y: 0, width: CGFloat(side), height: CGFloat(side)))
    let inset = CGFloat(side) * (1 - iconMarkScale) / 2
    drawMark(ctx, mark, frame: CGRect(x: inset, y: inset,
                                      width: CGFloat(side) - 2 * inset,
                                      height: CGFloat(side) - 2 * inset), colour: colour)
    write(ctx, url)
}

/// The mark alone on transparent, centred in a square of `points` × scale with 2% breathing room.
func launchMark(_ mark: CGPath, points: Int, scale: Int, colour: RGB, to url: URL) {
    let side = CGFloat(points * scale)
    let ctx = context(points * scale, points * scale)
    let inset = side * 0.02
    drawMark(ctx, mark, frame: CGRect(x: inset, y: inset, width: side - 2 * inset, height: side - 2 * inset),
             colour: colour)
    write(ctx, url)
}

// MARK: - Main

var args = Array(CommandLine.arguments.dropFirst())
let wantsPreview = args.contains("--preview")
args.removeAll { $0 == "--preview" }

let assets = URL(fileURLWithPath: args.count > 0 ? args[0] : "apps/mobile/Monaco/Assets.xcassets")
let svg = URL(fileURLWithPath: args.count > 1 ? args[1] : "apps/web/assets/mark.svg")
let mark = loadMark(svg)

let iconDir = assets.appendingPathComponent("AppIcon.appiconset")
let launchDir = assets.appendingPathComponent("LaunchMark.imageset")
try FileManager.default.createDirectory(at: iconDir, withIntermediateDirectories: true)
try FileManager.default.createDirectory(at: launchDir, withIntermediateDirectories: true)

icon(mark, background: field, colour: cream, to: iconDir.appendingPathComponent("AppIcon-Light.png"))
icon(mark, background: fieldDark, colour: cream, to: iconDir.appendingPathComponent("AppIcon-Dark.png"))
// Tinted: iOS recolours by luminance, so this is the mark in white on black.
icon(mark, background: black, colour: white, to: iconDir.appendingPathComponent("AppIcon-Tinted.png"))

for scale in 1...3 {
    let suffix = scale == 1 ? "" : "@\(scale)x"
    launchMark(mark, points: 96, scale: scale, colour: field,
               to: launchDir.appendingPathComponent("LaunchMark\(suffix).png"))
    launchMark(mark, points: 96, scale: scale, colour: cream,
               to: launchDir.appendingPathComponent("LaunchMark-Dark\(suffix).png"))
}

if wantsPreview {
    // Home-screen sizes, so "does it still read at 40pt?" is a thing you look at rather than hope.
    let dir = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("monaco-icon-preview")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    for side in [40, 60, 80, 120, 180] {
        icon(mark, side: side, background: field, colour: cream,
             to: dir.appendingPathComponent("icon-\(side).png"))
    }
}
