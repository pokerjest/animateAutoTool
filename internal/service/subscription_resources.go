package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

// Subscription resource states are deliberately more expressive than the
// legacy DownloadLog status. They describe the full RSS/qB/local-library
// reconciliation lifecycle while the old log remains a compatibility view.
const (
	SubscriptionResourceStateSeen        = "seen"
	SubscriptionResourceStateFiltered    = "filtered"
	SubscriptionResourceStateUnresolved  = "unresolved"
	SubscriptionResourceStatePending     = "pending"
	SubscriptionResourceStateDownloading = "downloading"
	SubscriptionResourceStateCompleted   = "completed"
	SubscriptionResourceStateFailed      = "failed"
	SubscriptionResourceStateUnknown     = "unknown"
	SubscriptionResourceStateSuperseded  = "superseded"
	SubscriptionResourceStateArchived    = "archived"
)

// resourceFingerprint is stable across RSS refreshes and changes to the
// release title's version marker. InfoHash/magnet are preferred; the title
// fallback still gives old feeds a durable identity.
func resourceFingerprint(ep parser.Episode) string {
	torrentURL := strings.TrimSpace(ep.Magnet)
	if torrentURL == "" {
		torrentURL = strings.TrimSpace(ep.TorrentURL)
	}
	raw := torrentInfoHashFromURL(torrentURL)
	if raw == "" {
		raw = torrentURL
	}
	if raw == "" {
		raw = strings.Join([]string{
			strings.ToLower(strings.TrimSpace(ep.Title)),
			parser.NormalizeSeasonNumber(ep.Season),
			parser.NormalizeEpisodeNumber(ep.EpisodeNum),
		}, "|")
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func resourceCanonicalKey(season, episode, title string, allowMultiSubgroup bool) string {
	return subscriptionEpisodeIdentity(season, episode, title, allowMultiSubgroup)
}

func resourceVersionTag(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	for i := 0; i+2 < len(lower); i++ {
		if lower[i] != 'v' || lower[i+1] < '0' || lower[i+1] > '9' {
			continue
		}
		j := i + 1
		for j < len(lower) && lower[j] >= '0' && lower[j] <= '9' {
			j++
		}
		if j > i+1 {
			return strings.ToUpper(lower[i:j])
		}
	}
	return "V1"
}

func resourceVersionNumber(tag string) int {
	tag = strings.TrimSpace(strings.ToUpper(tag))
	if strings.HasPrefix(tag, "V") {
		value, _ := strconv.Atoi(strings.TrimPrefix(tag, "V"))
		return value
	}
	return 1
}

func resourceStateRank(state string) int {
	switch strings.TrimSpace(state) {
	case SubscriptionResourceStateCompleted:
		return 6
	case SubscriptionResourceStateDownloading, SubscriptionResourceStatePending:
		return 5
	case SubscriptionResourceStateFailed:
		return 4
	case SubscriptionResourceStateSeen:
		return 3
	case SubscriptionResourceStateUnresolved:
		return 2
	case SubscriptionResourceStateFiltered, SubscriptionResourceStateSuperseded:
		return 1
	default:
		return 0
	}
}

func strongerResourceState(current, next string) string {
	if resourceStateRank(next) >= resourceStateRank(current) {
		return next
	}
	return current
}

func resourceStateFromDownloadLog(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case downloadLogStatusCompleted, downloadLogStatusRenamed:
		return SubscriptionResourceStateCompleted
	case downloadLogStatusDownloading:
		return SubscriptionResourceStateDownloading
	case downloadLogStatusFailed:
		return SubscriptionResourceStateFailed
	case downloadLogStatusArchived:
		return SubscriptionResourceStateArchived
	default:
		return SubscriptionResourceStateUnknown
	}
}

func resourceSummary(resources []model.SubscriptionResource) (rss, canonical, confirmed, downloading, completed, failed, unresolved int64) {
	currentResources := make([]model.SubscriptionResource, 0, len(resources))
	keys := make(map[string]struct{})
	for _, resource := range resources {
		if resource.Current {
			currentResources = append(currentResources, resource)
			if key := strings.TrimSpace(resource.CanonicalKey); key != "" {
				keys[key] = struct{}{}
			}
		}
		switch resource.State {
		case SubscriptionResourceStateCompleted:
			completed++
			confirmed++
		case SubscriptionResourceStateDownloading, SubscriptionResourceStatePending:
			downloading++
			confirmed++
		case SubscriptionResourceStateFailed:
			failed++
		case SubscriptionResourceStateUnresolved, SubscriptionResourceStateUnknown:
			unresolved++
		}
	}
	rss = int64(len(currentResources))
	canonical = int64(len(keys))
	return
}

func resourceNeedsAttention(resource model.SubscriptionResource) bool {
	if !resource.Selected {
		return false
	}
	switch resource.State {
	case SubscriptionResourceStateFailed, SubscriptionResourceStateUnresolved, SubscriptionResourceStateUnknown:
		return true
	default:
		return false
	}
}

// ResourceSummary exposes the stable aggregate fields used by both the
// server-rendered card and the v1 JSON API.
func ResourceSummary(resources []model.SubscriptionResource) (rss, canonical, confirmed, downloading, completed, failed, unresolved int64) {
	return resourceSummary(resources)
}

func ResourceNeedsAttention(resource model.SubscriptionResource) bool {
	return resourceNeedsAttention(resource)
}

func orderSubscriptionEpisodesConservatively(episodes []parser.Episode, sub *model.Subscription) []parser.Episode {
	if len(episodes) < 2 {
		return episodes
	}
	order := make([]string, 0, len(episodes))
	groups := make(map[string][]parser.Episode, len(episodes))
	for _, ep := range episodes {
		episode := strings.TrimSpace(ep.EpisodeNum)
		if episode == "" {
			episode = parser.EpisodeNumberFromTitle(ep.Title)
		}
		season := ep.Season
		if sub != nil {
			season = fmt.Sprintf("S%s", mediaSeasonValue(sub, ep.Season))
		}
		key := resourceCanonicalKey(season, episode, ep.Title, sub != nil && sub.AllowMultiSubgroup)
		if key == "" {
			key = "resource:" + resourceFingerprint(ep)
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], ep)
	}

	result := make([]parser.Episode, 0, len(episodes))
	for _, key := range order {
		candidates := groups[key]
		sort.SliceStable(candidates, func(i, j int) bool {
			leftVersion := resourceVersionNumber(resourceVersionTag(candidates[i].Title))
			rightVersion := resourceVersionNumber(resourceVersionTag(candidates[j].Title))
			if leftVersion != rightVersion {
				return leftVersion < rightVersion
			}
			if !candidates[i].PubDate.Equal(candidates[j].PubDate) {
				return candidates[i].PubDate.After(candidates[j].PubDate)
			}
			return false
		})
		result = append(result, candidates...)
	}
	return result
}

