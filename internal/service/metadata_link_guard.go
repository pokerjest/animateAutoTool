package service

import (
	"log"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// detachMismatchedSharedMetadataLink protects enrichment from mutating one
// metadata row through unrelated subscriptions or local series. Conflicting
// provider titles are also isolated because a contaminated alias must not be
// allowed to prove its own association.
func detachMismatchedSharedMetadataLink(anime *model.LocalAnime) bool {
	if anime == nil || anime.Metadata == nil || anime.MetadataID == nil || *anime.MetadataID == 0 || db.DB == nil {
		return false
	}
	identityMismatch := !localAnimeMatchesMetadataIdentity(anime, anime.Metadata)
	sourceConflict := metadataSourcesConflict(anime.Metadata)
	if !identityMismatch && !sourceConflict {
		return false
	}

	linkedLocalCount, linkedSubscriptionCount, err := sharedMetadataLinkCounts(*anime.MetadataID)
	if err != nil || linkedLocalCount+linkedSubscriptionCount < 2 {
		return false
	}
	if identityMismatch && !sourceConflict &&
		!sharedMetadataHasMatchingOwner(*anime.MetadataID, anime.Metadata, anime.ID, 0) {
		return false
	}

	metadataID := *anime.MetadataID
	log.Printf(
		"WARN: detached suspicious shared metadata link local_anime_id=%d title=%q metadata_id=%d metadata_title=%q linked_local_series=%d linked_subscriptions=%d source_conflict=%t; rebuilding from independent evidence",
		anime.ID,
		anime.Title,
		metadataID,
		anime.Metadata.Title,
		linkedLocalCount,
		linkedSubscriptionCount,
		sourceConflict,
	)
	anime.MetadataID = nil
	anime.Metadata = nil
	return true
}

func detachMismatchedSharedSubscriptionMetadataLink(sub *model.Subscription) bool {
	if sub == nil || sub.Metadata == nil || sub.MetadataID == nil || *sub.MetadataID == 0 || db.DB == nil {
		return false
	}
	identityMismatch := !subscriptionMatchesMetadataIdentity(sub, sub.Metadata)
	sourceConflict := metadataSourcesConflict(sub.Metadata)
	if !identityMismatch && !sourceConflict {
		return false
	}

	linkedLocalCount, linkedSubscriptionCount, err := sharedMetadataLinkCounts(*sub.MetadataID)
	if err != nil || linkedLocalCount+linkedSubscriptionCount < 2 {
		return false
	}
	if identityMismatch && !sourceConflict &&
		!sharedMetadataHasMatchingOwner(*sub.MetadataID, sub.Metadata, 0, sub.ID) {
		return false
	}

	metadataID := *sub.MetadataID
	log.Printf(
		"WARN: detached suspicious shared subscription metadata link subscription_id=%d title=%q metadata_id=%d metadata_title=%q linked_local_series=%d linked_subscriptions=%d source_conflict=%t; rebuilding from independent evidence",
		sub.ID,
		sub.Title,
		metadataID,
		sub.Metadata.Title,
		linkedLocalCount,
		linkedSubscriptionCount,
		sourceConflict,
	)
	sub.MetadataID = nil
	sub.Metadata = nil
	return true
}

func sharedMetadataLinkCounts(metadataID uint) (int64, int64, error) {
	var linkedLocalCount int64
	if err := db.DB.Model(&model.LocalAnime{}).
		Where("metadata_id = ?", metadataID).
		Count(&linkedLocalCount).Error; err != nil {
		return 0, 0, err
	}
	var linkedSubscriptionCount int64
	if err := db.DB.Model(&model.Subscription{}).
		Where("metadata_id = ?", metadataID).
		Count(&linkedSubscriptionCount).Error; err != nil {
		return 0, 0, err
	}
	return linkedLocalCount, linkedSubscriptionCount, nil
}

func sharedMetadataHasMatchingOwner(
	metadataID uint,
	metadata *model.AnimeMetadata,
	excludedLocalAnimeID uint,
	excludedSubscriptionID uint,
) bool {
	var localAnimes []model.LocalAnime
	query := db.DB.Select("id", "title", "path", "metadata_id").Where("metadata_id = ?", metadataID)
	if excludedLocalAnimeID != 0 {
		query = query.Where("id <> ?", excludedLocalAnimeID)
	}
	if err := query.Find(&localAnimes).Error; err == nil {
		for i := range localAnimes {
			if localAnimeMatchesMetadataIdentity(&localAnimes[i], metadata) {
				return true
			}
		}
	}

	var subscriptions []model.Subscription
	query = db.DB.Select("id", "title", "metadata_id").Where("metadata_id = ?", metadataID)
	if excludedSubscriptionID != 0 {
		query = query.Where("id <> ?", excludedSubscriptionID)
	}
	if err := query.Find(&subscriptions).Error; err == nil {
		for i := range subscriptions {
			if subscriptionMatchesMetadataIdentity(&subscriptions[i], metadata) {
				return true
			}
		}
	}
	return false
}

func localAnimeMatchesMetadataIdentity(anime *model.LocalAnime, metadata *model.AnimeMetadata) bool {
	if anime == nil || metadata == nil {
		return false
	}
	localTitles := localAnimeIdentityTitles(anime.Title, anime.Path)
	metadataTitles := metadataIdentityTitles(metadata)
	if len(localTitles) == 0 || len(metadataTitles) == 0 {
		// Missing title evidence is not enough to rewrite an association.
		return true
	}
	for _, localTitle := range localTitles {
		for _, metadataTitle := range metadataTitles {
			if titlesStronglyRelated(localTitle, metadataTitle) {
				return true
			}
		}
	}
	return false
}

func subscriptionMatchesMetadataIdentity(sub *model.Subscription, metadata *model.AnimeMetadata) bool {
	if sub == nil || metadata == nil {
		return false
	}
	subscriptionTitles := nonEmptyTitles([]string{sub.Title})
	metadataTitles := metadataIdentityTitles(metadata)
	if len(subscriptionTitles) == 0 || len(metadataTitles) == 0 {
		return true
	}
	for _, subscriptionTitle := range subscriptionTitles {
		for _, metadataTitle := range metadataTitles {
			if titlesStronglyRelated(subscriptionTitle, metadataTitle) {
				return true
			}
		}
	}
	return false
}
