package startup

import (
	"context"

	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/updater"
	"github.com/pokerjest/animateAutoTool/internal/worker"
)

// Run performs runtime-only initialization that should not happen as a side
// effect of constructing HTTP routes.
func Run(ctx context.Context) {
	scannerSvc := service.NewScannerService()
	scannerSvc.CleanupGarbage()

	metaSvc := service.NewMetadataService()
	metaSvc.StartMetadataMigration()

	worker.StartMetadataWorker()
	worker.StartDownloadLogSyncWorker(ctx)

	authSvc := service.NewAuthService()
	authSvc.EnsureDefaultUser()

	updater.Start()

	startRuntimeMonitor()
}
