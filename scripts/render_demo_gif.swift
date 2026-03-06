import AppKit
import Foundation
import ImageIO
import UniformTypeIdentifiers

struct FrameSpec {
    let terminalLines: [String]
    let chatBubbles: [Bubble]
    let delay: Double
}

struct Bubble {
    let text: String
    let incoming: Bool
}

let width = 1440
let height = 900
let outputPath = "docs/assets/rcod-demo.gif"

let frames: [FrameSpec] = [
    FrameSpec(
        terminalLines: [
            "$ ./rcod",
            "",
            "Remote Control On Demand",
            "Bot is running",
            "",
            "Watching /Users/me/Projects",
            "Discovered 14 repositories",
        ],
        chatBubbles: [
            Bubble(text: "RCOD is online\nFound 14 projects under /Users/me/Projects", incoming: true),
        ],
        delay: 1.1
    ),
    FrameSpec(
        terminalLines: [
            "$ ./rcod",
            "",
            "Watching /Users/me/Projects",
            "Discovered 14 repositories",
            "Telegram: /folders",
            "Rendered repo picker (page 1/2)",
        ],
        chatBubbles: [
            Bubble(text: "/folders", incoming: false),
            Bubble(text: "Available projects\n• client/api\n• client/web\n• ops/infra\n• sandbox/agent", incoming: true),
        ],
        delay: 1.0
    ),
    FrameSpec(
        terminalLines: [
            "$ ./rcod",
            "",
            "Telegram: /start api",
            "Resolved partial match -> client/api",
            "Starting claude rc --permission-mode bypassPermissions",
            "Session 9f3a created",
        ],
        chatBubbles: [
            Bubble(text: "/start api", incoming: false),
            Bubble(text: "Session started\nID: 9f3a\nProject: client/api", incoming: true),
        ],
        delay: 1.0
    ),
    FrameSpec(
        terminalLines: [
            "$ ./rcod",
            "",
            "Session 9f3a running in client/api",
            "Claude URL detected",
            "https://claude.ai/chat/rcod-demo",
            "Tracking logs and events...",
        ],
        chatBubbles: [
            Bubble(text: "Session 9f3a is ready\nOpen in Claude", incoming: true),
        ],
        delay: 1.0
    ),
    FrameSpec(
        terminalLines: [
            "$ ./rcod",
            "",
            "[9f3a] Analyzing repo structure",
            "[9f3a] Updating CI workflow",
            "[9f3a] Refining project docs",
            "[9f3a] Progress heartbeat sent",
        ],
        chatBubbles: [
            Bubble(text: "Progress update\nSession 9f3a\nUptime: 10m0s\nLast output: 4s ago\nLatest log: [9f3a] Refining project docs", incoming: true),
        ],
        delay: 1.2
    ),
    FrameSpec(
        terminalLines: [
            "$ ./rcod",
            "",
            "[9f3a] Build: ok",
            "[9f3a] Tests: ok",
            "[9f3a] Vet: ok",
            "[9f3a] Waiting for next task...",
        ],
        chatBubbles: [
            Bubble(text: "/status", incoming: false),
            Bubble(text: "Session 9f3a\nProject: client/api\nStatus: running\nUptime: 42m13s\nClaude: open session", incoming: true),
        ],
        delay: 1.1
    ),
]

func roundedRect(_ rect: CGRect, radius: CGFloat) -> NSBezierPath {
    NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius)
}

func drawText(_ text: String, in rect: CGRect, font: NSFont, color: NSColor, alignment: NSTextAlignment = .left) {
    let paragraph = NSMutableParagraphStyle()
    paragraph.alignment = alignment
    paragraph.lineBreakMode = .byWordWrapping

    let attrs: [NSAttributedString.Key: Any] = [
        .font: font,
        .foregroundColor: color,
        .paragraphStyle: paragraph,
    ]
    let attributed = NSAttributedString(string: text, attributes: attrs)
    attributed.draw(with: rect, options: [.usesLineFragmentOrigin, .usesFontLeading])
}