type SubscriptionResourceDiscoveryResult struct {
	RSSCount       int
	CanonicalCount int
	Filtered       int
	Superseded     int
	Unresolved     int
	Updated        int
}

// DiscoverSubscriptionResourcesContext refreshes the RSS candidate ledger
// without submitting anything to qBittorrent. It is the safe primitive used by
// "刷新并修复": collect evidence first, then let explicit subscription runs
// decide whether a genuinely new episode should be downloaded.
func (m *SubscriptionManager) DiscoverSubscriptionResourcesContext(
	ctx context.Context,
	sub *model.Subscription,
) (SubscriptionResourceDiscoveryResult, error) {
	if m == nil || sub == nil {
		return SubscriptionResourceDiscoveryResult{}, fmt.Errorf("subscription is nil")
	}
	episodes, activeRSS, _, err := m.parseRSSWithFallback(ctx, sub)
	if err != nil {
		return SubscriptionResourceDiscoveryResult{}, err
	}
	rules := buildSubscriptionRuleSet(sub)
	episodes = orderSubscriptionEpisodesConservatively(episodes, sub)

	var existing []model.SubscriptionResource
	if m.DB != nil && sub.ID != 0 {
		existing, err = store.NewSubscriptionResourceStore(m.DB).ListBySubscription(sub.ID)
		if err != nil {
			return SubscriptionResourceDiscoveryResult{}, err
		}
		_ = store.NewSubscriptionResourceStore(m.DB).MarkAllNotCurrent(sub.ID)
	}
	selectedKeys := make(map[string]bool)
	for _, resource := range existing {
		if resource.Selected && resource.CanonicalKey != "" &&
			resource.State != SubscriptionResourceStateFiltered &&
			resource.State != SubscriptionResourceStateSuperseded &&
			resource.State != SubscriptionResourceStateArchived {
			selectedKeys[resource.CanonicalKey] = true
		}
	}

	result := SubscriptionResourceDiscoveryResult{RSSCount: len(episodes)}
	canonicalKeys := make(map[string]struct{})
	for rank, ep := range episodes {
		episode := strings.TrimSpace(ep.EpisodeNum)
		if episode == "" {
			episode = parser.EpisodeNumberFromTitle(ep.Title)
		}
		season := fmt.Sprintf("S%s", mediaSeasonValue(sub, ep.Season))
		key := resourceCanonicalKey(season, episode, ep.Title, sub.AllowMultiSubgroup)
		if key != "" {
			canonicalKeys[key] = struct{}{}
		}

		state := SubscriptionResourceStateSeen
		reason := "RSS 对账发现"
		selected := true
		switch {
		case !rules.allows(ep):
			state = SubscriptionResourceStateFiltered
			reason = "未通过订阅过滤规则"
			selected = false
			result.Filtered++
		case strings.TrimSpace(episode) == "":
			state = SubscriptionResourceStateUnresolved
			reason = "无法从 RSS 标题解析集数"
			selected = false
			result.Unresolved++
		case selectedKeys[key]:
			state = SubscriptionResourceStateSuperseded
			reason = "同季同集已有已选候选"
			selected = false
			result.Superseded++
		default:
			selectedKeys[key] = true
		}
		if _, err := m.upsertEpisodeResource(sub, ep, season, episode, activeRSS, state, reason, selected, rank); err != nil {
			return result, err
		}
		result.Updated++
	}
	result.CanonicalCount = len(canonicalKeys)
	return result, nil
}

