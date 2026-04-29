package bangumi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func rewriteBangumiTransport(target string) http.RoundTripper {
	base := http.DefaultTransport
	serverURL, _ := http.NewRequest(http.MethodGet, target, nil)
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "api.bgm.tv", "bgm.tv":
			r.URL.Scheme = serverURL.URL.Scheme
			r.URL.Host = serverURL.URL.Host
		}
		return base.RoundTrip(r)
	})
}

func TestSearchSubjectsNormalizesImageURLs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/subject/test" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"list":[{"id":1,"name":"Test","name_cn":"测试","images":{"large":"//lain.bgm.tv/pic/cover/l/test.jpg"}}]}`))
	}))
	defer server.Close()

	client := NewClient("", "", "")
	client.client.SetTransport(rewriteBangumiTransport(server.URL))

	results, err := client.SearchSubjects("test")
	if err != nil {
		t.Fatalf("search subjects failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if got := results[0].Images.Large; got != "https://lain.bgm.tv/pic/cover/l/test.jpg" {
		t.Fatalf("unexpected image url: %q", got)
	}
}

func TestGetSubjectFallsBackToHTMLScrape(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/42":
			http.Error(w, "upstream down", http.StatusBadGateway)
		case "/subject/42":
			if r.URL.RawQuery == "responseGroup=medium" {
				http.Error(w, "legacy down", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`
				<html>
				<head><title>测试条目 | Bangumi 番组计划</title></head>
				<body>
					<div id="subject_summary" class="subject_summary" property="v:summary">简介<br />第二行</div>
					<span class="number" property="v:average">7.8</span>
					<img src="//lain.bgm.tv/pic/cover/c/test.jpg" />
				</body>
				</html>
			`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient("", "", "")
	client.client.SetTransport(rewriteBangumiTransport(server.URL))

	subject, err := client.GetSubject(42)
	if err != nil {
		t.Fatalf("get subject failed: %v", err)
	}
	if subject.NameCN != "测试条目" {
		t.Fatalf("unexpected subject title: %q", subject.NameCN)
	}
	if subject.Summary != "简介\n第二行" {
		t.Fatalf("unexpected summary: %q", subject.Summary)
	}
	if subject.Rating.Score != 7.8 {
		t.Fatalf("unexpected rating: %v", subject.Rating.Score)
	}
	if subject.Images.Large != "https://lain.bgm.tv/pic/cover/l/test.jpg" {
		t.Fatalf("unexpected large image: %q", subject.Images.Large)
	}
}
