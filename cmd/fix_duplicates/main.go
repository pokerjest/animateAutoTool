package main

import (
	"fmt"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type AnimeMetadata struct {
	ID        uint
	Title     string
	BangumiID int
}

func main() {
	dbPath := "data/animate.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	// 1. Find duplicate groups by Bangumi ID
	type Result struct {
		BangumiID int
		Count     int
	}
	var results []Result
	db.Raw("SELECT bangumi_id, count(*) as count FROM anime_metadata WHERE bangumi_id != 0 GROUP BY bangumi_id HAVING count > 1").Scan(&results)

	if len(results) > 0 {
		fmt.Printf("Found %d duplicate BangumiID groups. Fixing...\n", len(results))
		for _, r := range results {
			var metas []AnimeMetadata
			db.Table("anime_metadata").Where("bangumi_id = ?", r.BangumiID).Order("id asc").Find(&metas)

			if len(metas) < 2 {
				continue
			}

			keep := metas[0]
			fmt.Printf("Keeping ID %d for BangumiID %d, deleting others...\n", keep.ID, r.BangumiID)

			for i := 1; i < len(metas); i++ {
				dup := metas[i]
				// Remap Foreign Keys
				// Local Anime
				db.Exec("UPDATE local_anime SET metadata_id = ? WHERE metadata_id = ?", keep.ID, dup.ID)
				// Subscriptions
				db.Exec("UPDATE subscriptions SET metadata_id = ? WHERE metadata_id = ?", keep.ID, dup.ID)

				// Delete
				db.Exec("DELETE FROM anime_metadata WHERE id = ?", dup.ID)
			}
		}
	} else {
		fmt.Println("No duplicate BangumiIDs found.")
	}

	// 2. Scan for duplicate Title groups (where BangumiID is 0)
	// Theoretically we should merge these too if they are the exact same title.
	// Logic: If Title is identical and BangumiID is 0, they are likely duplicates created by failed scrapes.
	var titleResults []struct {
		Title string
		Count int
	}
	db.Raw("SELECT title, count(*) as count FROM anime_metadata WHERE bangumi_id = 0 AND title != '' GROUP BY title HAVING count > 1").Scan(&titleResults)
	if len(titleResults) > 0 {
		fmt.Printf("Found %d duplicate Title groups (BangumiID=0). Fixing...\n", len(titleResults))
		for _, r := range titleResults {
			var metas []AnimeMetadata
			db.Table("anime_metadata").Where("bangumi_id = 0 AND title = ?", r.Title).Order("id asc").Find(&metas)
			if len(metas) < 2 {
				continue
			}

			keep := metas[0]
			fmt.Printf("Keeping ID %d for Title '%s', deleting others...\n", keep.ID, r.Title)

			for i := 1; i < len(metas); i++ {
				dup := metas[i]
				db.Exec("UPDATE local_anime SET metadata_id = ? WHERE metadata_id = ?", keep.ID, dup.ID)
				db.Exec("UPDATE subscriptions SET metadata_id = ? WHERE metadata_id = ?", keep.ID, dup.ID)
				db.Exec("DELETE FROM anime_metadata WHERE id = ?", dup.ID)
			}
		}
	}

	// 3. Drop Index logic
	fmt.Println("Dropping old index...")
	db.Exec("DROP INDEX IF EXISTS idx_anime_metadata_bangumi_id")

	// 4. Create Unique Index
	fmt.Println("Creating UNIQUE INDEX...")
	err = db.Exec("CREATE UNIQUE INDEX idx_anime_metadata_bangumi_id ON anime_metadata(bangumi_id)").Error
	if err != nil {
		// Note: This will fail if there are still duplicates (e.g. BangumiID=0 duplicates).
		// We CANNOT put a unique index on '0' values if multiples exist.
		// So we must use a PARTIAL INDEX "WHERE bangumi_id != 0" or ensure 0 is treated specially?
		// Actually, standard SQL Unique Index allows multiple NULLs but NOT multiple 0s.
		// Since we use 0 for "unknown", we absolutely MUST filter them out or switch to NULL.
		// Switching locally to NULL is hard.
		// SOLUTION: CREATE UNIQUE INDEX ... WHERE bangumi_id != 0
		fmt.Printf("Standard unique index failed: %v. Trying Partial Index...\n", err)
		err = db.Exec("CREATE UNIQUE INDEX idx_anime_metadata_bangumi_id ON anime_metadata(bangumi_id) WHERE bangumi_id != 0").Error
		if err != nil {
			log.Fatalf("Failed to create partial unique index: %v", err)
		} else {
			fmt.Println("Partial Unique Index created successfully!")
		}
	} else {
		fmt.Println("Unique Index created successfully!")
	}
}
