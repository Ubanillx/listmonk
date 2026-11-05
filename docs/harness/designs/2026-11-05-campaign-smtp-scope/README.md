# 营销活动 SMTP 归属整改（2026-11-05）

用户要求：公海与私域采用相同发件逻辑；个人来源使用活动所有者；组织来源在组织空间选择该组织发件池；个人空间选择组织来源提示切换、隐藏邮箱。

复现：新增 `TestCampaignSMTPDoesNotFollowPublicAudienceOrganizations`，修改前个人概览返回空、个人空间暴露组织邮箱、全组织活动拒绝单池选择，四个子测试失败。

实现：SMTP 概览、保存、测试、启动、定向补发、全组织分配、就绪和额度使用同一活动归属。受众切换不重设来源/池/速率，保存后加载保留非首个池。池 ID 纳入已保存发送条件状态键。桌面来源/发件池选择器上下排列并限制控件宽度，避免长名称溢出；浏览器回归检查控件未超出区块。个人额度跨目标组织只统计一次。已投递快照不改写；旧组织来源缺池活动需选择池并保存后继续。

验证：

- `go test ./...`：840 通过（30 个包）。
- 使用独立 PostgreSQL 数据库/测试 schema，`go test ./cmd ./internal/core -run 'Test.*(Pool|SMTP|CampaignRetry|CampaignRecipientRetries)' -count=1`：171 通过；覆盖来源隔离、跨组织池拒绝、实际领取的发件 UUID、额度不重复、定向重试以及停发时归还公海 queued。
- `node dev/run-cypress.js --spec cypress/e2e/campaign-smtp-defaults.cy.js,cypress/e2e/public-pool-smtp.cy.js`：3/3 通过；覆盖个人空间提示、发件池选择/保存/刷新、切受众保留速率、空池状态和池变更使旧就绪失效，包含 390px 窄屏。
- 前端 lint、生产构建、文档链接检查通过；lint 保留已有地图变量 shadow warning。
- PowerShell 手工 SMTP 验证器同步 owner 规则并通过语法检查，未运行该脚本的 MailHog 场景；实际公海分配器由 PostgreSQL 回归覆盖。
- 本地前端已重建、后端已重启，`http://localhost:9173` 返回 200，无待执行升级；未部署线上。

截图（已查看实际图像）：[个人空间提示](shots/personal-workspace-organization-warning.png)、[组织发件池](shots/organization-sender-pool.png)、[手机布局](shots/organization-sender-pool-mobile.png)。
