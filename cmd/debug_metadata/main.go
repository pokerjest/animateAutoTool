package main

import (
	"database/sql"
	"fmt"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type AnimeMetadata struct {
	ID        uint
	Title     string
	BangumiID int
	TMDBID    int
	AniListID int
}

func main() {
	dbPath := "data/animate.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	var tableName string
	db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'anime_metadata%'").Scan(&tableName)
	fmt.Printf("Using table: %s\n", tableName)

	if tableName == "" {
		log.Fatal("Table not found!")
	}

	// Double check column names
	var cols []string
	db.Raw(fmt.Sprintf("PRAGMA table_info(%s)", tableName)).Scan(&struct{ Name string }{})
	rows, _ := db.Raw(fmt.Sprintf("PRAGMA table_info(%s)", tableName)).Rows()
	defer rows.Close()
	fmt.Println("Columns:")
	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}
		fmt.Printf("- %s\n", name)
		cols = append(cols, name)
	}

	// Determine correct column name for anilist
	anilistCol := "ani_list_id"
	for _, c := range cols {
		if c == "anilist_id" {
			anilistCol = "anilist_id"
		}
	}

	checkDuplicates(db, tableName, "bangumi_id")
	checkDuplicates(db, tableName, "tmdb_id")
	checkDuplicates(db, tableName, anilistCol)
	checkDuplicates(db, tableName, "title")
}

func checkDuplicates(db *gorm.DB, tableName, col string) {
	fmt.Printf("\n--- Checking duplicates by %s ---\n", col)

	query := fmt.Sprintf("SELECT %s as val, count(*) as count FROM %s WHERE %s IS NOT NULL AND %s != '' AND %s != 0 GROUP BY %s HAVING count > 1", col, tableName, col, col, col, col)

	if col == "title" {
		query = fmt.Sprintf("SELECT %s as val, count(*) as count FROM %s WHERE %s IS NOT NULL AND %s != '' GROUP BY %s HAVING count > 1", col, tableName, col, col, col)
	}

	// Use specific struct to avoid scanning issues
	rows, err := db.Raw(query).Rows()
	if err != nil {
		log.Printf("Query error: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var val interface{}
		var cnt int
		if err := rows.Scan(&val, &cnt); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}

		fmt.Printf("- %s: %v (Count: %d)\n", col, val, cnt)
		count++

		var metas []AnimeMetadata
		listQuery := fmt.Sprintf("%s = ?", col)
		// Handle string vs int query params
		db.Table(tableName).Where(listQuery, val).Find(&metas)
		for _, m := range metas {
			fmt.Printf("  -> ID: %d, Title: %s, BGM:%d, TMDB:%d, AL:%d\n", m.ID, m.Title, m.BangumiID, m.TMDBID, m.AniListID)
		}
	}
	if count == 0 {
		fmt.Printf("No duplicates for %s.\n", col)
	}
}
