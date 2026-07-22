# 贡献指南

感谢你考虑为 Animate Auto Tool 做贡献!这份文档说明如何参与项目开发、提交 PR 和报告问题。

## 行为准则

请保持友善与建设性。我们不接受人身攻击、骚扰或歧视性言论。

## 我可以做什么

- **报告 Bug**:通过 [Issues](https://github.com/pokerjest/animateAutoTool/issues) 提交,使用 Bug 模板。
- **提议新特性**:先开 Issue 讨论,避免你写完才发现方向不符。
- **改进文档**:README、`docs/` 下的文档、代码注释都欢迎。
- **修 Bug / 加特性**:认领或创建 Issue,然后开 PR。
- **补测试**:`internal/api`、`internal/launcher`、`internal/service/worker` 覆盖率偏低,欢迎补强。

## 开发环境

### 前置要求

- Go 1.25+
- (Linux) `libgtk-3-dev libappindicator3-dev` 用于系统托盘
- qBittorrent 4.0+(集成测试需要,本地开发可跳过)
- `golangci-lint` v2.11+(本地跑 lint 用)

### 起步

```bash
git clone https://github.com/pokerjest/animateAutoTool.git
cd animateAutoTool
./scripts/setup.sh           # 初始化环境与配置
vi config.yaml               # 填入 qBittorrent 等信息
./scripts/manage.sh run      # 前台运行,适合调试
```

访问 `http://localhost:8306`。

## 分支与提交

- 从 `main` 切分支,命名建议:`feat/xxx`、`fix/xxx`、`docs/xxx`、`refactor/xxx`、`test/xxx`。
- **提交信息**使用现有风格的简短描述,首行不超过 72 字符:
  - `feat: 添加 RSS 批量删除`
  - `fix: 修正 Windows 启动控制台残留`
  - `docs: 补充反向代理配置示例`
  - `refactor: 把 API 调用收口到 store`
  - `test: 覆盖 launcher 健康检查`
- 若修复 Issue,请在提交信息或 PR 描述写 `Fixes #123`。

## 代码规范

### 必须

- **`go test ./...` 通过**(含 `-race` 更佳)。
- **`golangci-lint run` 通过**:启用 `errcheck` / `govet` / `staticcheck` / `gosec` / `goconst` / `gocyclo` / `ineffassign` / `unused`,详见 [.golangci.yml](.golangci.yml)。
- **`gofmt` / `goimports` 格式化**(CI 会检查)。
- **`govulncheck ./...` 无新增高危漏洞**。
- 新增/修改公开行为应有对应单元测试。

### 推荐

- 数据访问统一走 `internal/store`,不要在 handler 里直接拿 `db.DB.Where(...)`。详见 [docs/architecture.md](docs/architecture.md)。
- HTTP 调用统一走 `internal/httpx`,带超时和代理支持。
- 错误信息倾向中文(项目以中文用户为主),日志保留英文键名 + 中文描述。
- 不要为了"以防万一"加冗余的 `nil` 检查或 try-catch 风格的多层 wrap——内部代码相信内部契约。
- 新增 SQL 字段、改字段类型或加唯一约束:**写显式 migration**,不要依赖 `AutoMigrate`,见 [README 数据库迁移约定](README.md#数据库迁移约定)。

## 测试要求

| 改动类型 | 最低测试期望 |
|---------|------------|
| Bug 修复 | 一条复现该 Bug 的回归测试 |
| 新 store 方法 | 单元测试 + sqlite in-memory 集成 |
| 新 API handler | 至少覆盖成功路径与一个常见失败 |
| 新数据库 migration | 至少一条"旧库升级到新版本"的测试 |
| 纯重构 | 不破坏既有测试,不降低覆盖率 |

跑测试:
```bash
go test ./...                              # 全部
go test ./internal/api -count=1 -v         # 单个包
go test ./... -race                        # 并发竞态
go test ./... -coverprofile=cover.out      # 覆盖率
```

## 提交 PR

1. **先开 Issue 讨论大改动**——避免做完才发现方向不对。小修不必。
2. 基于最新 `main` rebase 或 merge。
3. **自查清单**(也在 PR 模板里):
   - [ ] `go test ./...` 通过
   - [ ] `golangci-lint run` 通过
   - [ ] `gofmt` / `goimports` 已格式化
   - [ ] 新增/修改功能有测试
   - [ ] 文档已同步(README / `docs/` / 代码注释)
   - [ ] 涉及数据库的改动有 migration
4. **PR 描述**要写清楚:做了什么、为什么、影响范围、如何手测。
5. CI 全绿后等待 review。

## 发版流程

由维护者执行,详见 [docs/release-checklist.md](docs/release-checklist.md)。简要:

1. 更新 `VERSION` 与 `CHANGELOG.md`。
2. 跑发版前检查(测试、打包、烟雾测试)。
3. 打 tag `vX.Y.Z` 并 push,GitHub Actions 自动构建并发 Release。

## 报告安全漏洞

**不要**在公开 Issue 报告安全问题。请参考 [SECURITY.md](SECURITY.md)。

## 项目结构速览

更多细节见 [docs/architecture.md](docs/architecture.md)。

```
cmd/             主程序入口(server / doctor / repair)
internal/
  api/           HTTP handler(Gin)
  service/       业务逻辑与 worker
  store/         数据访问层(GORM)
  model/         GORM model
  downloader/    下载器适配(目前只有 qBittorrent)
  parser/        RSS / 文件名解析
  httpx/         统一 HTTP 客户端
  db/            数据库连接与 migration
web/             HTML 模板与静态资源
docs/            架构、发版、QA 清单
scripts/         运维脚本
```

## 有问题?

- 开 [Issue](https://github.com/pokerjest/animateAutoTool/issues) 提问(标签选 `question`)
- 看 [README](README.md) 与 [docs/](docs/)

再次感谢你的贡献!
