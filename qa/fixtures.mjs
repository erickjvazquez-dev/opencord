// Shared fixtures for the browser QA suites (browser.mjs + realtime.mjs).
import zlib from 'node:zlib'

function pngChunk(type, data) {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length)
  const typed = Buffer.concat([Buffer.from(type, 'ascii'), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(zlib.crc32(typed) >>> 0)
  return Buffer.concat([len, typed, crc])
}

// makePng builds a real, visibly-sized solid-colour PNG (8-bit truecolour RGB) so
// AI-vision QA actually SEES a rendered image, not a 1px dot. Sniffs as image/png.
export function makePng(w, h, [r, g, b]) {
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(w, 0)
  ihdr.writeUInt32BE(h, 4)
  ihdr[8] = 8 // bit depth
  ihdr[9] = 2 // colour type: truecolour RGB
  const row = Buffer.alloc(1 + w * 3) // leading filter byte (0) + RGB pixels
  for (let x = 0; x < w; x++) {
    row[1 + x * 3] = r
    row[2 + x * 3] = g
    row[3 + x * 3] = b
  }
  const raw = Buffer.concat(Array.from({ length: h }, () => row))
  const sig = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
  return Buffer.concat([
    sig,
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', zlib.deflateSync(raw)),
    pngChunk('IEND', Buffer.alloc(0)),
  ])
}
