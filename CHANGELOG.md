# Changelog

本项目所有显著变更都会记录在此文件。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/),版本号遵循 [SemVer](https://semver.org/lang/zh-CN/) 语义化版本。

发布说明的另一份完整列表见 [GitHub Releases](https://github.com/pokerjest/animateAutoTool/releases)。

## [Unreleased]

## [1.0.0] - 2026-08-06

### Changed

- 固化追加式数据库迁移、checksum/fingerprint、未来 schema 拒绝、009/015 修复报告和真实历史升级矩阵。
- 增强长期运行稳定性：HTTP 超时、request ID、health 日志、后台任务 panic 隔离、优雅关闭、恢复依赖校验和会话失效。
- 1.0 stable 官方升级来源限定为 `v0.9.9` 和 `v1.0.0-beta.*`；`v0.6`～`v0.8` 仅作为非契约回归 fixture。
- 固化 1.0 数据库与升级契约、稳定性观测文档和发布验收清单。

## [1.0.0-beta.18] - 2026-08-06

### Added
- 为 Bangumi 和 Mikan 海报增加有界磁盘缓存，缓存使用原子写入、大小/数量/时效清理，减少长期运行时重复请求。
- 健康检查预加载元数据，降低订阅媒体健康评估的数据库查询数量。

### Fixed
- 海报已经存在本地缓存时优先直接返回；远程源短暂失败时不再重复消耗网络请求。
- 订阅、日历和本地媒体页面的海报回退继续遵守内容类型和响应大小限制。

## [1.0.0-beta.17] - 2026-08-06

### Changed
- 日历海报按首选源、备用源分阶段竞速，并限制并发槽位；第一个有效图片返回后取消较慢请求。
- 订阅与本地番剧匹配增加 provider ID 冲突的路径和标题消歧，避免共享元数据导致错误播放关联。

### Fixed
- provider identity 冲突在证据消失后会自动关闭历史库问题，不再长期阻塞健康状态。
- 日历海报失败会聚合各来源错误，日志能够定位具体来源。

## [1.0.0-beta.16] - 2026-08-05

### Added
- 增加按小时轮转的 server/health 日志、request ID、慢请求和 HTTP 5xx 诊断。
- 增加后台任务、事件总线、scheduler、更新器、备份恢复和托管服务的阶段化日志与 panic 堆栈。
- 健康诊断包增加运行时和 `goroutines.txt` 快照。

### Changed
- HTTP 增加读取头超时、空闲超时和请求头大小限制；优雅关闭会等待已接受的后台任务。
- 单个后台任务失败不再直接拖垮主进程；迁移、恢复和更新失败会明确记录恢复动作。

### Security
- 持久化日志和健康诊断会遮盖常见 Token、密码、API Key、Authorization 和带密码 URL。

## [1.0.0-beta.15] - 2026-08-05

### Added
- 固化 `database_format`、`schema_format`、migration checksum、未来 schema 拒绝和迁移运行 manifest。
- 为 009/015 破坏性修复增加 preflight、survivor 映射、字段合并统计和审计报告。
- 增加真实 `v0.9.9` / 1.0 beta 历史数据库升级矩阵。

### Changed
- restore 增加依赖一致性、schema/SQLite 校验、旧表兼容和恢复用户后的会话失效。
- 发布清单将 1.0 官方最低升级来源收紧为 `0.9.9`，并明确整套快照回切边界。

## [1.0.0-beta.14] - 2026-08-05

### Added
- 新增 `015_local_anime_identity`，为本地文件夹番剧建立稳定 scan key 和唯一索引。

### Fixed
- 迁移前合并重复本地番剧，重新绑定 episode、playback history 和 library issues，并尽量合并非空字段。
- 旧备份恢复后会重新修复 local anime identity、索引和统计，兼容缺少新字段的历史数据库。

## [1.0.0-beta.13] - 2026-08-03

### Changed
- 本地番剧诊断提示改为批量读取开放的 scrape 问题，减少分页页面的重复查询。

### Fixed
- V1 本地番剧接口补充数据库初始化、目录读取、计数和分页查询错误处理，避免异常时返回不完整数据。

## [1.0.0-beta.12] - 2026-08-03

### Fixed
- 发行包浏览器 E2E 会等待订阅接口完成，并按请求 URL 与状态码区分真实故障和可选更新源失败，避免 Windows 构建被正常导航取消或外部更新源波动误判为失败。

## [1.0.0-beta.11] - 2026-08-03

### Changed
- Windows 直接运行带版本号的发行二进制时会迁移并切换到固定启动文件，完成启动后清理旧版和历史命名启动器。

### Fixed
- 订阅与本地番剧的来源冲突只在存在独立身份关联时上报，并自动关闭历史误报，避免无关番剧阻止播放入口识别。
- 元数据补全会先解除多个无关本地番剧错误共享的元数据记录，再依据各自 NFO 或网络来源重建关联，避免跨番剧污染。
- 更新器只选择版本号与目标 Release 一致的安装资源，并优先使用当前 `AnimateAutoTool` 命名，避免误装遗留旧版二进制。

## [1.0.0-beta.10] - 2026-08-01

### Added
- Mikan 番剧发现结果会显示“已订阅”和“本地已有”状态，并复用来源 ID 与强标题匹配判断本地媒体。
- 发布流水线新增 Windows 打包二进制 E2E，验证 ZIP 内容、启动脚本、Playwright 页面流程与优雅停止。

### Changed
- Windows ZIP 打包兼容 zip 和 PowerShell Compress-Archive，Linux CI 可同时生成 Windows E2E 包。

### Fixed
- Mikan 海报代理会在多个可信镜像间并发取最快有效图片，并统一缓存同一路径，改善部分网络下封面加载失败的问题。
- 前端海报回退会记录已尝试的代理和原始地址，避免重复请求或回退循环。

## [1.0.0-beta.9] - 2026-08-01

### Added
- 新增 Windows 平台的应用退出控制和托管子进程控制，停止、重启、更新与回滚可以等待服务正常收尾。

### Changed
- Windows 默认数据目录优先使用 LOCALAPPDATA，更新与回滚辅助程序改用隐藏运行的 PowerShell 脚本。

### Fixed
- 订阅与本地番剧支持通过 Bangumi、TMDB 和 AniList 的命名空间来源 ID 匹配，修复中文译名不同导致播放按钮缺失的问题。
- 来源 ID 或季度冲突会阻止自动关联；重复身份可通过已完成下载路径消歧，否则生成可重复追踪的库问题。
- Windows 文件路径比较改为大小写不敏感，并提高 stop.bat 与 restart.bat 对过期 PID 和退出失败的处理安全性。

## [1.0.0-beta.8] - 2026-08-01

### Changed
- 后台任务统一使用可取消的生命周期上下文，服务停止时等待已接受任务完成，减少 SQLite 关闭时的并发写入。
- 前端构建产物固定使用 LF 行尾并改进跨平台可复现性，避免不同 CI 平台产生无意义的 hash 变化。

### Security
- 限制备份上传大小、TMDB 图片代理响应类型和大小，并加强托管服务下载地址与压缩包校验。

### Fixed
- 修复订阅与本地媒体匹配、备份/R2 恢复、元数据刷新和分页边界等异常路径，补充安全回归测试。

## [1.0.0-beta.7] - 2026-07-31

### Changed
- 订阅列表会先完成 Jellyfin 与本地媒体库对账再计算入库状态，手动检查订阅后也会立即补做映射。

### Fixed
- 修复本地已有可播放剧集、但 Jellyfin 尚未完成关联时，订阅卡片和历史弹窗错误隐藏播放入口的问题。

## [1.0.0-beta.6] - 2026-07-31

### Changed
- 本地媒体扫描现在会明确切换文件扫描与元数据整理阶段，进度在阶段真正完成前最多显示 99%。
- 下载完成后的 Jellyfin 映射按本批受影响番剧执行，并持续轮询直到整批待映射番剧均已处理。

### Fixed
- 清理增量扫描遗留的同路径空白重复番剧记录，避免并发下载后生成幽灵条目。
- 修复文件扫描完成时任务中心短暂显示 100% 或仍停留在第一阶段的问题。

## [1.0.0-beta.5] - 2026-07-31

### Added
- 在系统设置的安全区域支持修改管理员用户名；修改需要当前密码，当前会话保持有效，侧边栏账号名会即时刷新。

### Fixed
- 为用户名修改补充唯一性校验、控制字符校验、审计记录和旧 `/api/*` 兼容接口。

## [1.0.0-beta.4] - 2026-07-31

### Added
- 新增持久化运行会话与数据操作日志；进程崩溃、强制终止或主机断电后，会自动识别并恢复被中断的媒体扫描、元数据链接和订阅对账。
- 为 RSS 主源、备用源和全部源不可用场景补充明确日志，并记录 AList、qBittorrent、Jellyfin 等托管服务的意外退出。

### Changed
- SQLite 启用完整同步与外键约束，启动时执行完整性检查，正常关闭时截断 WAL；数据库损坏时停止后台写任务并给出恢复提示。
- 元数据与订阅链接写入改为事务化处理，恢复期间暂停并发扫描、同步和定时任务，避免重复写入。

### Fixed
- 修复媒体目录只读状态误报、错误文件路径提示，以及共享媒体根目录下不同番剧可能错误关联的问题。
- 修复 RSS 主源宕机后未正确使用备用源、主源错误被 qBittorrent 错误覆盖，以及服务中断信息不足的问题。

## [1.0.0-beta.3] - 2026-07-31

### Fixed
- 修复测试版更新频道错误混入稳定版和“可回切”选项的问题；稳定版与测试版列表现在严格分离。

## [1.0.0-beta.2] - 2026-07-31

### Fixed
- 修复发行包和 Windows 独立可执行文件仍使用 `animate-server_*` 文件名前缀的问题；新资产统一使用 `AnimateAutoTool_*`，同时保留旧资产更新兼容。

## [1.0.0-beta.1] - 2026-07-31

### Changed
- 本地启动器统一更名为 `AnimateAutoTool`（Windows 为 `AnimateAutoTool.exe`），同时保留 `animate-server_*` Release 资产命名以兼容旧版更新器。
- 更新器、回滚流程、启动脚本和发行包统一使用新启动器名称，并支持从 v0.9.9 自动迁移旧文件和脚本。

### Fixed
- 修复本地媒体扫描遇到失效关联记录时可能崩溃的问题，并增加定向扫描回归覆盖。
- 修复移动端顶部同时显示两个菜单按钮的问题。
- 修复发行包中的启动脚本在无源码环境下仍尝试重新构建程序的问题。

## [0.9.9] - 2026-07-31

### Added
- 引入全局悬浮 AI 助手、可确认的内部 AI 工具提案，以及 OpenAI、Gemini、Claude 三类提供商配置。
- 新增订阅资源持久化对账、本地媒体增量扫描、三源元数据匹配和更安全的文件整理流程。
- 新增兼容清单、更新前快照、启动健康检查、失败自动恢复，以及测试版安全切换回稳定版的规则。
- 统一 Q 版角色品牌图标，为文档、浏览器、系统托盘、Windows 可执行文件和 macOS 应用包提供对应资源。

### Changed
- 播放系统拆分为管理与媒体工作区；媒体内容统一通过 Jellyfin 提供商接口浏览和播放。
- 下载完成后按受影响番剧目录批量执行安全整理、增量扫描、资源对账和 Jellyfin 刷新。
- 文档站改为与 Q 版角色一致的深墨蓝、电蓝机能风，并补充当前订阅、扫描、备份、AI 和更新器行为说明。

### Fixed
- 修复同时完成多个订阅时漏扫先完成资源、跨番剧错误映射、修正版标记丢失和 100% 下载仍显示进行中的问题。
- 修复番剧图鉴统计和分页不完整、本地番剧慢速自动加载提前结束，以及旧数据库缺失扩展字段的问题。
- 修复选择性备份恢复清空当前设备凭据、云备份列表删除后消失和更新失败恢复不完整的问题。

## [0.9.9-beta.15] - 2026-07-30

### Changed
- 下载完成状态对账按 InfoHash、精确标题、标准化标题和“季度/集数 + 关联标题 + 订阅目录”逐级匹配，避免用进度状态猜测资源身份。
- qBittorrent 任务匹配同时读取任务名和实际内容路径，支持 `Season 02` 目录、`S01E03-E05` 多集文件，以及 RSS `[MP4]` 与落盘 `.mp4` 名称差异。
- 桌面端侧边栏支持收起和展开，并记住当前浏览器的布局偏好。

### Fixed
- 修复多个番剧存在相同集数时下载记录错误映射，以及证据相同的候选被完成进度错误打破平局的问题。
- 修复同时完成多个订阅下载时，先完成资源未进入完成目标集合、因而漏掉增量扫描的问题。
- 修复自动整理未保留 `V2`、`V3` 修正版标记，及默认模板未完整传入发布组、分辨率、语言和版本信息的问题。
- 修复前端实时下载进度可能套用到无关番剧相同集数任务的问题。

## [0.9.9-beta.14] - 2026-07-30

### Changed
- 番剧图鉴改用与本地番剧一致的服务端分页和自动续载，搜索、订阅状态及本地状态筛选覆盖完整图鉴。
- 下载状态统一结合 qBittorrent 状态、完成进度和已下载字节判断，并同步到下载历史和订阅资源对账。

### Fixed
- 修复番剧图鉴只读取前 100 条、慢速加载后不继续请求，以及不同有效来源 ID 的同名番剧被错误合并的问题。
- 修复 qBittorrent 已达到 100% 但状态短暂保留为 downloading 时，页面和资源对账仍显示下载中的问题。
- 修复归档下载记录重复出现在最近下载和订阅历史，并错误套用实时 qBittorrent 进度的问题。
- 修复旧数据库已经记录 schema 014、但实际缺少 `anime_metadata.sort_title` 等扩展字段时，元数据保存、NFO 生成和 Jellyfin 同步持续失败的问题。

## [0.9.9-beta.11] - 2026-07-30

### Changed
- 下载完成后的本地媒体扫描改为按受影响番剧目录执行，只有无法安全划定范围时才回退到完整媒体根目录扫描。
- 下载完成后的元数据处理只针对受影响番剧，不再触发全库刮削和历史问题修复。
- 合并 15 秒窗口内的下载完成事件，统一执行延迟扫描、订阅资源对账和单次 Jellyfin 媒体库刷新。

### Fixed
- 修复连续下载完成时重复触发本地全库扫描、全库元数据处理和多个 Jellyfin 刷新请求的问题。
- 修复根目录散落文件需要完整扫描时，后续元数据处理范围被错误扩大到全部番剧的问题。
- 保留 qBittorrent 下载完成后的安全改名与移动流程，避免自动整理破坏做种状态。

## [0.9.9-beta.10] - 2026-07-30

### Fixed
- 修复旧数据库缺少扩展元数据字段导致订阅保存失败、下载任务无法继续的问题。
- 增加 AnimeMetadata schema 014 自动迁移，保留旧数据并支持重复启动。
- 修复发布兼容清单仍使用旧 schema 上限，避免更新器错误阻止修复版本。

## [0.9.9-beta.9] - 2026-07-30

### Added
- 新增订阅资源持久化对账，能够区分候选、下载中、已完成和失败资源，并补交确认缺失的剧集。
- 本地媒体扫描新增文件指纹、解析来源、置信度、冲突诊断和数据库迁移，支持多集范围、绝对集数、中文集数及 SP、OVA、OP、ED 等特殊类型。
- 设置页新增 Jellyfin/Emby 命名预设、元数据来源顺序、本地 NFO 覆盖策略，以及 NFO、图片和下载完成后增量扫描开关。

### Changed
- 本地整理统一使用安全预览流程，扩展季度、集数范围、特殊类型、版本、语言和 sidecar 文件处理，并继续保护 qBittorrent 做种文件。
- Bangumi、TMDB、AniList 继续并行联查，按配置顺序合并字段；本地 NFO 的手工字段和真实来源 ID 优先保留。
- NFO 与本地图片改用临时文件和原子替换，下载完成后自动执行增量扫描和媒体库同步。

### Fixed
- 修复旧数据库空字段覆盖新文件名解析结果、特别篇被错误归入第一季，以及整理后未保存完整解析证据的问题。
- 修复选择性备份恢复可能清空当前设备凭据的问题，并增强旧备份表缺失时的兼容恢复。

## [0.9.9-beta.8] - 2026-07-29

### Fixed
- qBittorrent 拒绝重复种子后，支持按磁力链接 InfoHash、内容路径和本地媒体库恢复已有剧集记录。
- 增加 qB 拒绝恢复和 URL 编码磁力链接的回归测试。

## [0.9.9-beta.7] - 2026-07-29

### Fixed
- 修复 qBittorrent 返回包装或旧版 `Fails.` 错误时未进入已有任务恢复流程的问题。
- 已有 qB 任务恢复增加跨平台保存路径、内容路径、集数、季号与标题的保守匹配，兼容别名和季号目录不一致的资源。
- 修复 qB 拒绝种子被误报为 WebUI 无法连接的问题，并增加任务数量和匹配结果诊断日志。
- 订阅检查正常但没有新增资源时计入成功统计，避免健康订阅显示“成功 0”。

## [0.9.9-beta.6] - 2026-07-29

### Added
- 订阅页新增“刷新并修复”，可同步下载器状态、修复下载记录并重新检查启用中的订阅。

### Fixed
- 修复 qBittorrent 返回 `Fails.` 后重复提交同一集下载的问题，能够识别已存在的任务并恢复下载记录。
- 修复订阅进度因失败或过期下载记录被错误推进的问题。
- 本地番剧扫描会串行修复历史 `SQLITE_BUSY` / `database is locked` 元数据问题。
- 增强下载日志、订阅进度和媒体问题记录的 SQLite 锁重试，避免扫描期间数据库写入失败。

## [0.9.9-beta.5] - 2026-07-29

### Added
- 本地导出和 R2 云备份统一压缩为 AES-256 加密 ZIP，支持管理员密码或独立备份密码。
- 恢复流程支持 ZIP 解密校验，同时继续兼容旧版 `.db` / `.sqlite` 备份。

### Fixed
- 修复下载完成后历史记录仍显示“下载中”的问题，增强 `[V2]` 版本资源与 qBittorrent 状态的匹配。
- 下载历史在 qBittorrent 报告完成后可立即显示已完成状态。

## [0.9.9-beta.4] - 2026-07-29

### Fixed
- 下载完成后更快同步 qBittorrent 状态，并立即触发受影响本地媒体目录扫描。
- 文件仍在落盘、移动或自动重命名时不再丢弃扫描任务，15 秒后会再次扫描以更新播放入口。
- 修复 R2 备份上传重试时请求体不可回退导致上传失败的问题，并补充实时下载进度展示。
- 整理侧栏账户、Mikan 资源区和订阅历史等窄屏布局，减少按钮挤压和状态显示错位。

## [0.9.9-beta.3] - 2026-07-29

### Fixed
- 修复删除云端 R2 备份后列表缓存被清空、剩余备份暂时不显示的问题；删除或上传后会重新读取云端列表。
- 修复侧栏左下角用户名、退出提示和版本徽章在窄宽度下挤压、换行和截断异常的问题。

## [0.9.9-beta.2] - 2026-07-29

### Added
- 增强更新器的 Release 兼容清单、更新前快照、启动就绪检查、失败自动恢复和测试版/稳定版切换规则。
- 增强数据库迁移、备份恢复和旧 API 兼容能力，降低版本切换和异常恢复风险。

### Changed
- 清理未使用的调试 CLI、旧 Windows 控制脚本、未完成的 AList/PikPak 秒播实现和重复的 RSS 解析器。
- 保留健康监测、AI、双工作区、播放器、媒体库、备份恢复和旧接口兼容能力不变。
- 修正文档 OpenAPI 描述的 YAML 兼容性问题。

## [0.9.9-beta.1] - 2026-07-29

### Changed
- 将测试版发布号提升为 `v0.9.9-beta.1`，确保当前 `v0.9.8` 正式版可以在测试通道检测并安装。
- 保持 `v0.9.8-beta.1` 的维护页更新通道和版本选择功能不变。

## [0.9.8-beta.1] - 2026-07-29

### Changed
- 系统设置 → 应用维护现在与今日概览共用稳定版 / 测试版更新通道，可选择具体目标版本并执行服务端校验后的更新。
- 保留更新任务状态和部署检查，移除维护页旧的“只更新到最新版本”按钮，避免两个更新入口行为不一致。

## [0.9.8] - 2026-07-29

### Added
- 今日概览新增稳定版与测试版更新通道，可选择具体目标版本并查看当前平台是否有可用安装包。
- 新增统一 Release 列表接口；测试通道同时展示正式版和 GitHub prerelease，稳定通道自动排除测试版本。

### Changed
- 指定版本更新由服务端重新读取 GitHub Release、匹配当前平台资产并校验 SHA256，前端不能提交下载地址或绕过版本校验。
- Release 列表增加短期缓存，安装指定版本时仍会强制重新校验；在线更新只允许升级，不允许降级。
- 番剧图鉴和本地番剧的次要操作收进卡片右上角菜单，卡片主体保持整卡可点击，减少按钮拥挤。

## [0.9.7] - 2026-07-29

### Added
- 新增 Bangumi、TMDB、AniList 三源联查接口，可从任一已知来源或标题出发生成经过后端验证的统一匹配候选。
- AI 助手新增受控的三源元数据搜索工具；AI 只能从真实候选中创建提案，经用户确认后一次性写入三个来源 ID。

### Changed
- 番剧图鉴和本地番剧的元数据修正界面改为展示三源候选、各来源连接状态、匹配依据与缺失来源。
- 海报卡片状态标签移入卡片内容区；追番日历、番剧图鉴、本地番剧、媒体首页和媒体库均支持整卡点击与键盘打开。
- 统一按钮图标的收缩行为，避免窄屏或长文本场景下图标被压缩得过小。

### Fixed
- 本地番剧整卡点击会进入媒体工作区播放器；批量整理模式下整卡点击仅切换选择状态。
- 卡片内的播放、整理、匹配和跳转按钮保持独立交互，不会误触整卡打开操作。

## [0.9.4] - 2026-07-27

### Fixed
- 修复从本地番剧、概览、订阅和继续观看入口播放时仍停留在管理模式的问题。
- 新增媒体工作区本地播放器路由，点击播放后自动进入媒体模式并开始播放。
- 保留旧 `/player` 路由兼容已有书签和旧客户端。

## [0.9.3] - 2026-07-27

### Fixed
- 恢复管理模式中的 AI 助手、系统健康和备份恢复入口；播放器继续作为独立的双模式播放能力。
- 修复移动端底部导航在新增管理功能后被错误挤占的问题，保留概览、日历、订阅和番剧图鉴四个核心入口。

## [0.9.2.1] - 2026-07-27

### Changed
- 播放线路收敛为 AnimateTool 代理和 Jellyfin 直连，并移到完整播放器视频下方；选择后固定使用当前线路，不再因卡顿或播放错误自动回退。
- 当前前端屏蔽 NetBird 配置和线路入口，旧配置、签名接口与流媒体路由继续保留用于兼容已有客户端和链接。
- Jellyfin 连接测试按当前浏览器保存的播放线路执行；媒体模式仅在 Jellyfin 地址与 API Key 均已配置后启用，媒体导航不再显示系统设置。
- 调整媒体首页、媒体库筛选、播放器控制区和 Jellyfin 设置卡片的桌面与移动端布局。

### Fixed
- 更新器支持比较四段版本号，确保 `v0.9.2.1` 能被 `v0.9.2` 正确识别为新版本。

## [0.9.2] - 2026-07-27

### Added
- 播放器新增 NetBird 代理线路，可通过带 12 小时短时签名的 AnimateTool 私网地址传输 Jellyfin 视频，保留 Range 请求且不暴露 Jellyfin API Key。
- 新增管理模式与媒体模式双工作区，以及提供商无关的媒体 API；首版支持 Jellyfin 媒体库、搜索、分页、详情、剧集、继续观看、收藏和播放进度同步。

### Changed
- 播放线路选择移到系统设置并按浏览器保存；播放器只显示实际线路，直连或 NetBird 失败时临时回退 AnimateTool 代理。
- 将项目说明收敛为精简 README，并新增基于 MkDocs Material 的多页中文文档站。
- 补充 TMDB、AniList、Bangumi、Jellyfin、R2、AI 和 Dynu 的官方凭据获取与验证入口。
- 新增 DDNS、双重 NAT、CGNAT、反向代理、Cloudflare Tunnel、Tailscale、FRP 和 IPv6 的公网访问指南。
- 将 Windows Cloudflare Tunnel 的 DNS、Fake-IP、7844 和 HTTP/2 排障过程整理为脱敏 runbook。

## [0.9.1] - 2026-07-27

### Changed
- 将 Jellyfin 的后端连接地址和浏览器直连地址集中到“系统设置 → 媒体服务”，播放器页面只保留播放线路选择。
- 播放器线路统一显示为“Jellyfin 直连”和“AnimateTool 代理”，并补充 Tailscale、局域网、Cloudflare 与公网场景说明。

### Fixed
- 直连持续卡顿或不可用时，提示改为明确说明已切换到 AnimateTool 代理，避免将配置方式误解为必须使用 Tailscale。

## [0.9.0] - 2026-07-27

### Added
- 健康诊断包新增结构化快照脱敏，保证 JSON 合法并保留当前异常、运行时和数据库状态。
- Mikan RSS 临时网络错误自动重试，降低 EOF、超时、429 和 5xx 导致的订阅误报。

### Changed
- 健康报告只将失败或超过 24 小时未更新的下载标记为异常，正常下载中的任务不再显示为阻塞。
- 订阅失败诊断只保留每个订阅最新一次失败状态，避免历史错误重复堆积。
- SQLite 写入冲突扩大退避重试，并根据数据库锁、网络错误和元数据匹配错误显示对应处理建议。
- 同步 OpenAPI 健康报告契约和前端生成类型。

### Fixed
- 修复健康诊断导出中 Token 状态被替换为裸 `[REDACTED]` 导致压缩包内 JSON 无法解析的问题。

## [0.8.9] - 2026-07-27

### Added
- 新增健康诊断异常日志与消费式导出；仅记录需要开发者介入的问题，下载后清理已导出的诊断记录。
- 本地番剧新增单项整理、勾选批量整理和全选当前搜索结果，执行前可预览目标路径、冲突、附属文件与做种状态。

### Changed
- Jellyfin 已扫描条目会自动建立本地关联，下载完成后自动刷新媒体库并同步最新条目。
- 现有番剧整理复用 Jellyfin/Plex 命名模板，字幕、NFO 与图片跟随移动，并在整理完成后重扫本地索引和刷新 Jellyfin。

### Fixed
- 整理现有文件时增加目标冲突、源文件变化、非法路径和 qBittorrent 做种保护，避免覆盖文件或中断不可靠映射的多文件种子。

## [0.8.8] - 2026-07-27

### Added
- 新增默认开启的下载后自动整理配置，支持自定义系列文件夹和剧集文件模板。

### Changed
- 新下载和已有单集通过 qBittorrent 整理为 Jellyfin/Plex 兼容的 `系列/Season 01/系列 - S01E01.ext` 结构，同一番剧各季度归入统一系列目录且不中断做种。

## [0.8.7] - 2026-07-27

### Added
- 订阅管理卡片支持整卡进入详情；已扫描并完成 Jellyfin 关联的订阅可直接跳转播放器，继续沿用现有续播进度。
- 新增 Mikan 海报同源代理、缓存与来源校验，远程设备直连图片失败时由 AnimateTool 主机代抓。

### Changed
- 追番日历从 Bangumi 添加订阅时，优先通过 Mikan 详情页中的 bgm.tv subject ID 精确匹配；中文名未命中时继续尝试原名。
- 追番日历海报采用浏览器直连、主机代理、默认海报三级回退，Mikan 发现页采用相同的远程访问策略。

### Fixed
- 修复同名、译名或续作可能关联到错误 Mikan 番组的问题；精确解析失败时自动回退标题搜索，不阻塞手动选择。
- 修复外部电脑和手机能打开页面但 Mikan、追番日历海报间歇加载失败的问题。

## [0.8.6] - 2026-07-26

### Added
- 新增全局常驻播放器、迷你播放器和继续观看入口，网页内切换路由时保持同一个视频实例、缓冲状态与播放位置。
- 新增按 AnimateTool 用户隔离的播放历史，并与 Jellyfin 共享播放进度；首页可按最近观看顺序一键续播。
- Mikan 订阅新增用户自定义必须包含和必须不含正则，可在订阅前预览筛选结果。

### Changed
- Mikan 字幕组专属 RSS 不再重复添加字幕组过滤规则，多字幕组与全部字幕组模式会保留用户自定义规则。
- HTTP 种子改由 AnimateTool 使用 Mikan 网络与代理配置下载，再以文件方式上传至 qBittorrent；磁力链接继续使用原有添加方式。

### Fixed
- 修复 qBittorrent 登录成功但 Cookie 数量为零时被误判的问题，支持 qBittorrent 的 IP/localhost 免认证模式并验证真实 API 会话。
- 修复 qBittorrent 添加接口返回 HTTP 200 但正文为 `Fails.` 时仍被记录为成功，以及 qBittorrent 主机无法访问 Mikan 时种子静默添加失败的问题。
- 修复字幕组专属 RSS 与自动字幕组正则叠加后重复筛选的问题，并通过幂等迁移清理历史自动规则。

## [0.8.5] - 2026-07-26

### Added
- RSS 订阅新增清晰度与字幕语言筛选，可在字幕组之后继续选择 2160p、1080p、720p、简中、繁中或简繁双语，并完整保存到订阅配置。
- 本地文件扫描新增目录预估和实时进度，任务中心会持续展示已扫描目录、候选番剧与已发现文件数量。
- Jellyfin 播放器新增已看/未看、单集收藏、整部收藏、上一集、下一集及播完自动连播，并展示分辨率、编码、声道、码率、大小、字幕数量与续播百分比。
- 系统健康页新增诊断日志导出，可将最新三个小时日志打包为 ZIP 后直接下载，便于提交故障信息。

### Changed
- Jellyfin 用户状态与媒体信息纳入 OpenAPI 类型契约，剧集列表同步展示 Jellyfin 已看状态和播放进度。
- 订阅筛选与扫描进度接入统一任务状态和前端反馈，生产版前端资源已重新生成。

### Fixed
- 修复播放器更新已看状态时写入 Vue Query 只读数据导致的控制台警告，并确保自动下一集会在媒体准备完成后继续播放。

## [0.8.0] - 2026-07-23

### Added
- Jellyfin 新增独立的浏览器直连地址，可为手机和平板配置 Tailscale 或其他浏览器可达服务器；播放器优先直连，失败时自动回退 AnimateTool 代理。
- Jellyfin 播放信息、流代理和进度上报纳入 OpenAPI 类型契约，播放器恢复 Jellyfin 断点并持续同步观看进度。

### Changed
- 重构本地番剧扫描器，采用递归媒体发现和作品级归并，支持散装剧集、年份/分类多层目录、Season、Specials、BDMV、符号链接及 NFO。
- 同作品的不同字幕组、清晰度和季目录会合并到同一番剧；同名不同年份的重制版继续保持独立。
- 扫描目录进行规范化并串行执行，重叠媒体根目录只认领一次物理文件，减少重复扫描和重复记录。

### Fixed
- 修复根目录视频被逐集识别成多部番剧、嵌套作品被错误合并、目录改名后留下重复项，以及文件恢复后受软删除唯一索引阻挡而漏扫的问题。
- 修复扫描不完整时可能误清理旧记录的问题；子目录无权限或读取失败时保留已有媒体数据并提供诊断。
- 修复 Jellyfin 直连失败后代理回退仍指向旧 `/api` 路径的问题。

## [0.7.4] - 2026-07-23

### Changed
- 媒体库、番剧图鉴和本地番剧列表改为滚动到底部前自动加载下一批内容，不再要求手动点击“继续加载”。
- 追番日历海报改由服务端同源代理、缩略和缓存，减少移动端直连图片源失败及大图解码压力。

### Fixed
- 修复 SQLite 并发写入时偶发 `SQLITE_BUSY / database is locked` 的问题，增加写入串行化、忙等待和有限退避重试。
- 修复可选清晰度升级、Jellyfin 刷新建议及未知分辨率被错误计入“需要关注”的问题。
- 修复本地元数据刷新成功后历史异常仍残留，以及刷新失败时缺少可恢复提示的问题。
- 修复非 localhost 访问点击“本机恢复”后仍进入表单、直到提交才提示受限的问题；现在会在入口处立即说明本机操作方式。

## [0.7.3] - 2026-07-23

### Changed
- 海报列表改用按需生成的缩略图、浏览器条件缓存与分批渲染，降低手机访问时的传输量和图片解码内存；静态哈希资源启用长期缓存。

### Fixed
- 修复移动端同时加载多张原始高分辨率海报时加载缓慢、图片被浏览器回收或显示失败的问题。

## [0.7.2] - 2026-07-23

### Fixed
- 修复代理地址未注入后台 Mikan 订阅、Bangumi 登录回调、部分观看进度、元数据图片、Jellyfin、AI 和更新请求的问题；新增按服务代理开关、地址校验与直接连通性测试。

## [0.7.1] - 2026-07-23

### Changed
- 首次在本机启动时可直接建立仅限 localhost 的初始化会话，由用户自行设置管理员密码，无需查找或输入随机密码。

### Security
- 初始化会话仅允许初始化未完成时通过 localhost 直连和同源请求建立；远程、代理转发及跨站请求继续被拒绝，完成初始化后入口自动关闭。

## [0.7.0] - 2026-07-23

### Added
- 恢复 Mikan 季度番组、文本搜索、字幕组选择、最近资源预览与 RSS 自动配置，并支持新建和编辑订阅时重新关联。
- 新增进程内任务注册表、任务快照 API 与类型化 `task_update` SSE，覆盖同步、扫描、订阅检查与修复、元数据、更新器和 R2 任务。
- 系统设置保存后同步写入本地 `config.yaml`，保留敏感字段留空不覆盖的行为。

### Changed
- 路由主内容加入约 200ms 的淡入上移过渡；异步按钮统一提供转圈、进行中文案、禁用状态和 `aria-busy`。
- 后台任务按钮持续显示到任务真正结束，断线或刷新后可通过任务快照恢复，并在完成后刷新对应数据。
- 追番日历通过海报进入条目并使用完整 Mikan 源添加订阅；备份、设置、AI、播放与认证流程统一异步反馈。

### Fixed
- 修复海报加载失败、Mikan ID 与 Bangumi subject ID 混用，以及旧订阅缺失 Mikan ID 的兼容回填问题。
- 修复列表操作共享忙碌状态、重复提交、后台请求已接收后按钮过早停止和减少动态效果时仍旋转的问题。

## [0.6.1] - 2026-07-23

### Fixed
- Fixed release CI lint failures so tests, lint, and cross-platform packaging pass.

### Security
- Upgraded the Go toolchain and dependencies with known vulnerabilities.

## [0.6.0] - 2026-07-23

### Added
- 使用 Vue 3、TypeScript、Vue Router、Pinia、TanStack Vue Query、Tailwind CSS、Reka UI 与 Lucide 重建完整前端。
- 新增 `/api/v1` JSON API、OpenAPI 3.1 契约、生成式 TypeScript 类型、统一响应和错误结构。
- 新增类型化 SSE 全局任务中心，覆盖扫描、订阅、元数据、下载、备份等长任务。
- 完整覆盖登录、初始化、恢复、仪表盘、订阅、日历、媒体库、本地番剧、播放、备份、健康、设置和 AI 页面。
- R2 上传与暂存改为异步任务，支持进度展示、连通性测试与选择性恢复。
- `CONTRIBUTING.md`、Issue / PR 模板、本 `CHANGELOG.md`,完善社区贡献流程。
- 重写 `SECURITY.md`,明确支持的版本范围与漏洞报告流程。
- `docs/api.md`:全量 HTTP 路由参考文档,按功能分组列出所有 API。
- 审计日志:新增 `audit_logs` 表与 `004_audit_logs` migration,记录登录、密码变更、删除订阅 / 本地目录、备份恢复、R2 删除、AI 设置变更等敏感操作。新增 `GET /api/audit-logs` 端点查询。
- `UserStore`:收口登录、改密、bootstrap 认证和当前会话用户读取的用户表访问。
- 补 `internal/launcher`(URL helpers、unzip / untar 路径穿越防护)、`internal/service`(backup_profiles 纯函数)、`internal/api`(`truncateChatHistory`、登出 / 改密 / 状态端点)的测试,`launcher` 覆盖率从 20.0% 提升到 31.4%,`api` 从 30.2% 提升到 32.7%。

### Fixed
- 修复 `/api/v1/setup/bootstrap` 被首次初始化中间件误拦截的问题。
- 阻止浏览器将登录凭据自动填入 R2 等设置字段。
- 补齐移动端主题切换、退出登录、焦点管理和无横向溢出的响应式布局。
- 前端将 AI 回复、Bangumi / AniList 简介、Toast、R2 进度错误等动态文本改为转义或文本渲染,降低 XSS 风险。
- AI 助手聊天历史改为按用户会话隔离,避免多用户部署时串上下文。
- 修正 `SECURITY.md` 对外部服务凭据“加密存储”的不准确描述,明确当前依赖本机文件权限与 Web 脱敏。

### Changed
- 生产环境移除 Go HTML 模板、HTMX、Alpine、浏览器端 Tailwind 编译器和远程 CDN 运行时依赖。
- Go embed 改为直接嵌入 `web/dist`，继续提供可离线运行的自托管单二进制。
- CI、Docker、Makefile 与发布脚本统一先构建 Node 22 前端，再构建 Go 服务。
- 收紧 CSP，并保留 Cookie 会话、同源写保护、本机恢复限制和现有 SQLite/配置格式。

### Internal
- `internal/model.AuditLog`、`internal/store.AuditLogStore`、`internal/service.RecordAudit` 形成审计日志的分层落地;handler 调用一致采用 `buildAuditContext(c)` 注入会话上下文。

## [0.5.4] - 2026-04-30

### Added
- `SECURITY.md` 首次加入仓库。
- AI 模块与 store 扩展的测试覆盖;补充 AI / Windows 文档。

### Changed
- 加固 Windows 启动流程,优化外部连接稳定性。
- 加固 updater 进度上报与 release 资产校验。

## [0.5.3.2] - 2026-04-29

### Added
- AI 工具页面与配套接口。

### Fixed
- 修复 health / 设置页面回归问题。

## [0.5.3.1] - 2026-04-29

### Fixed
- 修复 v0.5.3 引入的 CI lint 回归。
- 清理 v0.5.3 lint 修复后遗留的未使用辅助函数。

## [0.5.3] - 2026-04-29

### Added
- `docs/architecture.md` 架构指南,正式说明分层与 store 约定。
- `DownloadLog` / `LocalAnime` / `AnimeMetadata` store 与对应测试。
- 订阅策略、健康诊断 / doctor / repair 工具与配套 store helper。
- 覆盖 parser / launcher / updater 的纯函数测试。

### Changed
- 把 API 调用统一收口到 store 层,移除散落的 `db.DB.Where(...)` 直连。
- 收紧访问边界,扩大 store 测试覆盖。

### Chore
- 忽略 `.claude/` 项目元数据目录。

## [0.5.2] - 2026-04-29

### Changed
- 准备 v0.5.2 发布,加固运行时集成。
- 修复 v0.5.2 最后阻塞发布的 lint 问题。

## [0.5.1] - 2026-04-29

### Changed
- 加固 service 层边界。

## [0.5.0] - 2026-04-29

### Added
- 稳定化应用主流程的多处改动。

### Changed
- 工具链升级至 Go 1.25.9。
- 修复多轮 CI lint 回归。

## [0.4.11] - 2026-04-07

### Changed
- 文档与打包默认值同步到 v0.4.11。
- 修复 Windows 发布版本控制台残留,日志改为写入文件。

### Added
- 目录选择器;修正默认下载路径处理。

## [0.4.0] - 2026-04-03

首个对外正式发布版本之一,确立预编译多平台分发流程(Linux / Windows / macOS × amd64 / arm64)。

## [0.3.0] - 2025-12-30

早期里程碑版本。

---

[Unreleased]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.18...v1.0.0
[1.0.0-beta.18]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.17...v1.0.0-beta.18
[1.0.0-beta.17]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.16...v1.0.0-beta.17
[1.0.0-beta.16]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.15...v1.0.0-beta.16
[1.0.0-beta.15]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.14...v1.0.0-beta.15
[1.0.0-beta.14]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.13...v1.0.0-beta.14
[1.0.0-beta.13]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.12...v1.0.0-beta.13
[1.0.0-beta.12]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.11...v1.0.0-beta.12
[1.0.0-beta.11]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.10...v1.0.0-beta.11
[1.0.0-beta.10]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.9...v1.0.0-beta.10
[1.0.0-beta.9]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.8...v1.0.0-beta.9
[1.0.0-beta.8]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.7...v1.0.0-beta.8
[1.0.0-beta.7]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.6...v1.0.0-beta.7
[1.0.0-beta.6]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.5...v1.0.0-beta.6
[1.0.0-beta.5]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.4...v1.0.0-beta.5
[1.0.0-beta.4]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.3...v1.0.0-beta.4
[1.0.0-beta.3]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.2...v1.0.0-beta.3
[1.0.0-beta.2]: https://github.com/pokerjest/animateAutoTool/compare/v1.0.0-beta.1...v1.0.0-beta.2
[1.0.0-beta.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.9.9...v1.0.0-beta.1
[0.9.9]: https://github.com/pokerjest/animateAutoTool/compare/v0.9.8...v0.9.9
[0.9.4]: https://github.com/pokerjest/animateAutoTool/compare/v0.9.3...v0.9.4
[0.9.2.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.9.2...v0.9.2.1
[0.9.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.8.9...v0.9.0
[0.8.9]: https://github.com/pokerjest/animateAutoTool/compare/v0.8.8...v0.8.9
[0.8.8]: https://github.com/pokerjest/animateAutoTool/compare/v0.8.7...v0.8.8
[0.8.7]: https://github.com/pokerjest/animateAutoTool/compare/v0.8.6...v0.8.7
[0.8.6]: https://github.com/pokerjest/animateAutoTool/compare/v0.8.5...v0.8.6
[0.8.5]: https://github.com/pokerjest/animateAutoTool/compare/v0.8.4...v0.8.5
[0.8.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.4...v0.8.0
[0.7.4]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.4...v0.6.0
[0.5.4]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.3.2...v0.5.4
[0.5.3.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.3.1...v0.5.3.2
[0.5.3.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.3...v0.5.3.1
[0.5.3]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/pokerjest/animateAutoTool/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.4.11...v0.5.0
[0.4.11]: https://github.com/pokerjest/animateAutoTool/compare/v0.4.0...v0.4.11
[0.4.0]: https://github.com/pokerjest/animateAutoTool/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/pokerjest/animateAutoTool/releases/tag/v0.3.0
