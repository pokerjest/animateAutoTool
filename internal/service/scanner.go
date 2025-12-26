package service

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm/clause"
)

type ScannerService struct {
	WorkerCount int
	BatchSize   int
}

func NewScannerService() *ScannerService {
	return &ScannerService{
		WorkerCount: 8,   // Default to 8 workers
		BatchSize:   100, // Batch database writes
	}
}

// ScanResult holds the processed data for a single file
type ScanResult struct {
	FilePath   string
	FileSize   int64
	ParsedInfo parser.ParsedInfo
	// Derived Series Info
	SeriesPath  string
	SeriesTitle string
}

func (s *ScannerService) ScanAll() error {
	log.Println("Scanner: Starting full library scan (Parallel/Offline-First)...")
	start := time.Now()

	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err != nil {
		return err
	}

	// 1. Prepare Channels
	jobs := make(chan string, 1000)
	results := make(chan ScanResult, 1000)
	var wg sync.WaitGroup

	// 2. Start Workers
	for i := 0; i < s.WorkerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.worker(jobs, results)
		}(i)
	}

	// 3. Start Aggregator (Database Writer)
	done := make(chan bool)
	go func() {
		s.aggregator(results)
		done <- true
	}()

	// 4. Start Producer (File Walker)
	go func() {
		for _, d := range dirs {
			s.walkDirectory(d.Path, jobs)
		}
		close(jobs)
	}()

	// 5. Wait
	wg.Wait()      // Wait for workers to finish processing
	close(results) // Close results channel
	<-done         // Wait for aggregator to finish writing

	// Phase 1.5: Update Stats (File Counts)
	if err := db.DB.Exec("UPDATE local_animes SET file_count = (SELECT count(*) FROM local_episodes WHERE local_episodes.local_anime_id = local_animes.id)").Error; err != nil {
		log.Printf("Scanner: Failed to update file counts: %v", err)
	}

	log.Printf("Scanner: Full scan completed in %v", time.Since(start))

	// Phase 2: Cleanup (Prune missing files)
	// TODO: Implement pruning separately to avoid complexity here

	return nil
}

func (s *ScannerService) walkDirectory(root string, jobs chan<- string) {
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("Scanner: Error accessing %s: %v", path, err)
			return nil
		}
		if d.IsDir() {
			// Skip hidden folders
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Check extension
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if IsVideoExt(ext) {
			jobs <- path
		}
		return nil
	})
	if err != nil {
		log.Printf("Scanner: Walk failed for %s: %v", root, err)
	}
}

func (s *ScannerService) worker(jobs <-chan string, results chan<- ScanResult) {
	for path := range jobs {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		// 1. Parse Filename
		parsed := parser.ParseFilename(path)

		// 2. Analyze Context (Season Folders)
		// Logic:
		// If parent folder is "Season X" or "Specials", use Grandparent as Series Root.
		// Else, use Parent as Series Root.

		dir := filepath.Dir(path)
		parentName := filepath.Base(dir)

		seriesPath := dir
		seriesTitle := parentName // Default series title is folder name

		// Check for Season Folder
		seasonRegex := regexp.MustCompile(`(?i)^(Season \d+|S\d+|Specials|OVA)$`)
		if seasonRegex.MatchString(parentName) {
			// It is a season folder!
			// Move up one level
			grandParent := filepath.Dir(dir)
			seriesPath = grandParent
			seriesTitle = filepath.Base(grandParent)

			// If parser didn't find season, try to extract from folder name
			if parsed.Season == 1 { // Only override if default
				// Extract digits from "Season 2"
				numRegex := regexp.MustCompile(`\d+`)
				nums := numRegex.FindString(parentName)
				if nums != "" {
					ver, _ := strconv.Atoi(nums) // ignore error
					if ver > 0 {
						parsed.Season = ver
					}
				} else if strings.EqualFold(parentName, "Specials") || strings.EqualFold(parentName, "OVA") {
					parsed.Season = 0
				}
			}
		}

		// Use parsed title from filename if the folder structure is flat?
		// Actually Plex prefers Folder Name for Series Title.
		// EXCEPT if we are in Root directly? We are walking recursively.
		// Let's stick to Folder Name as Series Title (Plex behavior).
		// The Parsed Title from filename is useful for "Episode Title" if present, or verifying match.

		results <- ScanResult{
			FilePath:    path,
			FileSize:    info.Size(),
			ParsedInfo:  parsed,
			SeriesPath:  seriesPath,
			SeriesTitle: seriesTitle,
		}
	}
}

func (s *ScannerService) aggregator(results <-chan ScanResult) {
	// Cache series IDs to avoid repeated DB lookups
	seriesCache := make(map[string]uint)

	// Batch buffer for Episodes
	var batch []model.LocalEpisode

	// Flush function
	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Batch Upsert
		// SQLite batch size limits apply, so 100 is safe.
		if err := db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "path"}},
			DoUpdates: clause.AssignmentColumns([]string{"file_size", "season_num", "episode_num", "title", "container"}),
		}).Create(&batch).Error; err != nil {
			log.Printf("Scanner: Batch insert failed: %v", err)
		}
		batch = make([]model.LocalEpisode, 0, s.BatchSize)
	}

	for res := range results {
		// 1. Ensure Series Exists
		seriesID, ok := seriesCache[res.SeriesPath]
		if !ok {
			// Find or Create Series
			var series model.LocalAnime
			// Check by Path
			err := db.DB.Where("path = ?", res.SeriesPath).First(&series).Error
			if err != nil {
				// Create New "Local Series"
				series = model.LocalAnime{
					Title:       res.SeriesTitle,
					Path:        res.SeriesPath,
					FileCount:   0, // will update later? or maybe just ignore count for now
					DirectoryID: 0, // We might need to find which root reg it belongs to, or make specific logic
				}
				// Try to match directory ID logic if strictly required, but for now 0 is fine or we fix it.
				// Actually we should assign DirectoryID if possible for permission/scope logic.
				// Skipping for speed now, can enrich later.

				if err := db.DB.Create(&series).Error; err != nil {
					log.Printf("Scanner: Failed to create series %s: %v", res.SeriesTitle, err)
					continue
				}
			}
			seriesID = series.ID
			seriesCache[res.SeriesPath] = seriesID
		}

		// 2. Queue Episode for Batch Insert
		ep := model.LocalEpisode{
			LocalAnimeID: seriesID,
			Path:         res.FilePath,
			Title:        res.ParsedInfo.Title, // Raw title from filename as Ep Title initially
			EpisodeNum:   res.ParsedInfo.Episode,
			SeasonNum:    res.ParsedInfo.Season,
			FileSize:     res.FileSize,
			Container:    res.ParsedInfo.Extension,
			ParsedTitle:  res.ParsedInfo.Title,
			ParsedSeason: strconv.Itoa(res.ParsedInfo.Season),
			Resolution:   res.ParsedInfo.Resolution,
			SubGroup:     res.ParsedInfo.Group,
		}

		// If Ep Title is same as Series Title (common in parsing), maybe clear it to look cleaner?
		// e.g. Show.S01E01.mkv -> Title "Show".
		// We'll keep it for now as "ParsedTitle". Agent can overwrite later.

		batch = append(batch, ep)
		if len(batch) >= s.BatchSize {
			flush()
		}
	}

	// Final flush
	flush()
}
