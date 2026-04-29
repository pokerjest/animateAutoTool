package downloader

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestQBittorrentClientLoginAddListAndRename(t *testing.T) {
	t.Parallel()

	var sawLoginCookie bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse login form: %v", err)
			}
			if got := r.Form.Get("username"); got != "admin" {
				t.Fatalf("unexpected username: %q", got)
			}
			if got := r.Form.Get("password"); got != "secret" {
				t.Fatalf("unexpected password: %q", got)
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "cookie-value"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse add form: %v", err)
			}
			if got := r.Form.Get("urls"); got != "magnet:?xt=urn:btih:test" {
				t.Fatalf("unexpected torrent url: %q", got)
			}
			if got := r.Form.Get("savepath"); got != "/downloads/anime" {
				t.Fatalf("unexpected savepath: %q", got)
			}
			if got := r.Form.Get("category"); got != "anime" {
				t.Fatalf("unexpected category: %q", got)
			}
			if cookie, err := r.Cookie("SID"); err == nil && cookie.Value == "cookie-value" {
				sawLoginCookie = true
			}
			w.WriteHeader(http.StatusOK)
		case "/api/v2/torrents/info":
			if cookie, err := r.Cookie("SID"); err != nil || cookie.Value != "cookie-value" {
				t.Fatalf("expected auth cookie on list request, err=%v cookie=%v", err, cookie)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"hash":"abc","name":"episode.mkv","state":"pausedDL","content_path":"/downloads/episode.mkv","save_path":"/downloads"}]`))
		case "/api/v2/torrents/renameFile":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse rename form: %v", err)
			}
			if got := r.Form.Get("hash"); got != "abc" {
				t.Fatalf("unexpected hash: %q", got)
			}
			if got := r.Form.Get("oldPath"); got != "old/file.mkv" {
				t.Fatalf("unexpected oldPath: %q", got)
			}
			if got := r.Form.Get("newPath"); got != "new/file.mkv" {
				t.Fatalf("unexpected newPath: %q", got)
			}
			w.WriteHeader(http.StatusOK)
		case "/api/v2/app/version":
			_, _ = w.Write([]byte("5.0.4"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewQBittorrentClient(server.URL)
	if err := client.Login("admin", "secret"); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if err := client.AddTorrent("magnet:?xt=urn:btih:test", "/downloads/anime", "anime", false); err != nil {
		t.Fatalf("add torrent failed: %v", err)
	}
	if !sawLoginCookie {
		t.Fatal("expected add torrent request to carry login cookie")
	}

	torrents, err := client.ListTorrents()
	if err != nil {
		t.Fatalf("list torrents failed: %v", err)
	}
	if len(torrents) != 1 || torrents[0].Hash != "abc" {
		t.Fatalf("unexpected torrents payload: %+v", torrents)
	}

	if err := client.RenameFile("abc", "old/file.mkv", "new/file.mkv"); err != nil {
		t.Fatalf("rename file failed: %v", err)
	}
}

func TestQBittorrentClientLoginFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/auth/login" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("Fails."))
	}))
	defer server.Close()

	client := NewQBittorrentClient(server.URL)
	err := client.Login("admin", "wrong")
	if err == nil || !strings.Contains(err.Error(), "invalid credentials") {
		t.Fatalf("expected invalid credentials error, got %v", err)
	}
}

func TestQBittorrentClientRenameFileValidation(t *testing.T) {
	t.Parallel()

	client := &QBittorrentClient{}
	cases := []struct {
		name    string
		hash    string
		oldPath string
		newPath string
	}{
		{name: "missing hash", oldPath: "old", newPath: "new"},
		{name: "missing old path", hash: "abc", newPath: "new"},
		{name: "missing new path", hash: "abc", oldPath: "old"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := client.RenameFile(tc.hash, tc.oldPath, tc.newPath); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestQBittorrentClientBaseURLNormalized(t *testing.T) {
	t.Parallel()

	client := NewQBittorrentClient("http://127.0.0.1:8080/")
	if _, err := url.Parse(client.baseURL); err != nil {
		t.Fatalf("expected valid base URL, got %v", err)
	}
	if strings.HasSuffix(client.baseURL, "/") {
		t.Fatalf("expected trailing slash to be trimmed, got %q", client.baseURL)
	}
}
