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
