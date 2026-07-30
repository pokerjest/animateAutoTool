package api

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestLiveTorrentForLogMatchesHashBeforeTitle(t *testing.T) {
	byHash := map[string]downloader.TorrentInfo{
		"abc123": {Hash: "abc123", Name: "torrent by hash", Progress: .42},
	}
	byName := map[string]downloader.TorrentInfo{
		"torrent by title": {Name: "torrent by title", Progress: .84},
	}

	matched, ok := liveTorrentForLog(model.DownloadLog{InfoHash: "ABC123", Title: "torrent by title"}, byHash, byName)
	if !ok || matched.Progress != .42 {
		t.Fatalf("hash match = %#v, ok=%v; want hash torrent", matched, ok)
	}

	matched, ok = liveTorrentForLog(model.DownloadLog{Title: "torrent by title"}, byHash, byName)
	if !ok || matched.Progress != .84 {
		t.Fatalf("title match = %#v, ok=%v; want title torrent", matched, ok)
	}
}

func TestLiveTorrentForLogDoesNotEnrichArchivedHistory(t *testing.T) {
	torrent := downloader.TorrentInfo{Name: "Show - 01", Hash: "abc123", Progress: 0.42}
	byHash := map[string]downloader.TorrentInfo{"abc123": torrent}
	byName := map[string]downloader.TorrentInfo{"Show - 01": torrent}

	if matched, ok := liveTorrentForLog(
		model.DownloadLog{Status: "archived", InfoHash: "ABC123", Title: "Show - 01", Episode: "01"},
		byHash,
		byName,
	); ok {
		t.Fatalf("archived history matched live torrent: %+v", matched)
	}
}

func TestLiveTorrentForLogMatchesVersionedTitleAndEpisode(t *testing.T) {
	byHash := map[string]downloader.TorrentInfo{}
	byName := map[string]downloader.TorrentInfo{
		"normalized:[ani] show - 01 [1080p] [mp4]": {
			Name:     "[ANi] Show - 01 [1080P][V2][MP4]",
			Progress: .37,
		},
	}

	matched, ok := liveTorrentForLog(
		model.DownloadLog{Title: "[ANi] Show - 01 [1080P][MP4]"},
		byHash,
		byName,
	)
	if !ok || matched.Progress != .37 {
		t.Fatalf("normalized title match = %#v, ok=%v", matched, ok)
	}

	byName = map[string]downloader.TorrentInfo{}
	byEpisode := map[string][]downloader.TorrentInfo{
		"1:1": {
			{Name: "Show - 01", Progress: 42},
		},
	}
	matched, ok = liveTorrentForLogWithEpisodes(
		model.DownloadLog{Title: "[ANi] Show - 01 [1080P][MP4]", Episode: "01"},
		byHash,
		byName,
		byEpisode,
	)
	if !ok || matched.Progress != 42 {
		t.Fatalf("episode match = %#v, ok=%v", matched, ok)
	}
}

func TestNormalizeTorrentProgressSupportsRatioAndPercent(t *testing.T) {
	for _, test := range []struct {
		input float64
		want  float64
	}{
		{input: .42, want: .42},
		{input: 42, want: .42},
		{input: -1, want: 0},
		{input: 200, want: 1},
	} {
		if got := normalizeTorrentProgress(test.input); got != test.want {
			t.Fatalf("normalizeTorrentProgress(%v) = %v, want %v", test.input, got, test.want)
		}
	}
}

func TestPreferTorrentChoosesCompletedSnapshotBeforeActiveDuplicate(t *testing.T) {
	active := downloader.TorrentInfo{Name: "Show - 01", State: "downloading", Progress: .99, DownloadSpeed: 1024}
	completed := downloader.TorrentInfo{Name: "Show - 01", State: "downloading", Progress: 1}
	if got := preferTorrent(active, completed); got.Progress != 1 {
		t.Fatalf("preferTorrent chose %#v, want completed snapshot", got)
	}
}
