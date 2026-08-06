package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

const subscriptionPosterFetchTimeout = 10 * time.Second
const maxLocalPosterBytes = 12 << 20

var localPosterNames = []string{"poster.jpg", "poster.png", "cover.jpg", "cover.png", "folder.jpg", "folder.png"}

var searchSubscriptionMikan = func(ctx context.Context, title string) ([]parser.SearchResult, error) {
	return newConfiguredMikanParser().SearchContext(ctx, title)
}

// V1SubscriptionPosterHandler retries a subscription poster from a durable
// Mikan source or from the local metadata cache. The endpoint deliberately
// serves bytes instead of returning a remote URL so a browser does not need
// direct access to Mikan or any configured proxy.
func V1SubscriptionPosterHandler(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || id == 0 {
		c.Status(http.StatusBadRequest)
		return
	}

	sub, err := subscriptionWithMetadataByID(uint(id))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), subscriptionPosterFetchTimeout)
	defer cancel()

	var data []byte
	source := strings.ToLower(strings.TrimSpace(c.DefaultQuery("source", "mikan")))
	switch source {
	case "mikan":
		data, err = loadSubscriptionMikanPoster(ctx, sub)
	case "local":
		data, err = loadSubscriptionLocalPoster(ctx, sub)
	default:
		c.Status(http.StatusBadRequest)
		return
	}
	if err != nil || len(data) == 0 {
		if err != nil {
			log.Printf("subscription poster unavailable subscription_id=%d source=%s: %v", sub.ID, source, err)
		}
		c.Status(http.StatusBadGateway)
		return
	}
	servePosterImage(c, data)
}

func loadSubscriptionMikanPoster(ctx context.Context, sub *model.Subscription) ([]byte, error) {
	source, err := subscriptionMikanPosterSource(ctx, sub)
	if err != nil {
		return nil, err
	}
	return loadMikanPosterImage(ctx, source)
}

func subscriptionMikanPosterSource(ctx context.Context, sub *model.Subscription) (string, error) {
	if sub == nil {
		return "", errors.New("subscription is nil")
	}
	for _, candidate := range []string{sub.Image} {
		if source := trustedMikanPosterSource(candidate); source != "" {
			return source, nil
		}
	}

	mikanID := strings.TrimSpace(sub.MikanID)
	if mikanID == "" {
		if parsed, ok := parser.MikanIDFromRSSURL(sub.RSSUrl); ok {
			mikanID = parsed
		}
	}
	if mikanID == "" {
		return "", errors.New("subscription has no Mikan ID")
	}

	results, err := searchSubscriptionMikan(ctx, strings.TrimSpace(sub.Title))
	if err != nil {
		return "", fmt.Errorf("重新搜索 Mikan 番剧失败: %w", err)
	}
	for _, item := range results {
		if strings.TrimSpace(item.MikanID) != mikanID {
			continue
		}
		if source := trustedMikanPosterSource(item.Image); source != "" {
			return source, nil
		}
	}
	return "", fmt.Errorf("mikan 番剧 %s 没有可用海报", mikanID)
}

func trustedMikanPosterSource(raw string) string {
	parsed, err := validateMikanPosterURL(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.String()
}

func loadSubscriptionLocalPoster(ctx context.Context, sub *model.Subscription) ([]byte, error) {
	if sub == nil || db.DB == nil {
		return nil, errors.New("local poster store is unavailable")
	}
	localAnimes, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		return nil, fmt.Errorf("查找本地番剧失败: %w", err)
	}
	if len(localAnimes) == 0 {
		return nil, errors.New("未找到匹配的本地番剧")
	}

	for _, candidate := range localAnimes {
		var anime model.LocalAnime
		if err := db.DB.Preload("Metadata").First(&anime, candidate.ID).Error; err != nil {
			continue
		}
		if anime.Metadata != nil {
			for _, source := range []string{"", SourceBangumi, SourceTMDB, SourceAniList} {
				if data := metadataPosterData(anime.Metadata, source); len(data) > 0 {
					return data, nil
				}
			}
		}
		if data := loadLocalPosterFile(anime.Path); len(data) > 0 {
			return data, nil
		}
		if source := trustedMikanPosterSource(anime.Image); source != "" {
			if data, fetchErr := loadMikanPosterImage(ctx, source); fetchErr == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, errors.New("本地番剧没有可用海报缓存")
}

func loadLocalPosterFile(directory string) []byte {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil
	}
	for _, name := range localPosterNames {
		file, err := os.Open(filepath.Join(directory, name)) //nolint:gosec // directory comes from a scanned local-anime record.
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxLocalPosterBytes+1))
		_ = file.Close()
		if readErr != nil || len(data) == 0 || len(data) > maxLocalPosterBytes {
			continue
		}
		if strings.HasPrefix(http.DetectContentType(data), "image/") {
			return data
		}
	}
	return nil
}
