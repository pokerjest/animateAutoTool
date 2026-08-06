package worker

import (
	"log"
	"strconv"
	"sync"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

var metadataWorkerStartOnce sync.Once

// StartMetadataWorker 启动元数据处理 Worker
func StartMetadataWorker() {
	metadataWorkerStartOnce.Do(func() {
		event.GlobalBus.Subscribe(event.EventMetadataUpdated, func(e event.Event) {
			// Payload expectation: map[string]interface{}
			data, ok := e.Payload.(map[string]interface{})
			if !ok {
				log.Printf("WARN: MetadataWorker: ignored event with invalid payload type=%T", e.Payload)
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
			if db.DB == nil {
				log.Printf("ERROR: MetadataWorker: enrichment skipped local_anime_id=%d reason=database_unavailable", animeID)
				return
			}

			// Call Service
			// New service instance per task? safe for now.
			metaSvc := service.NewMetadataService()

			var anime model.LocalAnime
			if err := db.DB.First(&anime, animeID).Error; err != nil {
				log.Printf("WARN: MetadataWorker: anime lookup failed local_anime_id=%d error=%v", animeID, err)
				return
			}

			if err := metaSvc.EnrichAnime(&anime); err != nil {
				log.Printf("ERROR: MetadataWorker: enrichment failed local_anime_id=%d recovery_action=library_issue_and_retry error=%v", animeID, err)
				_ = service.ReportLibraryIssue(service.LibraryIssueInput{
					IssueKey:      "scrape:" + strconv.FormatUint(uint64(anime.ID), 10),
					IssueType:     service.LibraryIssueTypeScrape,
					Title:         anime.Title,
					DirectoryPath: anime.Path,
					LocalAnimeID:  &anime.ID,
					Message:       err.Error(),
					Hint:          service.MetadataIssueHint(err),
				})
			} else {
				log.Printf("MetadataWorker: enrichment completed local_anime_id=%d title=%q", anime.ID, anime.Title)
				_ = service.ResolveLibraryIssue("scrape:" + strconv.FormatUint(uint64(anime.ID), 10))
				// Notify Frontend of update
				// We can republish an event or relying on polling/SSE of "metadata_updated"
			}
		})
		log.Printf("MetadataWorker: subscribed to metadata events")
	})
}
