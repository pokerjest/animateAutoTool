# Release Checklist

## Before tagging

1. Confirm `VERSION` matches the intended release version.
2. Run `go test ./internal/db -count=1`.
3. Run `go test ./internal/api -count=1`.
4. Run `go test ./... -race`.
5. Run `./scripts/package.sh` and verify new artifacts exist in `dist/`.
6. Run `bash ./scripts/check_release_assets.sh` to validate updater-required assets and `SHA256SUMS.txt`.
7. Smoke-check core flows on a live server:
   - `/login`
   - `/calendar`
   - `/subscriptions`
   - `/local-anime`
   - `/backup`
8. Verify the latest schema migration was applied and startup logs show the expected schema version.
9. Review `git status` and confirm no accidental local files are about to ship.

## After pushing the release tag

1. Confirm GitHub Actions created the release workflow run.
2. Confirm the Release page contains:
   - Linux `tar.gz`
   - Windows standalone `exe`
   - Windows `zip`
   - macOS `dmg`
3. Download at least one packaged artifact and verify it opens correctly.
4. Record any upgrade notes if the release introduced a new schema migration or data repair step.

## Updater naming rules

The app updater currently recognizes these release asset suffixes:

1. Windows: `_windows_<arch>.exe`
2. macOS: `_darwin_<arch>.dmg`

Recommended filenames:

1. `animate-server_<version>_windows_amd64.exe`
2. `animate-server_<version>_darwin_amd64.dmg`
3. `animate-server_<version>_darwin_arm64.dmg`
4. `SHA256SUMS.txt`

`SHA256SUMS.txt` should include checksum lines for all updater assets above.
