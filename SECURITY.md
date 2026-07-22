# 安全策略

## 支持的版本

Animate Auto Tool 是个人维护的开源项目,**仅最新一个发布版本**接收安全修复。

| 版本 | 是否接收安全修复 |
| --- | --- |
| 最新 minor(当前为 `0.5.x`) | ✅ |
| 历史版本 | ❌ — 请先升级 |

建议始终通过应用内自更新或 [Releases 页](https://github.com/pokerjest/animateAutoTool/releases) 升级到最新版本,所有发布资产都附带 `SHA256SUMS.txt` 可校验完整性。

## 安全边界(部署前必读)

请先阅读 [README 的"公网部署前先理解安全边界"](README.md#0-公网部署前先理解安全边界)。要点:

- 首次初始化只允许 `localhost` 直连。
- 公网部署必须经 HTTPS 反向代理,并正确配置 `server.public_url` 与 `server.trusted_proxies`。
- 不要把 `0.0.0.0/0` 或整段内网写进 `trusted_proxies`。
- 忘记密码恢复 `/recover` 仅接受本机直连,不通过反向代理。
- 应用登录密码以 bcrypt 哈希保存;外部服务 Token / 密码保存在本机数据库或 `data/bootstrap/` 凭据文件中,依赖本机文件权限保护,Web 界面不会回显明文。

## 报告漏洞

**不要在 GitHub Issue 公开报告安全问题。**

请通过以下渠道私密报告:

1. **首选**:GitHub 的 [Private Vulnerability Reporting](https://github.com/pokerjest/animateAutoTool/security/advisories/new)(在仓库 Security 标签下)。
2. **备选**:发邮件给维护者(可在 [pokerjest 的 GitHub 主页](https://github.com/pokerjest) 找到公开邮箱)。

报告时请包含:

- 受影响版本(必填)
- 漏洞类型与严重程度估计(RCE / 信息泄露 / XSS / 权限绕过 / DoS 等)
- 复现步骤、PoC、所需前置条件
- 你认为合适的修复方向(可选)
- 你希望被致谢的方式(可选)

## 响应时序

作为个人维护项目,以下时序是**目标**,非严格 SLA:

| 阶段 | 目标时间 |
| --- | --- |
| 首次确认收到 | 5 个工作日内 |
| 给出初步评估(接受 / 拒绝 / 需更多信息) | 14 天内 |
| 准备并发布修复 | 严重漏洞 30 天内,中低危按下一个常规版本 |
| 公开 Advisory | 修复发布后 7 天内 |

## 披露策略

我们遵循**协同披露(coordinated disclosure)**:

- 修复发布之前,请不要公开漏洞细节。
- 修复发布后,我们会在 [GitHub Advisory](https://github.com/pokerjest/animateAutoTool/security/advisories) 公开,并在 `CHANGELOG.md` 标注。
- 在你同意的前提下,会在 Advisory 中署名致谢。

## 范围

**在范围内:**

- `internal/` 下的所有 Go 代码
- `web/` 下的模板与静态资源
- `cmd/` 入口、`scripts/` 中的部署脚本
- 默认配置带来的不安全行为
- 升级与备份恢复流程

**不在范围内:**

- 第三方依赖本身的漏洞——请直接报告给上游;若我们未及时升级到含修复的版本,可在本项目报告。
- 用户**显式**关闭安全开关后造成的问题(如把 `trusted_proxies` 设为 `0.0.0.0/0`)。
- 需要本机物理访问或已经获得管理员凭据才能触发的问题(已不构成提权)。
- DoS via 资源耗尽,除非有可放大的远程触发路径。

## 安全工程实践

项目当前已落实:

- `golangci-lint` 启用 `gosec`,覆盖 G1xx/G2xx/G3xx/G4xx/G5xx/G6xx 多项规则,详见 [.golangci.yml](.golangci.yml)。
- 每个 PR 跑 `govulncheck`,检测已知 CVE。
- 密码以 bcrypt 哈希保存。
- 外部服务凭据在设置页脱敏显示;选择性备份会清空 password / secret / token / key 等敏感配置值。
- 发布产物附带 `SHA256SUMS.txt`,应用内自更新校验完整性。
- `/recover` 仅接受 localhost 直连。

感谢你帮助让项目更安全。
