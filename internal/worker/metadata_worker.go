package worker

import (
	"log"
	"strconv"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// StartMetadataWorker 启动元数据处理 Worker
func StartMetadataWorker() {
	event.GlobalBus.Subscribe(event.EventMetadataUpdated, func(e event.Event) {
		// Payload expectation: map[string]interface{}
		data, ok := e.Payload.(map[string]interface{})
		if !ok {
			return
		}

		evtType, _ := data["type"].(string)
		if evtType != "new_anime" {
			return
		}

		animeID, ok := data["id"].(uint)
		if !ok {
			// handle float64 from JSON unmarshalling if passed via network?
			// internal bus is strict type if in-mem.
			return
		}

		log.Printf("Worker: Received new anime event for ID %d", animeID)

		// Call Service
		// New service instance per task? safe for now.
		metaSvc := service.NewMetadataService()

		var anime model.LocalAnime
		if err := db.DB.First(&anime, animeID).Error; err != nil {
			log.Printf("Worker: Anime %d not found in DB", animeID)
			return
		}

		if err := metaSvc.EnrichAnime(&anime); err != nil {
			log.Printf("Worker: Failed to enrich anime %d: %v", animeID, err)
			_ = service.ReportLibraryIssue(service.LibraryIssueInput{
				IssueKey:      "scrape:" + strconv.FormatUint(uint64(anime.ID), 10),
				IssueType:     service.LibraryIssueTypeScrape,
				Title:         anime.Title,
				DirectoryPath: anime.Path,
				LocalAnimeID:  &anime.ID,
				Message:       err.Error(),
				Hint:          "检查元数据源配置，或在详情里使用修正匹配手动关联番剧。",
			})
		} else {
			log.Printf("Worker: Automatically enriched anime %s", anime.Title)
			_ = service.ResolveLibraryIssue("scrape:" + strconv.FormatUint(uint64(anime.ID), 10))
			// Notify Frontend of update
			// We can republish an event or relying on polling/SSE of "metadata_updated"
		}
	})
}
