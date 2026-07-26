package parser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
)

const mikanTestSubgroupANi = "ANi"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func rewriteMikanTransport(target string) http.RoundTripper {
	base := http.DefaultTransport
	serverURL, _ := http.NewRequest(http.MethodGet, target, nil)
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "mikanani.me" {
			r.URL.Scheme = serverURL.URL.Scheme
			r.URL.Host = serverURL.URL.Host
		}
		return base.RoundTrip(r)
	})
}

func TestMikanParseRSS(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
			<rss xmlns="https://mikanani.me/0.1/">
				<channel>
					<item>
						<title>[ANi] 测试番剧 - 03 [1080P]</title>
						<enclosure url="https://example.com/test.torrent"></enclosure>
						<torrent>
							<contentLength>2147483648</contentLength>
							<pubDate>Tue, 29 Apr 2025 20:40:03 +0800</pubDate>
						</torrent>
					</item>
				</channel>
			</rss>`))
	}))
	defer server.Close()

	parser := NewMikanParser()
	episodes, err := parser.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse rss failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	if got := episodes[0].SubGroup; got != mikanTestSubgroupANi {
		t.Fatalf("unexpected subgroup: %q", got)
	}
	if got := episodes[0].EpisodeNum; got != "03" {
		t.Fatalf("unexpected episode num: %q", got)
	}
	if got := episodes[0].Magnet; got != "https://example.com/test.torrent" {
		t.Fatalf("unexpected magnet fallback: %q", got)
	}
	if got := episodes[0].Size; !strings.Contains(got, "GB") {
		t.Fatalf("expected formatted size in GB, got %q", got)
	}
}

func TestMikanSearchAndSubgroups(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/Home/Search"):
			_, _ = w.Write([]byte(`
				<a href="/Home/Bangumi/3141"><span data-src="/images/poster.jpg"></span><div class="an-text" title="测试番剧"></div></a>
			`))
		case r.URL.Path == "/Home/Bangumi/3141":
			_, _ = w.Write([]byte(`
				<div class="subgroup-text" id="583"><a style="color:#333">ANi</a></div>
				<div class="subgroup-text" id="382"><a style="color:#333">LoliHouse</a></div>
			`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	parser := &MikanParser{client: resty.New()}
	parser.client.SetTransport(rewriteMikanTransport(server.URL))

	results, err := parser.Search("测试")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 || results[0].MikanID != "3141" {
		t.Fatalf("unexpected search results: %+v", results)
	}
	if got := results[0].Image; got != "https://mikanani.me/images/poster.jpg" {
		t.Fatalf("unexpected image url: %q", got)
	}

	subgroups, err := parser.GetSubgroups("3141")
	if err != nil {
		t.Fatalf("get subgroups failed: %v", err)
	}
	if len(subgroups) != 3 {
		t.Fatalf("expected 3 subgroup options including all, got %d", len(subgroups))
	}
	if subgroups[1].Name != mikanTestSubgroupANi || subgroups[2].ID != "382" {
		t.Fatalf("unexpected subgroups: %+v", subgroups)
	}
}

func TestMikanResolveBangumiSubjectUsesVerifiedDetailLinkAndCache(t *testing.T) {
	t.Parallel()

	var searchRequests atomic.Int32
	var detailRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Home/Search":
			searchRequests.Add(1)
			_, _ = w.Write([]byte(`
				<a href="/Home/Bangumi/7393141"><span data-src="/images/right.jpg"></span><div class="an-text" title="精确匹配"></div></a>
				<a href="/Home/Bangumi/7393999"><span data-src="/images/wrong.jpg"></span><div class="an-text" title="同名但错误"></div></a>
			`))
		case "/Home/Bangumi/7393141":
			detailRequests.Add(1)
			_, _ = w.Write([]byte(`<a href="https://bgm.tv/subject/7598058">Bangumi</a>`))
		case "/Home/Bangumi/7393999":
			detailRequests.Add(1)
			_, _ = w.Write([]byte(`<a href="https://bgm.tv/subject/7000001">Bangumi</a>`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mikan := &MikanParser{client: resty.New()}
	mikan.client.SetTransport(rewriteMikanTransport(server.URL))

	results, err := mikan.ResolveBangumiSubjectContext(context.Background(), "7598058", "测试番剧")
	if err != nil {
		t.Fatalf("resolve Bangumi subject failed: %v", err)
	}
	if len(results) != 1 || results[0].MikanID != "7393141" {
		t.Fatalf("unexpected exact results: %+v", results)
	}
	if results[0].BangumiSubjectID != "7598058" {
		t.Fatalf("unexpected Bangumi subject ID: %q", results[0].BangumiSubjectID)
	}

	cached, err := mikan.ResolveBangumiSubjectContext(context.Background(), "7598058", "另一个译名")
	if err != nil || len(cached) != 1 {
		t.Fatalf("read cached exact result: results=%+v err=%v", cached, err)
	}
	if searchRequests.Load() != 1 || detailRequests.Load() != 2 {
		t.Fatalf("expected cached second resolve, search=%d detail=%d", searchRequests.Load(), detailRequests.Load())
	}
}

func TestMikanFetchTorrent(t *testing.T) {
	t.Parallel()

	torrentData := []byte("d4:infod4:name4:testee")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="episode-01.torrent"`)
		_, _ = w.Write(torrentData)
	}))
	defer server.Close()

	client := NewMikanParser()
	filename, data, err := client.FetchTorrentContext(context.Background(), server.URL+"/download/ignored")
	if err != nil {
		t.Fatalf("fetch torrent: %v", err)
	}
	if filename != "episode-01.torrent" {
		t.Fatalf("unexpected filename: %q", filename)
	}
	if string(data) != string(torrentData) {
		t.Fatalf("unexpected torrent data: %q", data)
	}
}

func TestMikanFetchTorrentRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "html instead of torrent",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("<html>access denied</html>"))
			},
			want: "not a bencoded torrent",
		},
		{
			name: "empty body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			want: "response is empty",
		},
		{
			name: "oversized body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(maxTorrentFileSize+1))
				w.WriteHeader(http.StatusOK)
			},
			want: "exceeds 10 MiB limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewMikanParser()
			_, _, err := client.FetchTorrentContext(context.Background(), server.URL+"/episode.torrent")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestMikanIDFromRSSURLOnlyAcceptsOfficialBangumiFeeds(t *testing.T) {
	tests := []struct {
		name string
		url  string
		id   string
		ok   bool
	}{
		{name: "base feed", url: "https://mikanani.me/RSS/Bangumi?bangumiId=3141", id: "3141", ok: true},
		{name: "subgroup feed", url: "https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583", id: "3141", ok: true},
		{name: "official www host", url: "https://www.mikanani.me/rss/bangumi/?bangumiId=9", id: "9", ok: true},
		{name: "lookalike host", url: "https://mikanani.me.example/RSS/Bangumi?bangumiId=3141", ok: false},
		{name: "wrong path", url: "https://mikanani.me/Home/Bangumi/3141?bangumiId=3141", ok: false},
		{name: "non numeric", url: "https://mikanani.me/RSS/Bangumi?bangumiId=abc", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := MikanIDFromRSSURL(tt.url)
			if ok != tt.ok || id != tt.id {
				t.Fatalf("MikanIDFromRSSURL(%q) = %q, %v; want %q, %v", tt.url, id, ok, tt.id, tt.ok)
			}
		})
	}
}
