package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

const overshadowedTitle = "From Overshadowed to Overpowered"

func TestPopulateSubscriptionLibraryStateFindsStrongNonExactTitle(t *testing.T) {
	sub := model.Subscription{
		Title:  "从后面来的神威先生",
		RSSUrl: "https://example.test/kanui-san",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:            "从后面来的神威先生 第一季",
		Path:             "/library/从后面来的神威先生 第一季",
		JellyfinSeriesID: "kanui-san-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID:   anime.ID,
		Title:          "第 1 集",
		EpisodeNum:     1,
		SeasonNum:      1,
		Path:           "/library/从后面来的神威先生 第一季/S01E01.mkv",
		JellyfinItemID: "kanui-san-episode-1",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
	})

	populateSubscriptionLibraryState(&sub)

	if sub.LocalAnimeID != anime.ID {
		t.Fatalf("expected local anime %d, got %d", anime.ID, sub.LocalAnimeID)
	}
	if sub.LibraryEpisodeCount != 1 {
		t.Fatalf("expected one library episode, got %d", sub.LibraryEpisodeCount)
	}
	if !sub.Playable {
		t.Fatal("expected matched local series to be playable from the subscription card")
	}
}

func TestFindSubscriptionLocalAnimesRejectsMetadataOnlyCollision(t *testing.T) {
	metadata := model.AnimeMetadata{
		Title:   "转生成猫的大叔",
		TitleCN: "转生成猫的大叔",
	}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "遭到流放的转生重骑士凭借游戏知识大开无双",
		RSSUrl:     "https://example.test/heavy-knight",
		MetadataID: &metadata.ID,
		Metadata:   &metadata,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:            "转生成猫的大叔",
		Path:             "/library/转生成猫的大叔 (2024)",
		MetadataID:       &metadata.ID,
		JellyfinSeriesID: "wrong-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID: anime.ID,
		Title:        "第 1 集",
		EpisodeNum:   1,
		SeasonNum:    1,
		Path:         "/library/转生成猫的大叔 (2024)/S01E01.mkv",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
		_ = db.DB.Unscoped().Delete(&metadata).Error
	})

	matches, err := findSubscriptionLocalAnimes(&sub)
	if err != nil {
		t.Fatalf("find local matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("metadata-only collision must not make another series playable: %+v", matches)
	}
}

