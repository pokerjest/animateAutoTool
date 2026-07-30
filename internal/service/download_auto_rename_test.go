package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

type fakeTorrentRenameSource struct {
	torrents  []downloader.TorrentInfo
	renamed   [][3]string
	locations [][2]string
	err       error
}

func (f *fakeTorrentRenameSource) ListTorrents() ([]downloader.TorrentInfo, error) {
	return f.torrents, f.err
}

func (f *fakeTorrentRenameSource) RenameFile(hash, oldPath, newPath string) error {
	f.renamed = append(f.renamed, [3]string{hash, oldPath, newPath})
	return f.err
}

func (f *fakeTorrentRenameSource) SetLocation(hash, location string) error {
	f.locations = append(f.locations, [2]string{hash, location})
	return f.err
}

func TestAutoRenameCompletedDownloadsUsesDefaultTemplate(t *testing.T) {
	withServiceTestDB(t)
	if err := store.NewConfigStore(db.DB).SetMany(map[string]string{model.ConfigKeyBaseDir: "/downloads"}); err != nil {
		t.Fatalf("set base dir: %v", err)
	}

	sub := model.Subscription{Title: "示例番剧", RSSUrl: "https://example.com/rss"}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	entry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Example - 03 [1080p]",
		Episode:        "03",
		SeasonVal:      "S01",
		Status:         downloadLogStatusCompleted,
		InfoHash:       "ABC",
		TargetFile:     `/downloads/示例番剧/[Group] Example - 03 [1080p].mkv`,
	}
	if err := db.DB.Create(&entry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	source := &fakeTorrentRenameSource{torrents: []downloader.TorrentInfo{{
		Hash:        "abc",
		Name:        entry.Title,
		State:       "uploading",
		SavePath:    `/downloads/示例番剧`,
		ContentPath: `/downloads/示例番剧/[Group] Example - 03 [1080p].mkv`,
	}}}
	result, err := AutoRenameCompletedDownloads(source)
	if err != nil {
		t.Fatalf("AutoRenameCompletedDownloads: %v", err)
	}
	if result.Renamed != 1 || result.Failed != 0 || len(source.renamed) != 1 {
		t.Fatalf("unexpected result=%#v calls=%#v", result, source.renamed)
	}
	wantRelative := "示例番剧 - S01E03.mkv"
	if source.renamed[0][1] != "[Group] Example - 03 [1080p].mkv" || source.renamed[0][2] != wantRelative {
		t.Fatalf("unexpected qB rename call %#v", source.renamed[0])
	}
	if len(source.locations) != 1 || source.locations[0][1] != "/downloads/示例番剧/Season 01" {
		t.Fatalf("unexpected qB location call %#v", source.locations)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, entry.ID).Error; err != nil {
		t.Fatalf("reload download log: %v", err)
	}
	if updated.Status != downloadLogStatusRenamed {
		t.Fatalf("status = %q", updated.Status)
	}
	if updated.TargetFile != "/downloads/示例番剧/Season 01/"+wantRelative {
		t.Fatalf("target_file = %q", updated.TargetFile)
	}
}

