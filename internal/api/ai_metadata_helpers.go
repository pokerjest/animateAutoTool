package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

func searchMetadataCandidates(ctx context.Context, source, keyword string) ([]SearchResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	switch source {
	case SourceTMDB:
		token := configValue(model.ConfigKeyTMDBToken)
		if token == "" {
			return nil, fmt.Errorf("还没有配置 TMDB Token")
		}
		results, err := tmdb.NewClient(token, configuredProxyURL(model.ConfigKeyProxyTMDB)).SearchTVContext(ctx, keyword)
		if err != nil {
			return nil, err
		}
		items := make([]SearchResult, 0, len(results))
		for _, show := range results {
			items = append(items, SearchResult{
				ID: show.ID, Name: show.OriginalName, NameCN: show.Name,
				Summary: show.Overview, AirDate: show.FirstAirDate,
			})
		}
		return items, nil
	case SourceAniList:
		token := configValue(model.ConfigKeyAniListToken)
		if token == "" {
			return nil, fmt.Errorf("还没有配置 AniList Token")
		}
		results, err := anilist.NewClient(token, configuredProxyURL(model.ConfigKeyProxyAniList)).SearchAnimeResultsContext(ctx, keyword, 10)
		if err != nil {
			return nil, err
		}
		items := make([]SearchResult, 0, len(results))
		for _, result := range results {
			name := result.Title.English
			if name == "" {
				name = result.Title.Romaji
			}
			items = append(items, SearchResult{
				ID: result.ID, Name: result.Title.Native, NameCN: name,
				Summary: result.Description,
			})
		}
		return items, nil
	case SourceBangumi, "":
		client := bangumi.NewClient("", "", "")
		applyProxyToBangumiClient(client)
		results, err := client.SearchSubjectsContext(ctx, keyword)
		if err != nil {
			return nil, err
		}
		return bangumiMetadataSearchResults(results), nil
	default:
		return nil, fmt.Errorf("不支持的元数据来源 %q", source)
	}
}