func TestPopulateSubscriptionStatsSharesPreloadedLibraryIndexAcrossNonExactTitles(t *testing.T) {
	anime := model.LocalAnime{
		Title:            "Shared Show Season 2",
		Path:             "/library/Shared Show Season 2",
		JellyfinSeriesID: "shared-show-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episodes := []model.LocalEpisode{
		{
			LocalAnimeID:   anime.ID,
			Title:          "Shared Show - 01",
			EpisodeNum:     1,
			SeasonNum:      2,
			Path:           "/library/Shared Show Season 2/S02E01.mkv",
			JellyfinItemID: "shared-show-episode-1",
		},
		{
			LocalAnimeID: anime.ID,
			Title:        "Shared Show - 02",
			EpisodeNum:   2,
			SeasonNum:    2,
			Path:         "/library/Shared Show Season 2/S02E02.mkv",
		},
	}
	for i := range episodes {
		if err := db.DB.Create(&episodes[i]).Error; err != nil {
			t.Fatalf("create local episode %d: %v", i, err)
		}
	}
	subs := []model.Subscription{
		{Title: "Shared Show", RSSUrl: "https://example.test/shared-show"},
		{Title: "Shared Show 2026", RSSUrl: "https://example.test/shared-show-2026"},
	}
	for i := range subs {
		if err := db.DB.Create(&subs[i]).Error; err != nil {
			t.Fatalf("create subscription %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Where("subscription_id IN ?", []uint{subs[0].ID, subs[1].ID}).Delete(&model.SubscriptionResource{}).Error
		_ = db.DB.Unscoped().Delete(&subs[0]).Error
		_ = db.DB.Unscoped().Delete(&subs[1]).Error
		_ = db.DB.Unscoped().Where("local_anime_id = ?", anime.ID).Delete(&model.LocalEpisode{}).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
	})

	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		t.Fatalf("load shared library index: %v", err)
	}
	if _, ok := libraryIndex.statsByAnime[anime.ID]; !ok {
		t.Fatalf("local anime %d was not included in the shared library index", anime.ID)
	}
	for i := range subs {
		populateSubscriptionStatWithLibraryIndex(&subs[i], libraryIndex)
		if subs[i].LocalAnimeID != anime.ID {
			t.Fatalf("subscription %d matched local anime %d, want %d", i, subs[i].LocalAnimeID, anime.ID)
		}
		if subs[i].LibraryEpisodeCount != 2 {
			t.Fatalf("subscription %d episode count = %d, want 2", i, subs[i].LibraryEpisodeCount)
		}
		if !subs[i].Playable {
			t.Fatalf("subscription %d should be playable", i)
		}
	}
}
func TestFindSubscriptionLocalAnimesUsesCompletedDownloadPath(t *testing.T) {
	sub := model.Subscription{
		Title:  "正后方的神威",
		RSSUrl: "https://example.test/kanui-path-link",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:            "从后面来的神威先生",
		Path:             `C:\Media\从后面来的神威先生`,
		JellyfinSeriesID: "kanui-path-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID:   anime.ID,
		Title:          "第 1 集",
		EpisodeNum:     1,
		SeasonNum:      1,
		Path:           `C:\Media\从后面来的神威先生\S01E01.mkv`,
		JellyfinItemID: "kanui-path-episode-1",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Kanui-san - 01",
		Episode:        "01",
		Status:         "completed",
		TargetFile:     `c:\media\从后面来的神威先生\s01e01.mkv`,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&logEntry).Error
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
	})

	matches, err := findSubscriptionLocalAnimes(&sub)
	if err != nil {
		t.Fatalf("find local matches: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != anime.ID {
		t.Fatalf("path-linked local matches = %+v, want anime %d", matches, anime.ID)
	}

	populateSubscriptionLibraryState(&sub)
	if sub.LocalAnimeID != anime.ID {
		t.Fatalf("subscription local anime = %d, want %d", sub.LocalAnimeID, anime.ID)
	}
	if sub.LibraryEpisodeCount != 1 {
		t.Fatalf("subscription library episode count = %d, want 1", sub.LibraryEpisodeCount)
	}
	if !sub.Playable {
		t.Fatal("path-linked subscription should be playable")
	}
}

func TestFindSubscriptionLocalAnimesFromIndexUsesRenamedDownloadPath(t *testing.T) {
	sub := model.Subscription{
		Title:  "最强出涸皇子的暗跃帝位争夺",
		RSSUrl: "https://example.test/prince-path-link",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:            "最强废渣皇子暗中活跃于帝位之争",
		Path:             `/library/最强废渣皇子暗中活跃于帝位之争`,
		JellyfinSeriesID: "prince-path-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID:   anime.ID,
		Title:          "第 1 集",
		EpisodeNum:     1,
		SeasonNum:      1,
		Path:           `/library/最强废渣皇子暗中活跃于帝位之争/S01E01.mkv`,
		JellyfinItemID: "prince-path-episode-1",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Prince - 01",
		Episode:        "01",
		Status:         "renamed",
		TargetFile:     episode.Path,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&logEntry).Error
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
	})

	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		t.Fatalf("load subscription library index: %v", err)
	}
	matches := findSubscriptionLocalAnimesFromIndex(&sub, libraryIndex)
	if len(matches) != 1 || matches[0].ID != anime.ID {
		t.Fatalf("indexed path-linked local matches = %+v, want anime %d", matches, anime.ID)
	}
	populateSubscriptionStatWithLibraryIndex(&sub, libraryIndex)
	if sub.LocalAnimeID != anime.ID || sub.LibraryEpisodeCount != 1 || !sub.Playable {
		t.Fatalf("indexed subscription state = local_anime_id %d, episodes %d, playable %v; want %d, 1, true", sub.LocalAnimeID, sub.LibraryEpisodeCount, sub.Playable, anime.ID)
	}
}

