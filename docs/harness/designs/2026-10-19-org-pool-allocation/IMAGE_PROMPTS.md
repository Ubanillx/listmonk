# AI UI 设计稿提示词记录

生成日期：2026-10-19。工具：内置 `image_gen.imagegen`；未使用 CLI/API 回退。每张图单独调用，均附真实页面截图（归档为 `source-pool-lists.jpg`），`transparent_background=false`。调用时参照曾使用 `.png` 扩展名，交付检查确认其原始字节为 JPEG 后校正了扩展名，没有转换或修改图像内容。

以下记录本轮提示词的完整设计约束与分镜内容，便于复现和细化；不是产品已实现的声明。原始逐字工具请求保留在本聊天的三个 Image Gen 调用中。

## 公共提示词

```text
Use case: ui-mockup. Create realistic production-quality Chinese desktop admin UI designs, one independent screen per image, not a collage or presentation board. Target dimensions 1644 x 1244 pixels, natural desktop aspect ratio. The attached screenshot is the actual Listmonk admin app; use it as strict visual-language grounding, not an edit target. Preserve the white/light-gray canvas, existing logo and left navigation, blue #155eef links and primary actions, restrained green textual status pills, 6px radii, lightweight separators, and existing Inter with Chinese fallback at readable 14–16px body sizes. No gradients, illustrations, charts, marketing, dark theme, new branding, nested cards or fake feature inventory. Use purposeful spacing and hierarchy. Current date is 2026-10-19, use Chinese dates such as 2026年10月19日, no weekdays. Original creation date may remain 2026年09月28日.

Goal: platform administrators can manage organization public-pool allocations across organizations without implicitly granting master-data editing or delivery-authorization capabilities. Keep source pool, target organization, allocation lifecycle and delivery authorization distinct. Organization selection does not switch workspace or grant membership. Member removal affects only the target organization's relationship, never shared pool contact data or another organization. Use masked fictitious example.com addresses with literal *** for delegated operators. No master-contact creation/import/edit-email/permanent-delete, export, SMTP credentials or campaign-send buttons inside the allocation workflow. Chinese labels must be concise and legible. One primary use case, not every feature at once.
```

## 显示顺序 1 的附加提示词

```text
Concept name: 上下文侧抽屉. Show the existing 公海客户列表 tree with parent 国际客户公海 and indented child 测试组织公海分配. Child actions 查看客户 and 管理分配; preserve the selected row as context. Open a right drawer about 690px wide, title 管理组织公海分配, fixed read-only 来源公海：国际客户公海 and 目标组织：测试组织, small 跨组织管理 · 不切换工作区 label. Tabs 客户分配 selected, 分配信息, 操作记录. Quiet notice 仅调整本组织分配，不修改公海主数据. Search 搜索客户编码或名称 and one 正常客户 state dropdown. Table with checkboxes, 客户编码 / 客户名称 / 邮箱 / 操作; C001 北辰贸易 bei***@example.com, C002 远航科技 yuan***@example.com, C003 青禾工业 qing***@example.com. Row action 从分配移除. One selected row exposes 已选 1 位客户 and primary 移除分配. Familiar compact pagination, footer 关闭. Separate read-only status 投放授权：已授权, 与客户分配独立管理. This frame portrays an allocation-only delegate, so no authorization toggle. Do not compress the background table into unreadable names. Focus ring at 管理分配.
```

输出：`concept-1-context-drawer.png`。图中额外生成了一个“查看已移除客户”链接；实现契约只保留状态下拉，不重复入口。

## 显示顺序 2 的附加提示词

```text
Concept name: 独立分配工作页. A full dedicated allocation detail page, not a drawer. Left navigation highlights 公海客户列表; top workspace stays 个人空间. 返回列表, breadcrumb 公海客户列表 / 国际客户公海 / 测试组织, heading 测试组织公海分配. Read-only 来源公海：国际客户公海 · 目标组织：测试组织 and 跨组织管理，不切换工作区. Tabs 客户分配 active, 分配信息, 操作记录. Slim summary 分配状态：已启用 | 投放授权：未授权. Quiet help 未授权不影响分配管理，但不能用于组织投放. Separate text link 管理投放授权 at far right; this frame is a highest administrator with access, but no coupled grant checkbox. Notice 这里只调整本组织的分配关系，不修改一级公海客户. Search, 正常客户 dropdown, primary 分配已有客户. Spacious table checkbox / 客户编码 / 客户名称 / 邮箱 / 分配时间 / 操作. C001 北辰贸易 bei***@example.com, C002 远航科技 yuan***@example.com, C003 青禾工业 qing***@example.com, C004 恒信商贸 heng***@example.com, dates 2026年10月19日. Each action 从分配移除. Familiar pagination, note 移除后公海主数据保留，其他组织分配不受影响. No metrics cards or decorative UI.
```

输出：`concept-2-allocation-page.png`。图稿统一使用脱敏演示邮箱；实际最高管理员与委派操作员的 DTO 差异以交互规范和原有敏感字段策略为准。

## 显示顺序 3 的附加提示词

```text
Concept name: 跨组织分配工作台. A wide centered modal over the existing pool-tree page, distinctly not a right drawer or dedicated page. Width about 1180px, height 900px, calm translucent overlay. Title 管理组织公海分配, X close. 来源公海：国际客户公海, small 跨组织管理 · 当前工作区不变. Two columns: left 240px target organization navigator with search 搜索组织; 测试组织 selected 已绑定, 华东业务部 已绑定, 华南业务部 未绑定. These are targets, not workspace switchers. Right header 测试组织, subdued 测试组织公海分配, 已启用. Tabs 客户分配 active, 分配信息, 操作记录. Notice 仅管理分配关系，不修改公海主数据. Search 搜索客户编码或名称, status 已移除客户. Table 客户 / 移除原因 / 操作. C005 · 松柏科技, song***@example.com, 转入私有, 恢复分配; C006 · 海川贸易, hai***@example.com, 重复分配, 恢复分配. Pagination. Note 恢复仅作用于测试组织，不解除联系人黑名单. Separate read-only 投放授权：已授权, 无投放授权管理权限; no grant/revoke buttons. Footer secondary 分配已有客户 and 关闭. Focus on fast cross-organization management and restoring relationships, no unrelated actions.
```

输出：`concept-3-organization-workbench.png`。
