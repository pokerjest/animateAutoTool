package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// PngToIco converts any image byte slice to a valid ICO byte slice (containing a 256x256 PNG).
// It decodes the input, resizes if necessary, encodes to PNG, and wraps in ICO.
func PngToIco(inputData []byte) ([]byte, error) {
	// 1. Decode generic image
	// This handles PNG, JPEG, GIF automatically if imports are present.
	img, _, err := image.Decode(bytes.NewReader(inputData))
	if err != nil {
		return nil, err
	}

	// 2. Resize to 256x256 (Max for ICO) if needed
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	validPngData := inputData
	// If decoded as PNG and size is OK, we could reuse inputData, but we can't easily know if inputData IS png bytes
	// without checking magic bytes or relying on format string from Decode.
	// Simpler to RE-ENCODE everything to PNG to be safe and consistent.

	targetW, targetH := 256, 256
	if width > 256 || height > 256 {
		// Downscale
		// Simple Nearest Neighbor implementation for stdlib (no external deps)
		dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
		xRatio := float64(width) / float64(targetW)
		yRatio := float64(height) / float64(targetH)

		for y := 0; y < targetH; y++ {
			for x := 0; x < targetW; x++ {
				srcX := int(float64(x) * xRatio)
				srcY := int(float64(y) * yRatio)
				// Naive sampling: pick top-left of the block
				dst.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
			}
		}
		img = dst
		width = targetW
		height = targetH
	}

	// 3. Re-encode as PNG
	pngBuf := new(bytes.Buffer)
	if err := png.Encode(pngBuf, img); err != nil {
		return nil, err
	}
	validPngData = pngBuf.Bytes()

	// ICO Width/Height 0 means 256
	wVal := uint8(width)
	hVal := uint8(height)
	if width >= 256 {
		wVal = 0
	}
	if height >= 256 {
		hVal = 0
	}

	// 4. Construct ICO
	buf := new(bytes.Buffer)

	// Header
	binary.Write(buf, binary.LittleEndian, uint16(0)) // Reserved
	binary.Write(buf, binary.LittleEndian, uint16(1)) // Type 1 (Icon)
	binary.Write(buf, binary.LittleEndian, uint16(1)) // Count 1

	// Dir Entry
	binary.Write(buf, binary.LittleEndian, wVal)
	binary.Write(buf, binary.LittleEndian, hVal)
	binary.Write(buf, binary.LittleEndian, uint8(0))                  // Colors
	binary.Write(buf, binary.LittleEndian, uint8(0))                  // Reserved
	binary.Write(buf, binary.LittleEndian, uint16(1))                 // Planes
	binary.Write(buf, binary.LittleEndian, uint16(32))                // BPP
	binary.Write(buf, binary.LittleEndian, uint32(len(validPngData))) // Size
	binary.Write(buf, binary.LittleEndian, uint32(22))                // Offset

	// Write PNG Data
	if _, err := buf.Write(validPngData); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Dummy use of draw package to ensure it compiles even if logic above doesn't strictly use interfaces that need it,
// though image.NewRGBA uses it.
var _ = draw.Draw