func bubbleHeight(text: String, width: CGFloat, font: NSFont) -> CGFloat {
    let paragraph = NSMutableParagraphStyle()
    paragraph.lineBreakMode = .byWordWrapping
    let attrs: [NSAttributedString.Key: Any] = [
        .font: font,
        .paragraphStyle: paragraph,
    ]
    let rect = NSString(string: text).boundingRect(
        with: NSSize(width: width, height: .greatestFiniteMagnitude),
        options: [.usesLineFragmentOrigin, .usesFontLeading],
        attributes: attrs
    )
    return ceil(rect.height) + 30
}

func renderFrame(_ spec: FrameSpec, index: Int) -> CGImage? {
    let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: width,
        pixelsHigh: height,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    )

    guard let rep else { return nil }
    NSGraphicsContext.saveGraphicsState()
    guard let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
    NSGraphicsContext.current = ctx

    let canvas = CGRect(x: 0, y: 0, width: width, height: height)
    let background = NSGradient(colors: [
        NSColor(calibratedRed: 0.96, green: 0.94, blue: 0.90, alpha: 1),
        NSColor(calibratedRed: 0.84, green: 0.87, blue: 0.91, alpha: 1),
    ])!
    background.draw(in: canvas, angle: -25)

    let accentBlob = roundedRect(CGRect(x: 980, y: 610, width: 340, height: 180), radius: 90)
    NSColor(calibratedRed: 0.97, green: 0.70, blue: 0.38, alpha: 0.28).setFill()
    accentBlob.fill()

    let accentBlob2 = roundedRect(CGRect(x: 80, y: 60, width: 280, height: 140), radius: 70)
    NSColor(calibratedRed: 0.16, green: 0.47, blue: 0.52, alpha: 0.16).setFill()
    accentBlob2.fill()

    let titleFont = NSFont.systemFont(ofSize: 54, weight: .bold)
    let subtitleFont = NSFont.systemFont(ofSize: 22, weight: .medium)
    drawText("RCOD", in: CGRect(x: 90, y: 760, width: 280, height: 70), font: titleFont, color: NSColor(calibratedRed: 0.13, green: 0.20, blue: 0.20, alpha: 1))
    drawText("Remote Control On Demand for Claude Code", in: CGRect(x: 92, y: 725, width: 520, height: 40), font: subtitleFont, color: NSColor(calibratedRed: 0.20, green: 0.28, blue: 0.28, alpha: 0.95))

    let terminalRect = CGRect(x: 82, y: 138, width: 780, height: 540)
    let terminalShadow = NSShadow()
    terminalShadow.shadowBlurRadius = 24
    terminalShadow.shadowOffset = NSSize(width: 0, height: -8)
    terminalShadow.shadowColor = NSColor.black.withAlphaComponent(0.18)
    terminalShadow.set()
    NSColor(calibratedRed: 0.08, green: 0.10, blue: 0.12, alpha: 1).setFill()
    roundedRect(terminalRect, radius: 28).fill()
    NSGraphicsContext.restoreGraphicsState()
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx

    let terminalHeader = CGRect(x: terminalRect.minX, y: terminalRect.maxY - 48, width: terminalRect.width, height: 48)
    NSColor(calibratedRed: 0.12, green: 0.14, blue: 0.16, alpha: 1).setFill()
    roundedRect(terminalHeader, radius: 28).fill()
    NSColor(calibratedRed: 0.93, green: 0.38, blue: 0.34, alpha: 1).setFill()
    NSBezierPath(ovalIn: CGRect(x: terminalRect.minX + 20, y: terminalRect.maxY - 30, width: 12, height: 12)).fill()
    NSColor(calibratedRed: 0.95, green: 0.75, blue: 0.30, alpha: 1).setFill()
    NSBezierPath(ovalIn: CGRect(x: terminalRect.minX + 40, y: terminalRect.maxY - 30, width: 12, height: 12)).fill()
    NSColor(calibratedRed: 0.40, green: 0.80, blue: 0.48, alpha: 1).setFill()
    NSBezierPath(ovalIn: CGRect(x: terminalRect.minX + 60, y: terminalRect.maxY - 30, width: 12, height: 12)).fill()
    drawText("terminal", in: CGRect(x: terminalRect.midX - 60, y: terminalRect.maxY - 35, width: 120, height: 20), font: NSFont.systemFont(ofSize: 14, weight: .medium), color: NSColor.white.withAlphaComponent(0.65), alignment: .center)

    let monoFont = NSFont.monospacedSystemFont(ofSize: 22, weight: .regular)
    for (lineIndex, line) in spec.terminalLines.enumerated() {
        let y = terminalRect.maxY - 92 - CGFloat(lineIndex) * 42
        drawText(line, in: CGRect(x: terminalRect.minX + 30, y: y, width: terminalRect.width - 60, height: 30), font: monoFont, color: NSColor(calibratedRed: 0.80, green: 0.92, blue: 0.80, alpha: 1))
    }

    let phoneRect = CGRect(x: 960, y: 120, width: 360, height: 650)
    let phoneShadow = NSShadow()
    phoneShadow.shadowBlurRadius = 28
    phoneShadow.shadowOffset = NSSize(width: 0, height: -10)
    phoneShadow.shadowColor = NSColor.black.withAlphaComponent(0.22)
    phoneShadow.set()
    NSColor(calibratedRed: 0.13, green: 0.13, blue: 0.15, alpha: 1).setFill()
    roundedRect(phoneRect, radius: 42).fill()
    NSGraphicsContext.restoreGraphicsState()
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx

    let screenRect = CGRect(x: phoneRect.minX + 18, y: phoneRect.minY + 18, width: phoneRect.width - 36, height: phoneRect.height - 36)
    NSColor(calibratedRed: 0.90, green: 0.95, blue: 0.98, alpha: 1).setFill()
    roundedRect(screenRect, radius: 32).fill()

    let tgBlue = NSColor(calibratedRed: 0.15, green: 0.67, blue: 0.95, alpha: 1)
    let tgOutgoing = NSColor(calibratedRed: 0.83, green: 0.96, blue: 0.78, alpha: 1)
    let tgIncoming = NSColor.white
    let tgDivider = NSColor(calibratedRed: 0.83, green: 0.88, blue: 0.91, alpha: 1)
    let chatBG = NSColor(calibratedRed: 0.88, green: 0.94, blue: 0.96, alpha: 1)
    let patternColor = NSColor(calibratedRed: 0.72, green: 0.84, blue: 0.88, alpha: 0.16)

    let chatRect = CGRect(x: screenRect.minX, y: screenRect.minY + 76, width: screenRect.width, height: screenRect.height - 152)
    chatBG.setFill()
    roundedRect(chatRect, radius: 0).fill()
    for row in stride(from: Int(chatRect.minY) - 20, through: Int(chatRect.maxY) + 20, by: 42) {
        for col in stride(from: Int(chatRect.minX) - 20, through: Int(chatRect.maxX) + 20, by: 42) {
            let offset = ((row / 42) % 2 == 0) ? 0 : 18
            let dotRect = CGRect(x: col + offset, y: row, width: 8, height: 8)
            patternColor.setFill()
            NSBezierPath(ovalIn: dotRect).fill()
        }
    }

    let topBar = CGRect(x: screenRect.minX, y: screenRect.maxY - 92, width: screenRect.width, height: 92)
    NSColor.white.setFill()
    let topBarPath = NSBezierPath()
    topBarPath.appendRoundedRect(topBar, xRadius: 32, yRadius: 32)
    topBarPath.fill()
    tgDivider.setFill()
    NSBezierPath(rect: CGRect(x: topBar.minX, y: topBar.minY, width: topBar.width, height: 1)).fill()

    drawText("9:41", in: CGRect(x: screenRect.minX + 18, y: screenRect.maxY - 26, width: 50, height: 18), font: NSFont.systemFont(ofSize: 12, weight: .semibold), color: NSColor(calibratedWhite: 0.28, alpha: 1))
    let statusDot = NSBezierPath(ovalIn: CGRect(x: screenRect.maxX - 30, y: screenRect.maxY - 22, width: 7, height: 7))
    tgBlue.setFill()
    statusDot.fill()

    let backChevron = NSBezierPath()
    backChevron.move(to: CGPoint(x: screenRect.minX + 24, y: screenRect.maxY - 58))
    backChevron.line(to: CGPoint(x: screenRect.minX + 15, y: screenRect.maxY - 49))
    backChevron.line(to: CGPoint(x: screenRect.minX + 24, y: screenRect.maxY - 40))
    backChevron.lineWidth = 3
    tgBlue.setStroke()
    backChevron.lineCapStyle = .round
    backChevron.lineJoinStyle = .round
    backChevron.stroke()

    tgBlue.setFill()
    NSBezierPath(ovalIn: CGRect(x: screenRect.minX + 34, y: screenRect.maxY - 74, width: 34, height: 34)).fill()
    drawText("R", in: CGRect(x: screenRect.minX + 45, y: screenRect.maxY - 66, width: 16, height: 18), font: NSFont.systemFont(ofSize: 18, weight: .bold), color: .white, alignment: .center)
    drawText("RCOD Bot", in: CGRect(x: screenRect.minX + 78, y: screenRect.maxY - 60, width: 160, height: 24), font: NSFont.systemFont(ofSize: 22, weight: .bold), color: NSColor(calibratedWhite: 0.16, alpha: 1))
    drawText("last seen just now", in: CGRect(x: screenRect.minX + 78, y: screenRect.maxY - 82, width: 140, height: 18), font: NSFont.systemFont(ofSize: 13, weight: .medium), color: NSColor(calibratedWhite: 0.55, alpha: 1))

    let datePill = CGRect(x: screenRect.midX - 48, y: chatRect.maxY - 42, width: 96, height: 22)
    NSColor.white.withAlphaComponent(0.78).setFill()
    roundedRect(datePill, radius: 11).fill()
    drawText("Today", in: CGRect(x: datePill.minX, y: datePill.minY + 3, width: datePill.width, height: 16), font: NSFont.systemFont(ofSize: 12, weight: .semibold), color: NSColor(calibratedWhite: 0.50, alpha: 1), alignment: .center)

    let composerRect = CGRect(x: screenRect.minX, y: screenRect.minY, width: screenRect.width, height: 76)
    NSColor.white.setFill()
    let composerPath = NSBezierPath()
    composerPath.appendRoundedRect(composerRect, xRadius: 32, yRadius: 32)
    composerPath.fill()
    tgDivider.setFill()
    NSBezierPath(rect: CGRect(x: composerRect.minX, y: composerRect.maxY - 1, width: composerRect.width, height: 1)).fill()
    let inputRect = CGRect(x: composerRect.minX + 14, y: composerRect.minY + 15, width: composerRect.width - 72, height: 44)
    NSColor(calibratedRed: 0.95, green: 0.97, blue: 0.98, alpha: 1).setFill()
    roundedRect(inputRect, radius: 22).fill()
    drawText("Message", in: CGRect(x: inputRect.minX + 18, y: inputRect.minY + 12, width: 90, height: 20), font: NSFont.systemFont(ofSize: 16, weight: .medium), color: NSColor(calibratedWhite: 0.64, alpha: 1))
    tgBlue.setFill()
    NSBezierPath(ovalIn: CGRect(x: composerRect.maxX - 50, y: composerRect.minY + 13, width: 38, height: 38)).fill()
    let sendIcon = NSBezierPath()
    sendIcon.move(to: CGPoint(x: composerRect.maxX - 35, y: composerRect.minY + 34))
    sendIcon.line(to: CGPoint(x: composerRect.maxX - 23, y: composerRect.minY + 32))
    sendIcon.line(to: CGPoint(x: composerRect.maxX - 31, y: composerRect.minY + 24))
    sendIcon.close()
    NSColor.white.setFill()
    sendIcon.fill()

    let bubbleFont = NSFont.systemFont(ofSize: 17, weight: .regular)
    var bubbleY = chatRect.maxY - 70
    for bubble in spec.chatBubbles {
        let maxBubbleWidth: CGFloat = 236
        let bubbleW = maxBubbleWidth
        let bubbleH = bubbleHeight(text: bubble.text, width: bubbleW - 34, font: bubbleFont)
        bubbleY -= bubbleH + 14
        let bubbleX = bubble.incoming ? screenRect.minX + 16 : screenRect.maxX - bubbleW - 16
        let bubbleRect = CGRect(x: bubbleX, y: bubbleY, width: bubbleW, height: bubbleH)
        let bubbleColor = bubble.incoming ? tgIncoming : tgOutgoing
        bubbleColor.setFill()
        roundedRect(bubbleRect, radius: 22).fill()
        let textColor = NSColor(calibratedWhite: 0.14, alpha: 1)
        drawText(bubble.text, in: bubbleRect.insetBy(dx: 15, dy: 11), font: bubbleFont, color: textColor)
        drawText("11:\(String(format: "%02d", 20 + index))", in: CGRect(x: bubbleRect.maxX - 42, y: bubbleRect.minY + 8, width: 34, height: 14), font: NSFont.systemFont(ofSize: 10, weight: .medium), color: NSColor(calibratedWhite: 0.56, alpha: 1), alignment: .right)
    }

    let footerRect = CGRect(x: 90, y: 58, width: 760, height: 40)
    drawText("Start repos, watch Claude links, get progress updates, control sessions from Telegram.", in: footerRect, font: NSFont.systemFont(ofSize: 20, weight: .semibold), color: NSColor(calibratedRed: 0.15, green: 0.20, blue: 0.20, alpha: 0.9))

    NSGraphicsContext.restoreGraphicsState()
    return rep.cgImage
}

