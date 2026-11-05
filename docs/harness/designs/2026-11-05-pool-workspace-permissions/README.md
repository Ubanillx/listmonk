# 部门公海列表权限修复（2026-11-05）

用户要求：公海客户列表只展示当前所在部门实际获分配的列表。

复现：`TestPoolListsStayWithinAllocatedWorkspace` 构造两个组织、同源跨组织分配、其他组织独占公海、仅投放授权及已解除分配。修复前本部门预期 2 行，普通账号得到 5 行，平台权限账号最多得到 8 行；直接详情也能读取未分配列表。

修复：组织空间只返回当前组织未解除的分配及对应一级公海。投放授权不等同于查看分配，主数据/投放/跨组织分配/全组织发送权限和最高管理员身份不扩大该目录。完整目录、minimal 选择器、直接详情、客户数和退订人数使用同一组织边界；单池/汇总联系人、筛选候选、分配目录和 Excel 导出同样受限。停用但仍绑定的分配保留可见。个人空间显式平台管理与专用跨组织分配工作台保留独立授权。

界面：组织空间隐藏全部组织/全局公海/其他组织筛选器，改用顶部工作区切换；保留本部门的来源与分配层级。桌面表格名称列设最小宽度，避免权限收敛后少量记录时被其他列挤窄。

验证：

- PostgreSQL 回归 `TestPoolListsStayWithinAllocatedWorkspace` 通过，覆盖普通查看者及 5 类平台权限身份；目录、直接 metadata/contacts/export/allocation 请求和跨组织客户/退订数量均校验。
- PostgreSQL 相关回归 `go test ./cmd ./internal/core -run 'Test.*(Pool|SMTP|CampaignRetry|CampaignRecipientRetries)' -count=1` 通过。
- `go test ./...` 通过。
- 隔离 Cypress 的 `pool-workspace-permissions.cy.js` 1/1 通过，覆盖真实部门切换、隐藏筛选器、其他部门直接链接 403、minimal、汇总客户、分配目录、两类 Excel、个人空间管理与手机无溢出。
- 既有 `pool-list-tree.cy.js` 5 项、`public-pool-smtp.cy.js` 1 项通过。
- 前端 lint 和生产构建通过（仅既有地图变量 shadow warning）；文档链接检查与 `git diff --check` 通过。

本次不需要数据库迁移。生产前端资产已重建，本地 `dev-backend-1` 已重启，9173 返回 HTTP 200，启动日志无错误且无待执行升级。未部署线上。

已查看实际截图：[部门目录（1200px）](shots/north-desktop.png)、[手机目录（390px）](shots/north-mobile.png)。
