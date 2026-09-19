#!/usr/bin/env swift
// Renders the Monaco mark as vector-drawn PNGs (CoreGraphics, no dependencies):
//   AppIcon-Light.png / AppIcon-Dark.png / AppIcon-Tinted.png  (1024², AppIcon.appiconset)
//   LaunchMark{,-Dark}@{1,2,3}x.png                             (96pt mark, LaunchMark.imageset)
//
// The mark: three equal circles on a shallow rising diagonal, overlaps knocked out with a gap,
// the top-right circle in profit green. Three friends, rising, one of them winning.
//
// Usage: swift scripts/design/render-app-icon.swift [assets-dir]
//        (default: apps/mobile/Monaco/Assets.xcassets)
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

struct RGB { let r, g, b: CGFloat }
func hex(_ v: UInt32) -> RGB {
    RGB(r: CGFloat((v >> 16) & 0xFF) / 255, g: CGFloat((v >> 8) & 0xFF) / 255, b: CGFloat(v & 0xFF) / 255)
}

let ink = hex(0x161613)
let inkDark = hex(0x0D0D0C)
let paper = hex(0xF4F3EF)
let green = hex(0x3CCB7F)
let white = hex(0xFFFFFF)
let seventyWhite = RGB(r: 0.7, g: 0.7, b: 0.7)
let black = hex(0x000000)

/// Icon geometry on a 1024 canvas (spec §4): diameter 27% of width, centres (33%,60%) (50%,50%) (67%,40%).
struct Mark {
    static let diameter: CGFloat = 0.27
    static let centres: [CGPoint] = [CGPoint(x: 0.33, y: 0.60), CGPoint(x: 0.50, y: 0.50), CGPoint(x: 0.67, y: 0.40)]
    /// Knockout gap as a fraction of the canvas (14px at 1024).
    static let gap: CGFloat = 14.0 / 1024.0
}

func context(_ w: Int, _ h: Int) -> CGContext {
    let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpace(name: CGColorSpace.sRGB)!,
                        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    // Top-left origin so the geometry reads like the spec.
    ctx.translateBy(x: 0, y: CGFloat(h))
    ctx.scaleBy(x: 1, y: -1)
    ctx.setShouldAntialias(true)
    ctx.interpolationQuality = .high
    return ctx
}

func fill(_ ctx: CGContext, _ c: RGB) { ctx.setFillColor(red: c.r, green: c.g, blue: c.b, alpha: 1) }

/// Draws the three circles in `frame` (a square in canvas units, mark geometry relative to it).
/// Gaps are painted with `gapColor`, or cleared to transparent when nil.
func drawMark(_ ctx: CGContext, frame: CGRect, colors: [RGB], gapColor: RGB?) {
    let d = Mark.diameter * frame.width
    let r = d / 2
    let g = Mark.gap * frame.width
    for (i, c) in Mark.centres.enumerated() {
        let centre = CGPoint(x: frame.minX + c.x * frame.width, y: frame.minY + c.y * frame.height)
        if i > 0 {
            let knock = CGRect(x: centre.x - r - g, y: centre.y - r - g, width: d + 2 * g, height: d + 2 * g)
            if let gapColor {
                fill(ctx, gapColor)
                ctx.fillEllipse(in: knock)
            } else {
                ctx.saveGState()
                ctx.setBlendMode(.clear)
                ctx.fillEllipse(in: knock)
                ctx.restoreGState()
            }
        }
        fill(ctx, colors[i])
        ctx.fillEllipse(in: CGRect(x: centre.x - r, y: centre.y - r, width: d, height: d))
    }
}

func write(_ ctx: CGContext, _ url: URL) {
    let image = ctx.makeImage()!
    let dest = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, image, nil)
    guard CGImageDestinationFinalize(dest) else { fatalError("could not write \(url.path)") }
    print("wrote \(url.path)")
}

func icon(background: RGB, colors: [RGB], to url: URL) {
    let ctx = context(1024, 1024)
    fill(ctx, background)
    ctx.fill(CGRect(x: 0, y: 0, width: 1024, height: 1024))
    drawMark(ctx, frame: CGRect(x: 0, y: 0, width: 1024, height: 1024), colors: colors, gapColor: background)
    write(ctx, url)
}

/// The mark alone on transparent, cropped to its bounds and centred in a square of `points` × scale.
func launchMark(points: Int, scale: Int, circle: RGB, to url: URL) {
    let side = points * scale
    let ctx = context(side, side)
    // Mark bounds within the unit square: x 0.195…0.805, y 0.265…0.735 → width 0.61.
    let markWidth: CGFloat = 0.67 - 0.33 + Mark.diameter
    let unit = CGFloat(side) / (markWidth * 1.04) // 2% breathing room each side
    let midX: CGFloat = 0.5, midY: CGFloat = 0.5
    let frame = CGRect(x: CGFloat(side) / 2 - midX * unit, y: CGFloat(side) / 2 - midY * unit, width: unit, height: unit)
    drawMark(ctx, frame: frame, colors: [circle, circle, green], gapColor: nil)
    write(ctx, url)
}

let args = CommandLine.arguments
let assets = URL(fileURLWithPath: args.count > 1 ? args[1] : "apps/mobile/Monaco/Assets.xcassets")
let iconDir = assets.appendingPathComponent("AppIcon.appiconset")
let launchDir = assets.appendingPathComponent("LaunchMark.imageset")
try FileManager.default.createDirectory(at: launchDir, withIntermediateDirectories: true)

icon(background: ink, colors: [paper, paper, green], to: iconDir.appendingPathComponent("AppIcon-Light.png"))
icon(background: inkDark, colors: [paper, paper, green], to: iconDir.appendingPathComponent("AppIcon-Dark.png"))
icon(background: black, colors: [seventyWhite, seventyWhite, white], to: iconDir.appendingPathComponent("AppIcon-Tinted.png"))
for scale in 1...3 {
    let suffix = scale == 1 ? "" : "@\(scale)x"
    launchMark(points: 96, scale: scale, circle: ink, to: launchDir.appendingPathComponent("LaunchMark\(suffix).png"))
    launchMark(points: 96, scale: scale, circle: paper, to: launchDir.appendingPathComponent("LaunchMark-Dark\(suffix).png"))
}
