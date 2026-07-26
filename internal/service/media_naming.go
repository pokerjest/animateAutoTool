package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/renamer"
)

func hydrateSubscriptionMetadata(sub *model.Subscription) {
	if sub == nil || sub.Metadata != nil || sub.MetadataID == nil || *sub.MetadataID == 0 || db.DB == nil {
		return
	}
	var metadata model.AnimeMetadata
	if err := db.DB.First(&metadata, *sub.MetadataID).Error; err == nil {
		sub.Metadata = &metadata
	}
}

func mediaSeriesTitle(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	hydrateSubscriptionMetadata(sub)
	if sub.Metadata != nil {
		for _, candidate := range []string{sub.Metadata.TitleCN, sub.Metadata.Title, sub.Metadata.TitleJP, sub.Metadata.TitleEN} {
			if value := strings.TrimSpace(candidate); value != "" {
				return parser.CleanTitle(value)
			}
		}
	}
	return parser.CleanTitle(strings.TrimSpace(sub.Title))
}

func mediaSeriesYear(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	hydrateSubscriptionMetadata(sub)
	if sub.Metadata == nil {
		return ""
	}
	airDate := strings.TrimSpace(sub.Metadata.AirDate)
	if len(airDate) >= 4 {
		if _, err := strconv.Atoi(airDate[:4]); err == nil {
			return airDate[:4]
		}
	}
	return ""
}

func mediaSeasonValue(sub *model.Subscription, sourceSeason string) string {
	value := strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(sourceSeason), "S"))
	if number, err := strconv.Atoi(value); err == nil && number >= 0 {
		if number == 1 && sub != nil {
			if titleSeason := parser.ParseSeason(sub.Title); titleSeason > 1 {
				return fmt.Sprintf("%02d", titleSeason)
			}
		}
		return fmt.Sprintf("%02d", number)
	}
	if sub != nil {
		return fmt.Sprintf("%02d", parser.ParseSeason(sub.Title))
	}
	return "01"
}

func mediaSeriesDirectory(sub *model.Subscription) (string, error) {
	pattern := strings.TrimSpace(configValue(model.ConfigKeyAutoRenameSeriesTemplate))
	if pattern == "" {
		pattern = renamer.DefaultSeriesTemplate
	}
	return renamer.FormatTemplate(pattern, renamer.TemplateData{
		Title: mediaSeriesTitle(sub),
		Year:  mediaSeriesYear(sub),
	})
}

func mediaEpisodeFilename(sub *model.Subscription, season, episode, ext, original string) (string, error) {
	pattern := strings.TrimSpace(configValue(model.ConfigKeyAutoRenameEpisodeTemplate))
	if pattern == "" {
		pattern = renamer.DefaultEpisodeTemplate
	}
	return renamer.FormatTemplate(pattern, renamer.TemplateData{
		Title:    mediaSeriesTitle(sub),
		Season:   mediaSeasonValue(sub, season),
		Episode:  episode,
		Year:     mediaSeriesYear(sub),
		Ext:      ext,
		Original: original,
	})
}

func mediaSeasonDirectory(sub *model.Subscription, season string) string {
	return "Season " + mediaSeasonValue(sub, season)
}

func mediaTargetDirectory(sub *model.Subscription, season, baseDir string) (string, error) {
	seriesDir, err := mediaSeriesDirectory(sub)
	if err != nil {
		return "", err
	}
	return joinDownloadPath(joinDownloadPath(baseDir, seriesDir), mediaSeasonDirectory(sub, season)), nil
}