func TestFindSubscriptionLocalAnimesIgnoresArchivedOrUnrelatedDownloadPaths(t *testing.T) {
	sub := model.Subscription{
		Title:  "完全不同的订阅标题",
		RSSUrl: "https://example.test/ignored-path-link",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title: "本地错误番剧",
		Path:  `/library/本地错误番剧`,
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID: anime.ID,
		Title:        "第 1 集",
		EpisodeNum:   1,
		SeasonNum:    1,
		Path:         `/library/本地错误番剧/S01E01.mkv`,
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	logs := []model.DownloadLog{
		{
			SubscriptionID: sub.ID,
			Status:         "archived",
			TargetFile:     episode.Path,
		},
		{
			SubscriptionID: sub.ID,
			Status:         "completed",
			TargetFile:     `/library/不存在的路径/S01E01.mkv`,
		},
	}
	for i := range logs {
		if err := db.DB.Create(&logs[i]).Error; err != nil {
			t.Fatalf("create download log %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		for i := range logs {
			_ = db.DB.Unscoped().Delete(&logs[i]).Error
		}
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
	})

	matches, err := findSubscriptionLocalAnimes(&sub)
	if err != nil {
		t.Fatalf("find local matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("archived or unrelated target path must not link local anime: %+v", matches)
	}
}

func createSubscriptionMatchMetadata(t *testing.T, title string, bangumiID, tmdbID, aniListID int) *model.AnimeMetadata {
	t.Helper()
	metadata := &model.AnimeMetadata{
		Title:     title,
		TitleCN:   title,
		BangumiID: bangumiID,
		TMDBID:    tmdbID,
		AniListID: aniListID,
	}
	if err := db.DB.Create(metadata).Error; err != nil {
		t.Fatalf("create match metadata: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(metadata).Error
	})
	return metadata
}

func createPlayableSubscriptionMatchAnime(t *testing.T, metadata *model.AnimeMetadata, title, path, seriesID string) *model.LocalAnime {
	t.Helper()
	anime := &model.LocalAnime{
		Title:            title,
		Path:             path,
		JellyfinSeriesID: seriesID,
	}
	if metadata != nil {
		anime.MetadataID = &metadata.ID
	}
	if err := db.DB.Create(anime).Error; err != nil {
		t.Fatalf("create match local anime: %v", err)
	}
	episode := &model.LocalEpisode{
		LocalAnimeID:   anime.ID,
		Title:          title + " 第 1 集",
		EpisodeNum:     1,
		SeasonNum:      1,
		Path:           path + "/S01E01.mkv",
		JellyfinItemID: seriesID + "-episode-1",
	}
	if err := db.DB.Create(episode).Error; err != nil {
		t.Fatalf("create match local episode: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(episode).Error
		_ = db.DB.Unscoped().Delete(anime).Error
	})
	return anime
}

func TestFindSubscriptionLocalAnimesUsesProviderIDsAcrossDivergentTitles(t *testing.T) {
	tests := []struct {
		name       string
		bangumiID  int
		tmdbID     int
		aniListID  int
		subTitle   string
		localTitle string
	}{
		{
			name:       "bangumi",
			bangumiID:  61001,
			subTitle:   "订阅端绯色标题",
			localTitle: "本地端星海标题",
		},
		{
			name:       "tmdb",
			tmdbID:     62001,
			subTitle:   "订阅端月影标题",
			localTitle: "本地端白昼标题",
		},
		{
			name:       "anilist",
			aniListID:  63001,
			subTitle:   "订阅端青鸟标题",
			localTitle: "本地端远山标题",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata := createSubscriptionMatchMetadata(t, "统一元数据标题", test.bangumiID, test.tmdbID, test.aniListID)
			sub := &model.Subscription{
				Title:      test.subTitle,
				RSSUrl:     "https://example.test/provider-" + test.name,
				MetadataID: &metadata.ID,
				Metadata:   metadata,
			}
			if err := db.DB.Create(sub).Error; err != nil {
				t.Fatalf("create subscription: %v", err)
			}
			t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
			anime := createPlayableSubscriptionMatchAnime(t, metadata, test.localTitle, "/library/"+test.name+"-provider", test.name+"-series")

			matches, err := findSubscriptionLocalAnimes(sub)
			if err != nil {
				t.Fatalf("find provider match: %v", err)
			}
			if len(matches) != 1 || matches[0].ID != anime.ID {
				t.Fatalf("provider match = %+v, want local anime %d", matches, anime.ID)
			}
		})
	}
}

func TestFindSubscriptionLocalAnimesFromIndexUsesProviderIDWithoutSharedTitleGram(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "索引元数据", 0, 64001, 0)
	sub := &model.Subscription{
		Title:      "甲",
		RSSUrl:     "https://example.test/provider-index-no-gram",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(t, metadata, "乙", "/library/provider-index-no-gram", "provider-index-series")

	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		t.Fatalf("load library index: %v", err)
	}
	if len(subscriptionProviderAnimeCandidates(sub, libraryIndex)) != 1 {
		t.Fatalf("provider index did not find local anime without shared title gram")
	}
	matches := findSubscriptionLocalAnimesFromIndex(sub, libraryIndex)
	if len(matches) != 1 || matches[0].ID != anime.ID {
		t.Fatalf("indexed provider match = %+v, want local anime %d", matches, anime.ID)
	}
}

