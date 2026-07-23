package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

const (
	defaultPosterCacheControl   = "private, max-age=86400, stale-while-revalidate=604800"
	versionedPosterCacheControl = "private, max-age=31536000, immutable"
	posterJPEGContentType       = "image/jpeg"
	maxPosterThumbnailWidth     = 1280
	maxPosterThumbnailEntries   = 384
)

type posterThumbnailCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	order   []string
}

var (
	posterThumbnails     = posterThumbnailCache{entries: make(map[string][]byte)}
	posterThumbnailGroup singleflight.Group
	posterThumbnailSlots = make(chan struct{}, 2)
)

func (c *posterThumbnailCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.entries[key]
	return data, ok
}

func (c *posterThumbnailCache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	c.entries[key] = data
	c.order = append(c.order, key)
	if len(c.order) <= maxPosterThumbnailEntries {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}

func parsePosterWidth(value string) int {
	width, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || width <= 0 {
		return 0
	}
	if width < 64 {
		return 64
	}
	if width > maxPosterThumbnailWidth {
		return maxPosterThumbnailWidth
	}
	return width
}

func posterETag(data []byte, width int) string {
	digest := sha256.Sum256(data)
	return `"` + hex.EncodeToString(digest[:12]) + "-w" + strconv.Itoa(width) + `"`
}

func requestETagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func servePosterImage(c *gin.Context, original []byte) {
	width := parsePosterWidth(c.Query("width"))
	etag := posterETag(original, width)
	cacheControl := defaultPosterCacheControl
	if c.Query("v") != "" {
		cacheControl = versionedPosterCacheControl
	}
	c.Header("Cache-Control", cacheControl)
	c.Header("ETag", etag)
	c.Header("Vary", "Cookie")
	if requestETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}

	data := original
	contentType := http.DetectContentType(original)
	if width > 0 {
		cacheKey := etag
		if cached, ok := posterThumbnails.get(cacheKey); ok {
			data = cached
			contentType = posterJPEGContentType
		} else {
			value, err, _ := posterThumbnailGroup.Do(cacheKey, func() (any, error) {
				posterThumbnailSlots <- struct{}{}
				defer func() { <-posterThumbnailSlots }()
				thumbnail, resizeErr := resizePosterJPEG(original, width)
				if resizeErr != nil {
					return nil, resizeErr
				}
				posterThumbnails.put(cacheKey, thumbnail)
				return thumbnail, nil
			})
			if err == nil {
				data = value.([]byte)
				contentType = posterJPEGContentType
			}
		}
	}

	c.Data(http.StatusOK, contentType, data)
}

func resizePosterJPEG(data []byte, targetWidth int) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	if bounds.Dx() <= targetWidth {
		targetWidth = bounds.Dx()
	}
	targetHeight := int(math.Round(float64(bounds.Dy()) * float64(targetWidth) / float64(bounds.Dx())))
	if targetHeight < 1 {
		targetHeight = 1
	}
	destination := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	xScale := float64(bounds.Dx()) / float64(targetWidth)
	yScale := float64(bounds.Dy()) / float64(targetHeight)
	for y := range targetHeight {
		sourceY := (float64(y)+0.5)*yScale - 0.5
		for x := range targetWidth {
			sourceX := (float64(x)+0.5)*xScale - 0.5
			destination.SetRGBA(x, y, bilinearPosterPixel(source, bounds, sourceX, sourceY))
		}
	}

	var output bytes.Buffer
	if err := jpeg.Encode(&output, destination, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func bilinearPosterPixel(source image.Image, bounds image.Rectangle, sourceX, sourceY float64) color.RGBA {
	x0 := min(max(int(math.Floor(sourceX)), 0), bounds.Dx()-1)
	y0 := min(max(int(math.Floor(sourceY)), 0), bounds.Dy()-1)
	x1 := min(x0+1, bounds.Dx()-1)
	y1 := min(y0+1, bounds.Dy()-1)
	xWeight := sourceX - math.Floor(sourceX)
	yWeight := sourceY - math.Floor(sourceY)
	if sourceX < 0 {
		xWeight = 0
	}
	if sourceY < 0 {
		yWeight = 0
	}

	pixels := [4]color.NRGBA{
		color.NRGBAModel.Convert(source.At(bounds.Min.X+x0, bounds.Min.Y+y0)).(color.NRGBA),
		color.NRGBAModel.Convert(source.At(bounds.Min.X+x1, bounds.Min.Y+y0)).(color.NRGBA),
		color.NRGBAModel.Convert(source.At(bounds.Min.X+x0, bounds.Min.Y+y1)).(color.NRGBA),
		color.NRGBAModel.Convert(source.At(bounds.Min.X+x1, bounds.Min.Y+y1)).(color.NRGBA),
	}
	blend := func(values [4]uint8) uint8 {
		top := float64(values[0])*(1-xWeight) + float64(values[1])*xWeight
		bottom := float64(values[2])*(1-xWeight) + float64(values[3])*xWeight
		return uint8(math.Round(top*(1-yWeight) + bottom*yWeight))
	}
	r := blend([4]uint8{pixels[0].R, pixels[1].R, pixels[2].R, pixels[3].R})
	g := blend([4]uint8{pixels[0].G, pixels[1].G, pixels[2].G, pixels[3].G})
	b := blend([4]uint8{pixels[0].B, pixels[1].B, pixels[2].B, pixels[3].B})
	a := blend([4]uint8{pixels[0].A, pixels[1].A, pixels[2].A, pixels[3].A})
	if a < 255 {
		r = uint8((uint32(r)*uint32(a) + 255*uint32(255-a)) / 255)
		g = uint8((uint32(g)*uint32(a) + 255*uint32(255-a)) / 255)
		b = uint8((uint32(b)*uint32(a) + 255*uint32(255-a)) / 255)
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}
