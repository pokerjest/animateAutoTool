package api

import (
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

func configValue(key string) string {
	if db.DB == nil {
		return ""
	}
	return store.NewConfigStore(db.DB).GetDefault(key, "")
}
