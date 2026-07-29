package api

import "testing"

func TestCandidateMatchesIDsRequiresSameGroupedCandidate(t *testing.T) {
	candidate := MetadataMatchCandidate{
		Bangumi: &MetadataSourceCandidate{ID: 10},
		TMDB:    &MetadataSourceCandidate{ID: 20},
		AniList: &MetadataSourceCandidate{ID: 30},
	}
	if !candidateMatchesIDs(candidate, 10, 20, 30) {
		t.Fatal("expected all three real IDs to match")
	}
	if candidateMatchesIDs(candidate, 10, 99, 30) {
		t.Fatal("unexpected match for an ID outside the candidate snapshot")
	}
	if candidateMatchesIDs(candidate, 10, 0, 0) == false {
		t.Fatal("a single-source legacy proposal should remain valid")
	}
}

func TestTitleScoreRecognizesExactAndVariantTitles(t *testing.T) {
	if score := titleScore("葬送のフリーレン", []string{"葬送のフリーレン"}); score < .99 {
		t.Fatalf("exact title score = %v, want near 1", score)
	}
	if score := titleScore("Frieren Beyond Journey's End", []string{"Sousou no Frieren", "Frieren: Beyond Journey's End"}); score < .7 {
		t.Fatalf("variant title score = %v, want a useful positive score", score)
	}
}

func TestMetadataSearchResultCarriesUnavailableSourceStatus(t *testing.T) {
	result := MetadataMatchSearchResult{
		Query: "test",
		SourceStatus: map[string]MetadataSourceStatus{
			SourceTMDB: {Configured: false, Error: "该来源尚未配置"},
		},
		Candidates: []MetadataMatchCandidate{},
	}
	if result.SourceStatus[SourceTMDB].Configured {
		t.Fatal("unconfigured source was marked configured")
	}
	if result.SourceStatus[SourceTMDB].Error == "" {
		t.Fatal("unconfigured source should carry an actionable error")
	}
}
