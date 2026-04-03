package startup

import (
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/worker"
)

// Run performs runtime-only initialization that should not happen as a side
// effect of constructing HTTP routes.
func Run() {
	scannerSvc := service.NewScannerService()
	scannerSvc.CleanupGarbage()

	metaSvc := service.NewMetadataService()
	metaSvc.StartMetadataMigration()

	worker.StartMetadataWorker()
	worker.StartDownloadLogSyncWorker()

	authSvc := service.NewAuthService()
	authSvc.EnsureDefaultUser()
}
