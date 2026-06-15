import Cocoa

guard CommandLine.arguments.count > 1 else {
    print("Usage: swift remove-bg.swift <image-path>")
    exit(1)
}

let inputPath = CommandLine.arguments[1]
let url = URL(fileURLWithPath: inputPath)

guard let image = NSImage(contentsOf: url),
      let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    print("Could not load image")
    exit(1)
}

let w = cgImage.width
let h = cgImage.height
let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
let bpp = 4
let bpr = w * bpp

// Read input as ARGB (premultiplied first)
let inBmp = CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedFirst.rawValue)
guard let inData = calloc(w * h, bpp) else { exit(1) }
defer { free(inData) }
guard let inCtx = CGContext(data: inData, width: w, height: h, bitsPerComponent: 8, bytesPerRow: bpr, space: colorSpace, bitmapInfo: inBmp.rawValue) else { exit(1) }
inCtx.draw(cgImage, in: CGRect(x: 0, y: 0, width: w, height: h))

let pixels = inData.bindMemory(to: UInt8.self, capacity: w * h * bpp)

func colorAt(_ x: Int, _ y: Int) -> (UInt8, UInt8, UInt8) {
    let i = (y * w + x) * bpp
    return (pixels[i+1], pixels[i+2], pixels[i+3])
}

// Sample background from edges (skip first/last 30px to avoid window corners)
func avgEdgeColor() -> (UInt8, UInt8, UInt8) {
    var r=0, g=0, b=0, c=0
    let skip = 30
    for x in skip..<(w-skip) {
        for y in [0, h-1] {
            let p = colorAt(x, y)
            r+=Int(p.0); g+=Int(p.1); b+=Int(p.2); c+=1
        }
    }
    for y in skip..<(h-skip) {
        for x in [0, w-1] {
            let p = colorAt(x, y)
            r+=Int(p.0); g+=Int(p.1); b+=Int(p.2); c+=1
        }
    }
    return (UInt8(r/c), UInt8(g/c), UInt8(b/c))
}

let bg = avgEdgeColor()
print("Background: (\(bg.0), \(bg.1), \(bg.2))")

func sqDiff(_ a: (UInt8, UInt8, UInt8), _ b: (UInt8, UInt8, UInt8)) -> Int {
    let dr = Int(a.0) - Int(b.0)
    let dg = Int(a.1) - Int(b.1)
    let db = Int(a.2) - Int(b.2)
    return dr*dr + dg*dg + db*db
}

let threshold = 3000
var visited = [Bool](repeating: false, count: w * h)
var queue: [(Int, Int)] = []

// Seed queue with edge pixels that match background
for x in 0..<w {
    for y in [0, h-1] {
        if sqDiff(colorAt(x, y), bg) <= threshold {
            let idx = y * w + x
            if !visited[idx] {
                visited[idx] = true
                queue.append((x, y))
            }
        }
    }
}
for y in 0..<h {
    for x in [0, w-1] {
        if sqDiff(colorAt(x, y), bg) <= threshold {
            let idx = y * w + x
            if !visited[idx] {
                visited[idx] = true
                queue.append((x, y))
            }
        }
    }
}

// BFS flood-fill
var head = 0
while head < queue.count {
    let (x, y) = queue[head]
    head += 1
    let neighbors = [(x-1, y), (x+1, y), (x, y-1), (x, y+1)]
    for (nx, ny) in neighbors {
        guard nx >= 0 && nx < w && ny >= 0 && ny < h else { continue }
        let idx = ny * w + nx
        if !visited[idx] && sqDiff(colorAt(nx, ny), bg) <= threshold {
            visited[idx] = true
            queue.append((nx, ny))
        }
    }
}

// Create output with alpha (RGBA premultiplied last)
let outBmp = CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedLast.rawValue)
guard let outData = calloc(w * h, bpp) else { exit(1) }
defer { free(outData) }
let outPixels = outData.bindMemory(to: UInt8.self, capacity: w * h * bpp)

for y in 0..<h {
    for x in 0..<w {
        let i = (y * w + x) * bpp
        let c = colorAt(x, y)
        let idx = y * w + x
        if visited[idx] {
            // Edge-connected background → transparent
            outPixels[i] = 0
            outPixels[i+1] = 0
            outPixels[i+2] = 0
            outPixels[i+3] = 0
        } else {
            // Foreground → opaque
            outPixels[i] = c.0
            outPixels[i+1] = c.1
            outPixels[i+2] = c.2
            outPixels[i+3] = 255
        }
    }
}

guard let outCtx = CGContext(data: outData, width: w, height: h, bitsPerComponent: 8, bytesPerRow: bpr, space: colorSpace, bitmapInfo: outBmp.rawValue) else { exit(1) }
guard let outCG = outCtx.makeImage() else { exit(1) }

let output = NSImage(cgImage: outCG, size: NSSize(width: w, height: h))
guard let tiffData = output.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiffData),
      let pngData = bitmap.representation(using: .png, properties: [:]) else {
    print("Could not encode PNG")
    exit(1)
}

try pngData.write(to: url)
print("Background removed (flood-fill from edges): \(w)x\(h), visited \(queue.count) pixels")
