# 1.0 Release Checklist

这份清单是打 `v1.0.0` stable 前的硬门禁。任何一项失败都不能用“本机能启动”替代。

## A. 版本、工作区和文档

1. 确认 `VERSION`、`web/frontend/package.json`、`web/frontend/package-lock.json`、打包脚本和目标 tag 都是同一个 `v1.0.0`。
2. 更新 `CHANGELOG.md`，说明新增 migration、数据修复、升级边界和已知不可逆限制。
3. `git status --short` 只包含有意提交的文件；不提交真实 `config.yaml`、数据库、日志、诊断包、临时构建目录。
4. `git diff --check` 通过。
5. 文档站严格构建和 OpenAPI 校验通过：

   ```bash
   mkdocs build --strict
   python -m openapi_spec_validator docs/openapi.yaml
   ```

## B. 数据库、升级和恢复

1. `go test ./... -count=1`
2. `go test ./... -race`
3. `make lint`
4. `make vulncheck`
5. `bash ./scripts/test_historical_upgrade_matrix.sh`
6. 确认真实升级矩阵固定覆盖：
   - `v0.9.9`
   - `v1.0.0-beta.1`
   - `v1.0.0-beta.7`
   - `v1.0.0-beta.14`
7. 数据库专项测试必须覆盖：
   - migration checksum/fingerprint 改写拒绝；
   - 未知 migration、未来 schema 和缺失可信基线拒绝；
   - migration 失败只回滚当前条目，下次可重试；
   - 009/015 preflight、survivor 映射和 `report.json`/`summary.txt`；
   - 二次启动幂等和无孤儿引用。
8. restore 专项测试必须覆盖：
   - 缺少表时不删除当前表；
   - 用户、episode、anime、metadata、playback 依赖校验；
   - 恢复用户后现有 session 立即失效；
   - schema、SQLite check、关键表计数和配置镜像校验。

## C. 前端、打包和真实运行

1. `make frontend-test`
2. `make frontend-build`
3. `make package-e2e`，确认解压后的真实发行包可以启动、登录、访问核心页面并优雅停止。
4. 用目标版本打包：

   ```bash
   ./scripts/package.sh v1.0.0
   ```

5. 验证 `dist/` 包含当前平台和 CI 生成的所有资产，并运行：

   ```bash
   DIST_DIR=./dist bash ./scripts/check_release_assets.sh v1.0.0
   ```

6. manifest 必须声明：
   - `database_format=1`
   - `schema_format=1`
   - `schema_version=015`
   - `min_upgrade_from=0.9.9`
   - 稳定版 `channel=stable`
   - `rollback_supported` 与真实“程序 + 数据库 + 配置”回切能力一致
7. 真实服务 smoke test：
   - `/login`
   - `/calendar`
   - `/subscriptions`
   - `/local-anime`
   - `/backup`
   - `/api/v1/health`
   - `/api/v1/diagnostics/health/export`
8. 检查启动日志出现预期 schema、migration run 和 readiness 结果；模拟一次后台任务失败，确认 health 日志能定位组件、阶段、对象 ID 和恢复动作。

## D. 打 tag 后

1. 确认 GitHub Actions 的测试、打包、Windows E2E、DMG、release jobs 全部成功。
2. Release 页面必须包含：
   - Linux `amd64/arm64` `tar.gz`
   - Windows `amd64` `exe` 和 `zip`
   - macOS `amd64/arm64` `tar.gz` 和 `dmg`
   - `animate-release-manifest.json`
   - `SHA256SUMS.txt`
3. 下载至少一个 Windows、Linux 和 macOS 资产，验证能启动并显示正确版本。
4. 用 `v0.9.9` 或 beta fixture 做一次升级后启动检查，确认 schema、关键表计数和二次启动幂等。
5. 若出现 migration repair 或不可逆历史数据限制，在 GitHub Release notes 和 `CHANGELOG.md` 同时记录。

## Updater naming rules

The app updater currently recognizes these release asset suffixes:

1. Windows: `_windows_<arch>.exe`
2. Linux: `_linux_<arch>.tar.gz`
3. macOS app bundle installs: `_darwin_<arch>.dmg`
4. macOS unpacked binary installs: `_darwin_<arch>.tar.gz`

Recommended filenames:

1. `AnimateAutoTool_<version>_windows_amd64.exe`
2. `AnimateAutoTool_<version>_linux_amd64.tar.gz`
3. `AnimateAutoTool_<version>_linux_arm64.tar.gz`
4. `AnimateAutoTool_<version>_darwin_amd64.tar.gz`
5. `AnimateAutoTool_<version>_darwin_arm64.tar.gz`
6. `AnimateAutoTool_<version>_darwin_amd64.dmg`
7. `AnimateAutoTool_<version>_darwin_arm64.dmg`
8. `SHA256SUMS.txt`

`SHA256SUMS.txt` should include checksum lines for all updater assets above.

Release assets use the `AnimateAutoTool` prefix. The updater matches platform suffixes and remains compatible with older `animate-server_*` assets. The launcher stored inside archives and macOS app bundles is named `AnimateAutoTool`; archives also carry an `animate-server` compatibility copy so v0.9.9 can upgrade in place.
