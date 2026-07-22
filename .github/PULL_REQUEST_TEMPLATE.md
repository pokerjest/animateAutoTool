<!--
感谢提交 PR!请填写下面的信息,帮助 reviewer 更快理解你的改动。
小改动(typo、单行修复)可以删掉用不到的章节。
-->

## 改动概述

<!-- 一两句话说清楚做了什么、为什么 -->

## 关联 Issue

<!-- 如果修复了 Issue,写 `Fixes #123`;只是相关,写 `Refs #123` -->

Fixes #

## 改动类型

- [ ] Bug 修复(non-breaking)
- [ ] 新功能(non-breaking)
- [ ] 破坏性变更(需要在 CHANGELOG 标记,可能需要 migration)
- [ ] 重构(行为不变)
- [ ] 文档
- [ ] 测试
- [ ] 构建 / CI / 脚本

## 影响范围

<!-- 改了哪些模块?是否影响 API 兼容性?是否需要新 migration?是否影响升级路径? -->

## 如何手测

<!-- 给 reviewer 一份最小复现路径,例如:
1. 跑 ./scripts/manage.sh run
2. 打开 /subscriptions,点击 ...
3. 看到 ...
-->

## 自查清单

- [ ] `go test ./...` 通过
- [ ] `golangci-lint run` 通过(或本地至少 `go vet`)
- [ ] `gofmt -l .` 与 `goimports -l .` 输出为空
- [ ] 新增/修改的功能有对应测试
- [ ] 涉及数据库改动已写显式 migration(`internal/db/migrations.go`)
- [ ] 文档已同步(README / docs/ / 代码注释)
- [ ] 没有提交敏感信息(Token、密码、Cookie、个人路径)
- [ ] 不破坏现有 API 兼容性(若破坏,已在改动概述里标注)

## 截图(可选,UI 改动建议附)

<!-- 拖图片到此处 -->