func TestFindSubscriptionLocalAnimesRejectsDifferentProviderIDAndReportsConflict(t *testing.T) {
	subMetadata := createSubscriptionMatchMetadata(t, "同名元数据 A", 0, 65001, 0)
	localMetadata := createSubscriptionMatchMetadata(t, "同名元数据 B", 0, 65002, 0)
	sub := &model.Subscription{
		Title:      "同名冲突剧",
		RSSUrl:     "https://example.test/provider-id-conflict",
		MetadataID: &subMetadata.ID,
		Metadata:   subMetadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(t, localMetadata, "同名冲突剧", "/library/provider-id-conflict", "provider-conflict-series")

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find conflict match: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("different provider IDs must reject matching: %+v", matches)
	}
	var issue model.LibraryIssue
	if err := db.DB.Where("title = ? AND local_anime_id = ?", sub.Title, anime.ID).First(&issue).Error; err != nil {
		t.Fatalf("expected provider conflict issue: %v", err)
	}
	if !strings.Contains(issue.Message, "TMDB") || !strings.Contains(issue.Message, "65001") || !strings.Contains(issue.Message, "65002") {
		t.Fatalf("provider conflict issue = %q, want provider and both IDs", issue.Message)
	}
}

func TestFindSubscriptionLocalAnimesDoesNotReportUnrelatedProviderConflicts(t *testing.T) {
	subMetadata := createSubscriptionMatchMetadata(t, "目标订阅元数据", 590786, 302051, 0)
	sub := &model.Subscription{
		Title:      "目标订阅",
		RSSUrl:     "https://example.test/unrelated-provider-conflicts",
		MetadataID: &subMetadata.ID,
		Metadata:   subMetadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })

	for i, item := range []struct {
		title     string
		bangumiID int
		tmdbID    int
	}{
		{title: "2.5次元的诱惑", bangumiID: 410346, tmdbID: 216074},
		{title: "Ranma 1/2", bangumiID: 2789, tmdbID: 259140},
		{title: "杜鹃的婚约", bangumiID: 327606, tmdbID: 78483},
	} {
		metadata := createSubscriptionMatchMetadata(t, item.title+" 元数据", item.bangumiID, item.tmdbID, 0)
		createPlayableSubscriptionMatchAnime(
			t,
			metadata,
			item.title,
			"/library/unrelated-provider-conflict-"+fmt.Sprint(i),
			"unrelated-provider-conflict-"+fmt.Sprint(i),
		)
	}

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find unrelated provider matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("unrelated provider candidates must not match: %+v", matches)
	}

	var issues []model.LibraryIssue
	if err := db.DB.Where("title = ?", sub.Title).Find(&issues).Error; err != nil {
		t.Fatalf("load unrelated provider issues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unrelated provider candidates produced false conflicts: %+v", issues)
	}
}

