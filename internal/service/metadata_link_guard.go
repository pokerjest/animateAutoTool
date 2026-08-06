package service

import (
	"log"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// detachMismatchedSharedMetadataLink protects enrichment from mutating one
// metadata row through many unrelated local series. It only detaches when the
// row is shared and every available title identity disagrees.
func detachMismatchedSharedMetadataLink(anime *model.LocalAnime) bool {
	if anime == nil || anime.Metadata == nil || anime.MetadataID == nil || *anime.MetadataID == 0 || db.DB == nil {
		return false
	}
	if localAnimeMatchesMetadataIdentity(anime, anime.Metadata) {
		return false
	}

	var linkedCount int64
	if err := db.DB.Model(&model.LocalAnime{}).
		Where("metadata_id = ?", *anime.MetadataID).
		Count(&linkedCount).Error; err != nil || linkedCount < 2 {
		return false
	}

	metadataID := *anime.MetadataID
	log.Printf(
		"WARN: detached suspicious shared metadata link local_anime_id=%d title=%q metadata_id=%d metadata_title=%q linked_local_series=%d; rebuilding from independent evidence",
		anime.ID,
		anime.Title,
		metadataID,
		anime.Metadata.Title,
		linkedCount,
	)
	anime.MetadataID = nil
	anime.Metadata = nil
	return true
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