func TestAutoRenameCompletedDownloadsDefaultsOnAndSupportsWindowsPaths(t *testing.T) {
	withServiceTestDB(t)
	if err := store.NewConfigStore(db.DB).SetMany(map[string]string{model.ConfigKeyBaseDir: `D:\Anime`}); err != nil {
		t.Fatalf("set base dir: %v", err)
	}

	sub := model.Subscription{Title: "Windows Show", RSSUrl: "https://example.com/windows"}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	entry := model.DownloadLog{SubscriptionID: sub.ID, Title: "release", Episode: "8", Status: downloadLogStatusCompleted, InfoHash: "win"}
	if err := db.DB.Create(&entry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	source := &fakeTorrentRenameSource{torrents: []downloader.TorrentInfo{{
		Hash: "WIN", Name: "release", State: "stalledUP",
		SavePath: `D:\Anime\Windows Show`, ContentPath: `D:\Anime\Windows Show\release.mp4`,
	}}}
	result, err := AutoRenameCompletedDownloads(source)
	if err != nil {
		t.Fatalf("AutoRenameCompletedDownloads: %v", err)
	}
	if result.Renamed != 1 || len(result.Targets) != 1 {
		t.Fatalf("unexpected result %#v", result)
	}
	if result.Targets[0] != `D:\Anime\Windows Show\Season 01\Windows Show - S01E08.mp4` {
		t.Fatalf("unexpected Windows target %q", result.Targets[0])
	}
}

func TestAutoRenameCompletedDownloadsPreservesCorrectedReleaseVersion(t *testing.T) {
	withServiceTestDB(t)
	if err := store.NewConfigStore(db.DB).SetMany(map[string]string{model.ConfigKeyBaseDir: "/downloads"}); err != nil {
		t.Fatalf("set base dir: %v", err)
	}

	sub := model.Subscription{Title: "Corrected Show", RSSUrl: "https://example.com/corrected"}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	entry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[ANi] Corrected Show - 01 [1080P][V2]",
		Episode:        "01",
		SeasonVal:      "S01",
		Status:         downloadLogStatusCompleted,
		InfoHash:       "corrected",
		TargetFile:     "/downloads/Corrected Show/[ANi] Corrected Show - 01 [1080P][V2].mp4",
	}
	if err := db.DB.Create(&entry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}
	source := &fakeTorrentRenameSource{torrents: []downloader.TorrentInfo{{
		Hash:        "corrected",
		Name:        entry.Title,
		State:       "uploading",
		SavePath:    "/downloads/Corrected Show",
		ContentPath: entry.TargetFile,
	}}}

	result, err := AutoRenameCompletedDownloads(source)
	if err != nil {
		t.Fatalf("AutoRenameCompletedDownloads: %v", err)
	}
	if result.Renamed != 1 || len(source.renamed) != 1 {
		t.Fatalf("unexpected result=%#v calls=%#v", result, source.renamed)
	}
	if got, want := source.renamed[0][2], "Corrected Show - S01E01 v2.mp4"; got != want {
		t.Fatalf("corrected release target = %q, want %q", got, want)
	}
}

func TestAutoRenameCompletedDownloadsCanBeDisabledAndSkipsMultiFileRoot(t *testing.T) {
	withServiceTestDB(t)
	if err := store.NewConfigStore(db.DB).SetMany(map[string]string{model.ConfigKeyAutoRenameEnabled: "false"}); err != nil {
		t.Fatalf("disable auto rename: %v", err)
	}
	source := &fakeTorrentRenameSource{torrents: []downloader.TorrentInfo{{Hash: "one"}}}
	result, err := AutoRenameCompletedDownloads(source)
	if err != nil {
		t.Fatalf("AutoRenameCompletedDownloads: %v", err)
	}
	if result.Renamed != 0 || len(source.renamed) != 0 {
		t.Fatalf("disabled rename changed torrents: %#v", result)
	}
}

func TestMergeCompletedTargetsReplacesOldPathsAndAddsRecoveredRename(t *testing.T) {
	got := MergeCompletedTargets([]string{"/old/a.mkv", "/keep/b.mkv"}, AutoRenameResult{
		Targets:      []string{"/new/a.mkv", "/new/c.mkv"},
		Replacements: map[string]string{"/old/a.mkv": "/new/a.mkv"},
	})
	want := []string{"/new/a.mkv", "/keep/b.mkv", "/new/c.mkv"}
	if len(got) != len(want) {
		t.Fatalf("MergeCompletedTargets = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MergeCompletedTargets[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMediaNamingGroupsSeasonSpecificTitlesUnderOneSeries(t *testing.T) {
	withServiceTestDB(t)

	first := model.Subscription{Title: "Example", Metadata: &model.AnimeMetadata{TitleCN: "示例番剧"}}
	second := model.Subscription{Title: "Example Season 2", Metadata: &model.AnimeMetadata{TitleCN: "示例番剧 第二季"}}
	firstDir, err := mediaTargetDirectory(&first, "S01", "/media")
	if err != nil {
		t.Fatalf("first target: %v", err)
	}
	secondDir, err := mediaTargetDirectory(&second, "S02", "/media")
	if err != nil {
		t.Fatalf("second target: %v", err)
	}
	if firstDir != "/media/示例番剧/Season 01" {
		t.Fatalf("unexpected first season directory %q", firstDir)
	}
	if secondDir != "/media/示例番剧/Season 02" {
		t.Fatalf("unexpected second season directory %q", secondDir)
	}
}
