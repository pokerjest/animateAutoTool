# Release Checklist

## Before tagging

1. Confirm `VERSION` matches the intended release version.
2. Run `go test ./internal/db -count=1`.
3. Run `go test ./internal/api -count=1`.
4. Run `go test ./... -race`.
5. Run `./scripts/package.sh` and verify new artifacts exist in `dist/`.
6. Smoke-check core flows on a live server:
   - `/login`
   - `/calendar`
   - `/subscriptions`
   - `/local-anime`
   - `/backup`
7. Verify the latest schema migration was applied and startup logs show the expected schema version.
8. Review `git status` and confirm no accidental local files are about to ship.

## After pushing the release tag

1. Confirm GitHub Actions created the release workflow run.
2. Confirm the Release page contains:
   - Linux `tar.gz`
   - Windows standalone `exe`
   - Windows `zip`
   - macOS `dmg`
3. Download at least one packaged artifact and verify it opens correctly.
4. Record any upgrade notes if the release introduced a new schema migration or data repair step.
