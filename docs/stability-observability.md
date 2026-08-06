# 运行稳定性与故障定位约定

AnimateAutoTool 作为长期运行服务，所有关键后台路径都必须满足：

- 失败日志带有组件、阶段、对象 ID（如 migration ID、subscription ID、task ID 或 request ID）；
- 日志说明当前事务/回滚状态和下一步恢复动作；
- panic 必须记录堆栈，且不能因为单个后台任务直接结束整个进程；
- 常规持久化日志不写入 query、cookie、authorization、密码、Token 或 API Key。

## 日志位置

- `logs/server-YYYYMMDD-HH.log`：完整运行日志，按小时轮转，保留最近 7 天；
- `logs/health-YYYYMMDD-HH.log`：只保留错误、失败、超时、数据库锁、权限/磁盘异常和 HTTP 5xx；
- `data/updates/migration-runs/current.json`：最近一次迁移运行状态、失败 migration、重试次数、快照 ID/SHA256；
- `data/updates/migration-reports/<migration-id>/`：009/015 破坏性修复的审计报告；
- 健康诊断导出包：包含当前健康快照、失败任务、数据库运行参数和 `goroutines.txt` 堆栈快照。

## 关键日志前缀

- `Startup`：启动、异常退出恢复、后台 worker 启停；
- `DatabaseMigration`：迁移锁、快照、每条 migration、失败回滚和恢复提示；
- `DownloadLogWorker`：qB 登录、状态同步、扫描、资源对账和延迟重扫；
- `Scheduler`：调度跳过原因、单轮统计和 panic 恢复；
- `Updater`：Release 检查、兼容性拒绝、checksum、快照、下载和应用更新；
- `RestoreService` / `BackupService`：备份/恢复阶段、依赖检查、校验、会话失效和结果；
- `HTTPRequest`：request ID、method、route、status、耗时和错误数量；
- `API background task`：后台任务 ID、任务名、耗时和 panic 堆栈。

## 排障顺序

1. 先查看同一时间段的 `health-*.log`；
2. 用 request ID、migration ID、subscription ID 或 task ID 关联 `server-*.log`；
3. 若涉及迁移/恢复，先核对快照 SHA256 和 `current.json`，不要直接删除数据库；
4. 若出现卡死或 goroutine 泄漏，导出健康诊断包并查看 `goroutines.txt`；
5. 修复后必须重新执行对应阶段的故障注入测试和全量 race 门禁。
