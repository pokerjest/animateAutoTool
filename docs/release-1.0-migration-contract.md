# 1.0 数据库与升级契约

## 兼容边界

1.0 官方支持从 `v0.9.9` 和 `v1.0.0-beta.*` 向前升级。`v0.6`～`v0.8` 只用于非契约回归 fixture。

当前 1.0 stable 基线为数据库 migration `015_local_anime_identity`、`database_format=1`、`schema_format=1`。发布 manifest 中的 `schema_version` 使用序号 `015`；数据库 `schema_migrations` 中仍保存完整 migration ID。应用版本和数据库协议版本分开判断，不能只比较 SemVer。

升级清单同时声明：

- `database_format`：磁盘数据库/备份容器格式；
- `schema_format`：追加式 migration 协议；
- `schema_version`：发布 manifest 使用的当前 schema 序号（例如 `015`）；数据库内部同时保留完整 migration ID（例如 `015_local_anime_identity`）。

这三个值彼此独立，应用版本号不会替代数据库协议版本。

## 迁移规则

- 已发布 migration 的 ID、序号、描述和 checksum 不得修改或重用。
- 只允许追加 migration；旧程序遇到未知或更高 schema 会拒绝启动。
- 每条 migration 使用独立事务，失败后只回滚当前条目，下次从失败条目重试。
- 升级前保留带 SHA256 的数据库快照，失败时不立即清理唯一可恢复快照。
- `schema_migrations` 保存 migration 序号、ID、描述和稳定 checksum；已执行历史与当前代码不一致时拒绝启动。
- 数据库声明未知 migration、未来 schema 或缺少可信历史基线时，旧程序拒绝进入业务服务，只进入维护/恢复路径。
- 迁移运行状态保存在 `data/updates/migration-runs/current.json`，包含 started/completed/failed、失败 migration、最后错误、快照 ID/SHA256 和重试次数。
- 009 和 015 在建立约束前执行可审计的数据修复，报告位于：
  `data/updates/migration-reports/<migration-id>/`。

009/015 报告至少包含数据库版本、迁移前快照 ID/SHA256、受影响行数、survivor/duplicate 映射、字段合并统计、不可恢复字段摘要和执行结果。

## 不可逆操作

历史 beta 数据如果已经执行 009 或 015，但没有对应迁移前快照，应用只会生成 `already_executed_irreversible` 审计报告，不会伪造恢复结果。原始数据只能从迁移前快照恢复。

1.x 禁止删除表/列、收紧约束、静默清空重复数据、修改已发布 migration，或仅替换二进制而不恢复数据库和配置。需要不可逆归并时必须升级主版本并提供 dry-run、统计、报告和恢复路径。

## 回切

回切不是数据库 downgrade。只有稳定版清单明确允许 beta → stable 时才可操作，并且必须恢复“旧程序 + 数据库 + 配置”成套快照。`rollback_supported` 只表示清单声明的完整回切路径；迁移快照本身仅是数据库恢复材料。

## 1.0 验收要求

发布 stable 前必须固定通过：

- checksum/history 改写拒绝、未知 schema 拒绝、未来 schema 拒绝；
- migration 失败注入、单条事务回滚和下次重试；
- 009/015 repair report；
- `v0.9.9`、`v1.0.0-beta.1`、`v1.0.0-beta.7`、`v1.0.0-beta.14` → 当前版本的真实升级矩阵；
- restore 依赖、孤儿引用、session invalidation、schema/quick_check；
- 二次启动幂等，且 `git diff --check`、前端构建、lint、依赖扫描和 packaged E2E 全部通过。