func writeGIF(frames: [FrameSpec], to path: String) throws {
    let outputURL = URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent(path)
    try FileManager.default.createDirectory(at: outputURL.deletingLastPathComponent(), withIntermediateDirectories: true)

    guard let destination = CGImageDestinationCreateWithURL(outputURL as CFURL, UTType.gif.identifier as CFString, frames.count, nil) else {
        throw NSError(domain: "RenderGIF", code: 1, userInfo: [NSLocalizedDescriptionKey: "Could not create GIF destination"])
    }

    let gifProperties: [CFString: Any] = [
        kCGImagePropertyGIFLoopCount: 0,
    ]
    CGImageDestinationSetProperties(destination, [
        kCGImagePropertyGIFDictionary: gifProperties,
    ] as CFDictionary)

    for (index, frame) in frames.enumerated() {
        guard let cgImage = renderFrame(frame, index: index) else {
            throw NSError(domain: "RenderGIF", code: 2, userInfo: [NSLocalizedDescriptionKey: "Could not render frame \(index)"])
        }

        CGImageDestinationAddImage(destination, cgImage, [
            kCGImagePropertyGIFDictionary: [
                kCGImagePropertyGIFDelayTime: frame.delay
            ]
        ] as CFDictionary)
    }

    if !CGImageDestinationFinalize(destination) {
        throw NSError(domain: "RenderGIF", code: 3, userInfo: [NSLocalizedDescriptionKey: "Could not finalize GIF"])
    }
}

do {
    try writeGIF(frames: frames, to: outputPath)
    print("Generated \(outputPath)")
} catch {
    fputs("Failed to generate GIF: \(error)\n", stderr)
    exit(1)
}
