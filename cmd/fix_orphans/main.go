package main

import (
	"fmt"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Minimal models
type AnimeMetadata struct {
	ID    uint
	Title string
}

type LocalAnime struct {
	ID         uint
	Title      string
	MetadataID *uint
}

func main() {
	dbPath := "data/animate.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	// 1. Identify Orphaned LocalAnime records
	var orphans []LocalAnime
	// Find LocalAnime where MetadataID points to non-existing Metadata
	err = db.Table("local_animes").
		Where("metadata_id IS NOT NULL AND metadata_id NOT IN (SELECT id FROM anime_metadata)").
		Find(&orphans).Error

	if err != nil {
		log.Fatal("Error finding orphans:", err)
	}

	if len(orphans) == 0 {
		fmt.Println("No orphaned LocalAnime records found.")
		return
	}

	fmt.Printf("Found %d orphaned LocalAnime records. Fixing...\n", len(orphans))

	fixedCount := 0
	resetCount := 0

	for _, orphan := range orphans {
		fmt.Printf("Orphan ID: %d, Title: '%s', Old MetaID: %d\n", orphan.ID, orphan.Title, *orphan.MetadataID)

		// Try to find matching metadata by title
		var match AnimeMetadata
		err := db.Table("anime_metadata").Where("title = ?", orphan.Title).First(&match).Error

		if err == nil && match.ID != 0 {
			// Found match! Relink
			fmt.Printf("  -> Found match! Linking to Metadata ID %d ('%s')\n", match.ID, match.Title)
			db.Table("local_animes").Where("id = ?", orphan.ID).Update("metadata_id", match.ID)
			fixedCount++
		} else {
			// No match found. Set to NULL so it can be re-scanned/enriched cleanly.
			fmt.Printf("  -> No match found. Resetting MetadataID to NULL.\n")
			db.Table("local_animes").Where("id = ?", orphan.ID).Update("metadata_id", nil)
			resetCount++
		}
	}

	fmt.Printf("Fix Complete. Fixed: %d, Reset: %d\n", fixedCount, resetCount)
}
