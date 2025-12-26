package main

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Define minimal models for migration
type AnimeMetadata struct {
	gorm.Model
	Title     string
	BangumiID int
}

// We need to know which tables reference metadata_id to fix FKs.
// Based on typical schema:
type Subscription struct {
	gorm.Model
	MetadataID *uint
}

type LocalAnime struct {
	gorm.Model
	MetadataID *uint
}

func main() {
	dbPath := "data/animate.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	// Identify duplicate groups
	var results []struct {
		BangumiID int
		Count     int
	}

	err = db.Model(&AnimeMetadata{}).
		Select("bangumi_id, count(*) as count").
		Where("bangumi_id > 0 AND deleted_at IS NULL").
		Group("bangumi_id").
		Having("count > 1").
		Scan(&results).Error

	if err != nil {
		log.Fatal("Failed to finding duplicates:", err)
	}

	log.Printf("Found %d duplications groups.", len(results))

	for _, r := range results {
		if r.BangumiID == 0 {
			continue
		}

		var metas []AnimeMetadata
		// Find all records for this duplicates bgm ID
		if err := db.Where("bangumi_id = ?", r.BangumiID).Order("id asc").Find(&metas).Error; err != nil {
			log.Printf("Error finding metas for %d: %v", r.BangumiID, err)
			continue
		}

		if len(metas) < 2 {
			continue
		}

		// Strategy: Keep the FIRST one (lowest ID), remove others.
		// Reason: Lower ID likely established first.
		// Better Strategy: Keep the one with most relations?
		// Simple Strategy: Keep first, move all relations to first.

		keeper := metas[0]
		removals := metas[1:]

		log.Printf("Processing duplicate group BangumiID=%d. Keeping ID=%d. Removing %d records.", r.BangumiID, keeper.ID, len(removals))

		for _, rm := range removals {
			// Repoint Subscriptions
			var subs []Subscription
			db.Where("metadata_id = ?", rm.ID).Find(&subs)
			if len(subs) > 0 {
				log.Printf("  -> Moving %d Subscriptions from ID %d to %d", len(subs), rm.ID, keeper.ID)
				db.Model(&Subscription{}).Where("metadata_id = ?", rm.ID).Update("metadata_id", keeper.ID)
			}

			// Repoint LocalAnimes
			var locals []LocalAnime
			db.Where("metadata_id = ?", rm.ID).Find(&locals)
			if len(locals) > 0 {
				log.Printf("  -> Moving %d LocalAnimes from ID %d to %d", len(locals), rm.ID, keeper.ID)
				db.Model(&LocalAnime{}).Where("metadata_id = ?", rm.ID).Update("metadata_id", keeper.ID)
			}

			// Finally, Hard Delete the duplicate to free up the constraint space
			if err := db.Unscoped().Delete(&rm).Error; err != nil {
				log.Printf("  -> ERROR deleting ID %d: %v", rm.ID, err)
			} else {
				log.Printf("  -> Deleted ID %d", rm.ID)
			}
		}
	}

	log.Println("Cleanup complete.")
}