func TestHealthLibraryIssuesResolvesHistoricalFalseProviderConflict(t *testing.T) {
	subMetadata := createSubscriptionMatchMetadata(t, "无职转生 第三季元数据", 277554, 94664, 0)
	localMetadata := createSubscriptionMatchMetadata(t, "落第贤者元数据", 630163, 314554, 0)
	// Reproduce historical metadata contamination: the subscription metadata
	// contains a local title alias even though provider IDs and metadata differ.
	subMetadata.TitleEN = overshadowedTitle
	subMetadata.TMDBTitle = overshadowedTitle
	sub := &model.Subscription{
		Title:      "无职转生 第三季 ～到了异世界就拿出真本事～",
		Season:     "Season 3",
		RSSUrl:     "https://example.test/health-stale-season-conflict",
		MetadataID: &subMetadata.ID,
		Metadata:   subMetadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(t, localMetadata, "From Overshadowed to Overpowered Season 1", "/library/health-stale-season-conflict", "health-stale-season-series")
	localAnimeID := anime.ID
	issue := &model.LibraryIssue{
		IssueKey:        fmt.Sprintf("subscription-provider-conflict:%d:%d:season", sub.ID, anime.ID),
		IssueType:       "parse",
		Status:          "open",
		Title:           sub.Title,
		LocalAnimeID:    &localAnimeID,
		Message:         "historical false season conflict",
		OccurrenceCount: 1,
	}
	if err := db.DB.Create(issue).Error; err != nil {
		t.Fatalf("create historical issue: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(issue).Error })

	issues, err := healthLibraryIssues()
	if err != nil {
		t.Fatalf("load health issues: %v", err)
	}
	for _, healthIssue := range issues {
		if healthIssue.IssueKey == issue.IssueKey {
			t.Fatalf("health snapshot retained stale conflict: %+v", healthIssue)
		}
	}
	var updated model.LibraryIssue
	if err := db.DB.First(&updated, issue.ID).Error; err != nil {
		t.Fatalf("reload historical issue: %v", err)
	}
	if updated.Status != "resolved" {
		t.Fatalf("historical false conflict status = %q, want resolved", updated.Status)
	}
}

func TestSharedContaminatedMetadataDoesNotReopenSeasonConflict(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "无职转生 第三季元数据", 277554, 94664, 0)
	metadata.TitleEN = overshadowedTitle
	metadata.TMDBTitle = overshadowedTitle
	if err := db.DB.Save(metadata).Error; err != nil {
		t.Fatalf("persist contaminated metadata aliases: %v", err)
	}
	sub := &model.Subscription{
		Title:      "无职转生 第三季 ～到了异世界就拿出真本事～",
		Season:     "Season 3",
		RSSUrl:     "https://example.test/shared-contaminated-season",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(
		t,
		metadata,
		"From Overshadowed to Overpowered Season 1",
		"/library/shared-contaminated-season",
		"shared-contaminated-series",
	)
	if err := db.DB.Model(anime).Update("season", 1).Error; err != nil {
		t.Fatalf("set local season: %v", err)
	}
	anime.Season = 1
	anime.Metadata = metadata

	identity := service.EvaluateSubscriptionLocalIdentity(sub, anime)
	if !identity.Conflict || identity.Provider != subscriptionProviderSeason {
		t.Fatalf("identity = %+v, want raw season conflict before evidence filtering", identity)
	}
	if shouldReportSubscriptionLocalIdentityConflict(sub, anime, identity) {
		t.Fatal("shared contaminated metadata must not independently prove a season conflict")
	}

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find local matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("contaminated metadata must not make unrelated local anime playable: %+v", matches)
	}
	var issues int64
	if err := db.DB.Model(&model.LibraryIssue{}).
		Where("status = ? AND issue_key = ?", service.LibraryIssueStatusOpen,
			fmt.Sprintf("subscription-provider-conflict:%d:%d:season", sub.ID, anime.ID)).
		Count(&issues).Error; err != nil {
		t.Fatalf("count season conflicts: %v", err)
	}
	if issues != 0 {
		t.Fatalf("shared contaminated metadata reopened %d season conflicts", issues)
	}
}

func TestDistinctProviderMetadataStillReportsSeasonConflict(t *testing.T) {
	subMetadata := createSubscriptionMatchMetadata(t, "同一作品订阅元数据", 0, 67101, 0)
	localMetadata := createSubscriptionMatchMetadata(t, "同一作品本地元数据", 0, 67101, 0)
	sub := &model.Subscription{
		Title:      "不同语言订阅标题 第 2 季",
		Season:     "Season 2",
		RSSUrl:     "https://example.test/distinct-provider-season",
		MetadataID: &subMetadata.ID,
		Metadata:   subMetadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(
		t,
		localMetadata,
		"Different Localized Title Season 3",
		"/library/distinct-provider-season",
		"distinct-provider-series",
	)
	if err := db.DB.Model(anime).Update("season", 3).Error; err != nil {
		t.Fatalf("set local season: %v", err)
	}
	anime.Season = 3
	anime.Metadata = localMetadata

	identity := service.EvaluateSubscriptionLocalIdentity(sub, anime)
	if !identity.Conflict || !identity.ExternalMatch || identity.Provider != subscriptionProviderSeason {
		t.Fatalf("identity = %+v, want provider-backed season conflict", identity)
	}
	if !shouldReportSubscriptionLocalIdentityConflict(sub, anime, identity) {
		t.Fatal("distinct metadata rows with the same provider ID must keep reporting season conflicts")
	}
}

func TestLoadSubscriptionLocalMatchIndexResolvesHistoricalFalseProviderConflict(t *testing.T) {
	subMetadata := createSubscriptionMatchMetadata(t, "目标订阅元数据", 590786, 302051, 0)
	localMetadata := createSubscriptionMatchMetadata(t, "2.5次元的诱惑 元数据", 410346, 216074, 0)
	sub := &model.Subscription{
		Title:      "目标订阅",
		RSSUrl:     "https://example.test/stale-provider-conflict",
		MetadataID: &subMetadata.ID,
		Metadata:   subMetadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(
		t,
		localMetadata,
		"2.5次元的诱惑",
		"/library/stale-provider-conflict",
		"stale-provider-conflict-series",
	)
	localAnimeID := anime.ID
	issue := &model.LibraryIssue{
		IssueKey:        fmt.Sprintf("subscription-provider-conflict:%d:%d:BANGUMI", sub.ID, anime.ID),
		IssueType:       "scrape",
		Status:          "open",
		Title:           sub.Title,
		LocalAnimeID:    &localAnimeID,
		Message:         "historical false positive",
		OccurrenceCount: 1,
	}
	if err := db.DB.Create(issue).Error; err != nil {
		t.Fatalf("create historical issue: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(issue).Error })

	if _, err := loadSubscriptionLocalMatchIndex(); err != nil {
		t.Fatalf("load subscription match index: %v", err)
	}
	var updated model.LibraryIssue
	if err := db.DB.First(&updated, issue.ID).Error; err != nil {
		t.Fatalf("reload historical issue: %v", err)
	}
	if updated.Status != "resolved" {
		t.Fatalf("historical false conflict status = %q, want resolved", updated.Status)
	}
}

func TestFindSubscriptionLocalAnimesRejectsCrossProviderConflict(t *testing.T) {
	subMetadata := createSubscriptionMatchMetadata(t, "交叉冲突元数据 A", 0, 66001, 66002)
	localMetadata := createSubscriptionMatchMetadata(t, "交叉冲突元数据 B", 0, 66001, 66999)
	sub := &model.Subscription{
		Title:      "交叉冲突订阅标题",
		RSSUrl:     "https://example.test/cross-provider-conflict",
		MetadataID: &subMetadata.ID,
		Metadata:   subMetadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(t, localMetadata, "交叉冲突本地标题", "/library/cross-provider-conflict", "cross-provider-series")

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find cross-provider conflict: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("cross-provider conflict must reject matching: %+v", matches)
	}
	var issue model.LibraryIssue
	if err := db.DB.Where("title = ? AND local_anime_id = ?", sub.Title, anime.ID).First(&issue).Error; err != nil {
		t.Fatalf("expected cross-provider conflict issue: %v", err)
	}
	if !strings.Contains(issue.Message, "ANILIST") || !strings.Contains(issue.Message, "66002") || !strings.Contains(issue.Message, "66999") {
		t.Fatalf("cross-provider issue = %q, want provider and both IDs", issue.Message)
	}
}

func TestFindSubscriptionLocalAnimesRejectsExplicitSeasonConflict(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "季度冲突元数据", 0, 67001, 0)
	sub := &model.Subscription{
		Title:      "季度冲突番剧 Season 2",
		Season:     "Season 2",
		RSSUrl:     "https://example.test/season-conflict",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(t, metadata, "季度冲突番剧 Season 3", "/library/season-conflict", "season-conflict-series")
	if err := db.DB.Model(anime).Update("season", 3).Error; err != nil {
		t.Fatalf("set local season: %v", err)
	}

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find season conflict: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("explicit season conflict must reject matching: %+v", matches)
	}
	var issue model.LibraryIssue
	if err := db.DB.Where("title = ? AND local_anime_id = ?", sub.Title, anime.ID).First(&issue).Error; err != nil {
		t.Fatalf("expected season conflict issue: %v", err)
	}
	if !strings.Contains(issue.Message, "季度冲突") || !strings.Contains(issue.Message, "2") || !strings.Contains(issue.Message, "3") {
		t.Fatalf("season issue = %q, want both seasons", issue.Message)
	}
}

func TestFindSubscriptionLocalAnimesIgnoresUnrelatedPathLinkedSeasonConflict(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "无职转生 第三季元数据", 277554, 94664, 0)
	localMetadata := createSubscriptionMatchMetadata(t, "落第贤者元数据", 630163, 314554, 0)
	sub := &model.Subscription{
		Title:      "目标订阅 第三季",
		Season:     "Season 3",
		RSSUrl:     "https://example.test/unrelated-path-season-conflict",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	anime := createPlayableSubscriptionMatchAnime(t, localMetadata, "From Overshadowed to Overpowered Season 1", "/library/unrelated-path-season-conflict", "unrelated-path-season-conflict-series")
	if err := db.DB.Exec("UPDATE local_animes SET season = ?, metadata_id = NULL WHERE id = ?", 1, anime.ID).Error; err != nil {
		t.Fatalf("make local anime unrelated: %v", err)
	}
	logEntry := &model.DownloadLog{
		SubscriptionID: sub.ID,
		Status:         "completed",
		TargetFile:     anime.Path + "/S01E01.mkv",
	}
	if err := db.DB.Create(logEntry).Error; err != nil {
		t.Fatalf("create path link: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(logEntry).Error })

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find unrelated path-linked season conflict: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("unrelated path-linked candidate must not match: %+v", matches)
	}
	var issues []model.LibraryIssue
	if err := db.DB.Where("title = ?", sub.Title).Find(&issues).Error; err != nil {
		t.Fatalf("load path-linked issues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unrelated path-linked candidate produced false conflicts: %+v", issues)
	}
}

func TestFindSubscriptionLocalAnimesDisambiguatesDuplicateProviderIDByPath(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "重复身份元数据", 0, 68001, 0)
	sub := &model.Subscription{
		Title:      "重复身份订阅标题",
		RSSUrl:     "https://example.test/duplicate-provider-path",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	first := createPlayableSubscriptionMatchAnime(t, metadata, "重复身份本地甲", "/library/duplicate-provider-a", "duplicate-a")
	second := createPlayableSubscriptionMatchAnime(t, metadata, "重复身份本地乙", "/library/duplicate-provider-b", "duplicate-b")
	logEntry := &model.DownloadLog{
		SubscriptionID: sub.ID,
		Status:         "completed",
		TargetFile:     first.Path + "/S01E01.mkv",
	}
	if err := db.DB.Create(logEntry).Error; err != nil {
		t.Fatalf("create path link: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(logEntry).Error })

	matches, err := findSubscriptionLocalAnimes(sub)
	if err != nil {
		t.Fatalf("find duplicate provider match: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != first.ID || matches[0].ID == second.ID {
		t.Fatalf("path disambiguation = %+v, want local anime %d only", matches, first.ID)
	}
}

func TestFindSubscriptionLocalAnimesFromIndexRejectsUnresolvedDuplicateProviderID(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "无路径重复身份元数据", 0, 69001, 0)
	sub := &model.Subscription{
		Title:      "无路径重复身份订阅标题",
		RSSUrl:     "https://example.test/duplicate-provider-no-path",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	first := createPlayableSubscriptionMatchAnime(t, metadata, "无路径重复身份本地甲", "/library/duplicate-no-path-a", "duplicate-no-path-a")
	second := createPlayableSubscriptionMatchAnime(t, metadata, "无路径重复身份本地乙", "/library/duplicate-no-path-b", "duplicate-no-path-b")

	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		t.Fatalf("load library index: %v", err)
	}
	matches := findSubscriptionLocalAnimesFromIndex(sub, libraryIndex)
	if len(matches) != 0 {
		t.Fatalf("unresolved duplicate provider identity must not choose a local anime: %+v", matches)
	}
	var issues []model.LibraryIssue
	if err := db.DB.Where("title = ? AND local_anime_id IN ?", sub.Title, []uint{first.ID, second.ID}).Find(&issues).Error; err != nil {
		t.Fatalf("load duplicate identity issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("duplicate identity issues = %d, want 2: %+v", len(issues), issues)
	}
	for _, issue := range issues {
		if !strings.Contains(issue.Message, "tmdb:69001") || !strings.Contains(issue.Message, "无路径重复身份本地") {
			t.Fatalf("duplicate identity issue = %q, want key and titles", issue.Message)
		}
	}
}

func TestFindSubscriptionLocalAnimesFromIndexPrefersUniqueExactTitleAndResolvesDuplicateIssues(t *testing.T) {
	metadata := createSubscriptionMatchMetadata(t, "透明之夜元数据", 607340, 305814, 202269)
	sub := &model.Subscription{
		Title:      "与奔驰于透明之夜的你，谈一场看不见的恋爱。",
		RSSUrl:     "https://example.test/duplicate-provider-exact-title",
		MetadataID: &metadata.ID,
		Metadata:   metadata,
	}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(sub).Error })
	exact := createPlayableSubscriptionMatchAnime(
		t,
		metadata,
		"与奔驰于透明之夜的你，谈一场看不见的恋爱。",
		"/library/duplicate-provider-exact-title",
		"duplicate-provider-exact-title",
	)
	traditional := createPlayableSubscriptionMatchAnime(
		t,
		metadata,
		"與奔馳於透明之夜的你，談一場看不見的戀愛。",
		"/library/duplicate-provider-traditional-title",
		"duplicate-provider-traditional-title",
	)
	for _, anime := range []*model.LocalAnime{exact, traditional} {
		localAnimeID := anime.ID
		issue := &model.LibraryIssue{
			IssueKey:        fmt.Sprintf("subscription-provider-duplicate:%d:%d", sub.ID, anime.ID),
			IssueType:       service.LibraryIssueTypeScrape,
			Status:          service.LibraryIssueStatusOpen,
			Title:           sub.Title,
			LocalAnimeID:    &localAnimeID,
			Message:         "historical duplicate issue",
			OccurrenceCount: 1,
		}
		if err := db.DB.Create(issue).Error; err != nil {
			t.Fatalf("create historical duplicate issue: %v", err)
		}
		t.Cleanup(func() { _ = db.DB.Unscoped().Delete(issue).Error })
	}

	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		t.Fatalf("load library index: %v", err)
	}
	matches := findSubscriptionLocalAnimesFromIndex(sub, libraryIndex)
	if len(matches) != 1 || matches[0].ID != exact.ID {
		t.Fatalf("exact title disambiguation = %+v, want local anime %d", matches, exact.ID)
	}

	var openIssues int64
	if err := db.DB.Model(&model.LibraryIssue{}).
		Where("status = ? AND issue_key LIKE ?", service.LibraryIssueStatusOpen, fmt.Sprintf("subscription-provider-duplicate:%d:%%", sub.ID)).
		Count(&openIssues).Error; err != nil {
		t.Fatalf("count open duplicate issues: %v", err)
	}
	if openIssues != 0 {
		t.Fatalf("open duplicate issues = %d, want 0 after exact disambiguation", openIssues)
	}
}