func (m *SubscriptionManager) upsertEpisodeResource(
	sub *model.Subscription,
	ep parser.Episode,
	season, episode, source, state, reason string,
	selected bool,
	rank int,
) (*model.SubscriptionResource, error) {
	if m == nil || m.DB == nil || sub == nil || sub.ID == 0 {
		return nil, nil
	}
	fingerprint := resourceFingerprint(ep)
	if fingerprint == "" {
		return nil, nil
	}
	now := time.Now().UTC()
	torrentURL := strings.TrimSpace(ep.TorrentURL)
	if torrentURL == "" {
		torrentURL = strings.TrimSpace(ep.Magnet)
	}
	rssURL := strings.TrimSpace(sub.RSSUrl)
	sourceLabel := strings.TrimSpace(source)
	if strings.HasPrefix(sourceLabel, "http://") || strings.HasPrefix(sourceLabel, "https://") {
		rssURL = sourceLabel
		if sourceLabel == strings.TrimSpace(sub.BackupRSSUrl) {
			sourceLabel = "backup"
		} else {
			sourceLabel = "primary"
		}
	}
	resource := &model.SubscriptionResource{
		SubscriptionID: sub.ID,
		CanonicalKey:   resourceCanonicalKey(season, episode, ep.Title, sub.AllowMultiSubgroup),
		Fingerprint:    fingerprint,
		Title:          strings.TrimSpace(ep.Title),
		Episode:        strings.TrimSpace(episode),
		SeasonVal:      strings.TrimSpace(season),
		Subgroup:       strings.TrimSpace(ep.SubGroup),
		VersionTag:     resourceVersionTag(ep.Title),
		TorrentURL:     torrentURL,
		RSSURL:         rssURL,
		InfoHash:       torrentInfoHashFromURL(torrentURL),
		Source:         sourceLabel,
		State:          strings.TrimSpace(state),
		StateReason:    strings.TrimSpace(reason),
		CandidateRank:  rank,
		Selected:       selected,
		Current:        true,
		LastSeenAt:     &now,
	}
	rs := store.NewSubscriptionResourceStore(m.DB)
	existing, err := rs.FindByFingerprint(sub.ID, fingerprint)
	if err == nil && existing != nil {
		resource.ID = existing.ID
		resource.CreatedAt = existing.CreatedAt
		nextState := strongerResourceState(existing.State, resource.State)
		if existing.State == SubscriptionResourceStateSuperseded &&
			!existing.Selected &&
			resource.State == SubscriptionResourceStateSeen {
			nextState = existing.State
		}
		if nextState == existing.State && resourceStateRank(existing.State) > resourceStateRank(state) {
			resource.Selected = existing.Selected
		}
		resource.State = nextState
		if strings.TrimSpace(existing.StateReason) != "" && resource.State == existing.State {
			resource.StateReason = existing.StateReason
		}
		resource.LastError = existing.LastError
		resource.TaskHash = existing.TaskHash
		resource.TargetFile = existing.TargetFile
		resource.LastAttemptAt = existing.LastAttemptAt
		resource.RetryAfter = existing.RetryAfter
		if resource.InfoHash == "" {
			resource.InfoHash = existing.InfoHash
		}
		if existing.CompletedAt != nil {
			resource.CompletedAt = existing.CompletedAt
		}
		if existing.SubmittedAt != nil {
			resource.SubmittedAt = existing.SubmittedAt
		}
		if existing.AttemptCount > resource.AttemptCount {
			resource.AttemptCount = existing.AttemptCount
		}
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := rs.Upsert(resource); err != nil {
		return nil, err
	}
	return resource, nil
}

func resourceStateForCanonical(resources []model.SubscriptionResource, key string) (model.SubscriptionResource, bool) {
	var best model.SubscriptionResource
	found := false
	for _, resource := range resources {
		if resource.CanonicalKey != key {
			continue
		}
		if !found || resourceStateRank(resource.State) > resourceStateRank(best.State) ||
			(resourceStateRank(resource.State) == resourceStateRank(best.State) &&
				resourceVersionNumber(resource.VersionTag) > resourceVersionNumber(best.VersionTag)) {
			best, found = resource, true
		}
	}
	return best, found
}

func resourceIDPointer(resource *model.SubscriptionResource) *uint {
	if resource == nil || resource.ID == 0 {
		return nil
	}
	id := resource.ID
	return &id
}

func resourceFingerprintForLog(entry model.DownloadLog) string {
	raw := strings.TrimSpace(entry.InfoHash)
	if raw == "" {
		raw = strings.TrimSpace(entry.Magnet)
	}
	if raw == "" {
		raw = strings.Join([]string{
			strings.ToLower(strings.TrimSpace(entry.Title)),
			strings.ToLower(strings.TrimSpace(entry.SeasonVal)),
			strings.ToLower(strings.TrimSpace(entry.Episode)),
		}, "|")
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ReconcileSubscriptionResourcesFromDownloadLogs projects legacy qB/local
// status into the durable resource table. It is safe to call repeatedly from
// the refresh endpoint and the background worker.
func ReconcileSubscriptionResourcesFromDownloadLogs() (int, error) {
	if db.DB == nil {
		return 0, nil
	}
	rs := store.NewSubscriptionResourceStore(db.DB)
	var logs []model.DownloadLog
	if err := db.DB.Where("subscription_id <> 0").Find(&logs).Error; err != nil {
		return 0, err
	}
	updated := 0
	for i := range logs {
		entry := &logs[i]
		fingerprint := resourceFingerprintForLog(*entry)
		if fingerprint == "" {
			continue
		}
		var resource *model.SubscriptionResource
		if entry.ResourceID != nil && *entry.ResourceID != 0 {
			var found model.SubscriptionResource
			if err := db.DB.First(&found, *entry.ResourceID).Error; err == nil {
				resource = &found
			}
		}
		if resource == nil {
			found, err := rs.FindByFingerprint(entry.SubscriptionID, fingerprint)
			if err == nil {
				resource = found
			} else if errors.Is(err, gorm.ErrRecordNotFound) {
				season := strings.TrimSpace(entry.SeasonVal)
				episode := strings.TrimSpace(entry.Episode)
				resource = &model.SubscriptionResource{
					SubscriptionID: entry.SubscriptionID,
					CanonicalKey:   resourceCanonicalKey(season, episode, entry.Title, false),
					Fingerprint:    fingerprint,
					Title:          entry.Title,
					Episode:        episode,
					SeasonVal:      season,
					TorrentURL:     entry.Magnet,
					InfoHash:       entry.InfoHash,
					Source:         "download-log",
					State:          resourceStateFromDownloadLog(entry.Status),
					TargetFile:     entry.TargetFile,
					Selected:       true,
				}
				if err := rs.Upsert(resource); err != nil {
					return updated, err
				}
			} else {
				return updated, err
			}
		}
		now := time.Now().UTC()
		state := resourceStateFromDownloadLog(entry.Status)
		updates := map[string]any{
			"state":        strongerResourceState(resource.State, state),
			"state_reason": "由下载历史对账",
			"last_seen_at": &now,
		}
		if entry.InfoHash != "" {
			updates["info_hash"] = entry.InfoHash
			updates["task_hash"] = entry.InfoHash
		}
		if entry.TargetFile != "" {
			updates["target_file"] = entry.TargetFile
		}
		if state == SubscriptionResourceStateCompleted {
			updates["completed_at"] = &now
		}
		if err := rs.UpdateByID(resource.ID, updates); err != nil {
			return updated, err
		}
		if entry.ResourceID == nil || *entry.ResourceID != resource.ID {
			if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", entry.ID).Update("resource_id", resource.ID).Error; err != nil {
				return updated, err
			}
		}
		updated++
	}
	return updated, nil
}
