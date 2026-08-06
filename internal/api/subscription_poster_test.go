package api

import (
	"context"
	"errors"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestTrustedMikanPosterSourceOnlyAllowsOfficialHTTPS(t *testing.T) {
	require.Equal(t, "https://mikanani.me/images/poster.jpg", trustedMikanPosterSource("https://mikanani.me/images/poster.jpg"))
	require.Equal(t, "https://www.mikanani.me/images/poster.jpg", trustedMikanPosterSource("https://www.mikanani.me/images/poster.jpg"))
	require.Empty(t, trustedMikanPosterSource("http://mikanani.me/images/poster.jpg"))
	require.Empty(t, trustedMikanPosterSource("https://example.com/poster.jpg"))
}

func TestSubscriptionMikanPosterSourcePrefersSavedMikanImage(t *testing.T) {
	original := searchSubscriptionMikan
	searchSubscriptionMikan = func(context.Context, string) ([]parser.SearchResult, error) {
		t.Fatal("saved Mikan image should avoid a network search")
		return nil, nil
	}
	t.Cleanup(func() { searchSubscriptionMikan = original })

	source, err := subscriptionMikanPosterSource(context.Background(), &model.Subscription{
		MikanID: "3141",
		Title:   "测试番剧",
		Image:   "https://mikanani.me/images/poster.jpg",
	})
	require.NoError(t, err)
	require.Equal(t, "https://mikanani.me/images/poster.jpg", source)
}

func TestSubscriptionMikanPosterSourceRecoversWhenMetadataReplacedImage(t *testing.T) {
	original := searchSubscriptionMikan
	searchSubscriptionMikan = func(_ context.Context, title string) ([]parser.SearchResult, error) {
		require.Equal(t, "测试番剧", title)
		return []parser.SearchResult{
			{MikanID: "other", Image: "https://mikanani.me/images/other.jpg"},
			{MikanID: "3141", Image: "https://www.mikanani.me/images/recovered.jpg"},
		}, nil
	}
	t.Cleanup(func() { searchSubscriptionMikan = original })

	source, err := subscriptionMikanPosterSource(context.Background(), &model.Subscription{
		MikanID: "3141",
		Title:   "测试番剧",
		Image:   "/api/v1/posters/42",
	})
	require.NoError(t, err)
	require.Equal(t, "https://www.mikanani.me/images/recovered.jpg", source)
}

func TestSubscriptionMikanPosterSourceReportsUnavailableSearch(t *testing.T) {
	original := searchSubscriptionMikan
	searchSubscriptionMikan = func(context.Context, string) ([]parser.SearchResult, error) {
		return nil, errors.New("mikan offline")
	}
	t.Cleanup(func() { searchSubscriptionMikan = original })

	_, err := subscriptionMikanPosterSource(context.Background(), &model.Subscription{
		MikanID: "3141",
		Title:   "测试番剧",
		Image:   "/api/v1/posters/42",
	})
	require.ErrorContains(t, err, "重新搜索 Mikan 番剧失败")
}
