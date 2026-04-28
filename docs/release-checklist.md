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
   - macOS `tar.gz`
   - macOS `dmg`
   - Stable tags publish as normal releases; tags containing `-` publish as prereleases
3. Download at least one packaged artifact and verify it opens correctly.
4. Record any upgrade notes if the release introduced a new schema migration or data repair step.

## Updater naming rules

The app updater currently recognizes these release asset suffixes:

1. Windows: `_windows_<arch>.exe`
2. Linux: `_linux_<arch>.tar.gz`
3. macOS app bundle installs: `_darwin_<arch>.dmg`
4. macOS unpacked binary installs: `_darwin_<arch>.tar.gz`

Recommended filenames:

1. `animate-server_<version>_windows_amd64.exe`
2. `animate-server_<version>_linux_amd64.tar.gz`
3. `animate-server_<version>_linux_arm64.tar.gz`
4. `animate-server_<version>_darwin_amd64.tar.gz`
5. `animate-server_<version>_darwin_arm64.tar.gz`
6. `animate-server_<version>_darwin_amd64.dmg`
7. `animate-server_<version>_darwin_arm64.dmg`
8. `SHA256SUMS.txt`

`SHA256SUMS.txt` should include checksum lines for all updater assets above.
