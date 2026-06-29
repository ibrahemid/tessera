import AppKit

// Deterministic app-icon generator: draws the Tessera mosaic mark (four
// tesserae, top-left gold) on an ink ground, and writes the macOS AppIcon set.
// Run: swift Tools/generate_icon.swift <Assets.xcassets/AppIcon.appiconset dir>

func color(_ hex: UInt32) -> NSColor {
    NSColor(srgbRed: CGFloat((hex >> 16) & 0xff) / 255, green: CGFloat((hex >> 8) & 0xff) / 255,
            blue: CGFloat(hex & 0xff) / 255, alpha: 1)
}

// Draws the icon into the CURRENT graphics context at the given point size.
func drawIcon(size: CGFloat) {
    let ctx = NSGraphicsContext.current!.cgContext

    // Ink rounded-rect ground with a soft vertical gradient.
    let bgRect = CGRect(x: 0, y: 0, width: size, height: size)
    let corner = size * 0.2237 // Apple squircle-ish radius
    let bgPath = NSBezierPath(roundedRect: bgRect, xRadius: corner, yRadius: corner)
    bgPath.addClip()
    let grad = NSGradient(colors: [color(0x20232E), color(0x121319)])!
    grad.draw(in: bgRect, angle: -90)

    // Four tesserae centered.
    let unit = size * 0.30
    let gap = size * 0.06
    let block = unit * 2 + gap
    let originX = (size - block) / 2
    let originY = (size - block) / 2
    let tileRadius = unit * 0.26

    let tiles: [(CGFloat, CGFloat, UInt32)] = [
        (originX, originY + unit + gap, 0xE3B23C),          // top-left: gold
        (originX + unit + gap, originY + unit + gap, 0x4A4E5C), // top-right: slate
        (originX, originY, 0x3A3E4A),                        // bottom-left
        (originX + unit + gap, originY, 0x2C2F3A),           // bottom-right
    ]
    for (x, y, hex) in tiles {
        let r = CGRect(x: x, y: y, width: unit, height: unit)
        let p = NSBezierPath(roundedRect: r, xRadius: tileRadius, yRadius: tileRadius)
        color(hex).setFill()
        p.fill()
    }
    // Subtle gold inner glow on the gold tile.
    ctx.setShadow(offset: .zero, blur: size * 0.04, color: color(0xE3B23C).withAlphaComponent(0.5).cgColor)
    let goldRect = CGRect(x: originX, y: originY + unit + gap, width: unit, height: unit)
    let goldPath = NSBezierPath(roundedRect: goldRect, xRadius: tileRadius, yRadius: tileRadius)
    color(0xE3B23C).setFill()
    goldPath.fill()
}

// Renders at EXACT pixel dimensions (1pt = 1px), independent of display scale,
// so actool accepts every icon slot.
func png(_ px: Int) -> Data {
    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px,
        bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { return Data() }
    rep.size = NSSize(width: px, height: px)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    drawIcon(size: CGFloat(px))
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:]) ?? Data()
}

let outDir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "./AppIcon.appiconset"
try? FileManager.default.createDirectory(atPath: outDir, withIntermediateDirectories: true)

// (filename, pixel size, idiom, scale, sizePt)
let specs: [(String, Int, String, String, String)] = [
    ("icon_16.png", 16, "mac", "1x", "16x16"),
    ("icon_16@2x.png", 32, "mac", "2x", "16x16"),
    ("icon_32.png", 32, "mac", "1x", "32x32"),
    ("icon_32@2x.png", 64, "mac", "2x", "32x32"),
    ("icon_128.png", 128, "mac", "1x", "128x128"),
    ("icon_128@2x.png", 256, "mac", "2x", "128x128"),
    ("icon_256.png", 256, "mac", "1x", "256x256"),
    ("icon_256@2x.png", 512, "mac", "2x", "256x256"),
    ("icon_512.png", 512, "mac", "1x", "512x512"),
    ("icon_512@2x.png", 1024, "mac", "2x", "512x512"),
]

var images: [String] = []
for (name, px, idiom, scale, sizePt) in specs {
    let data = png(px)
    try? data.write(to: URL(fileURLWithPath: "\(outDir)/\(name)"))
    images.append("""
        {
          "filename" : "\(name)",
          "idiom" : "\(idiom)",
          "scale" : "\(scale)",
          "size" : "\(sizePt)"
        }
    """)
}

let contents = """
{
  "images" : [
\(images.joined(separator: ",\n"))
  ],
  "info" : { "author" : "xcode", "version" : 1 }
}
"""
try? contents.write(toFile: "\(outDir)/Contents.json", atomically: true, encoding: .utf8)
print("wrote \(specs.count) icons to \(outDir)")
