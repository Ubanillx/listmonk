# README 配图

本目录存放仓库根 `README.md` 使用的示意图与界面截图。所有截图均取自**本仓库代码**运行的演示实例（演示数据库、中文界面），不包含真实客户数据。

## 文件清单

| 文件 | 说明 |
| --- | --- |
| `hero.svg` | 顶部横幅：项目定位、技术标签与工作区模型概览。 |
| `architecture.svg` | 系统全景（客户端 → Go 服务 → PostgreSQL）与活动发送管道（5 步）。 |
| `dashboard.webp` | 仪表板：私域 / 公海 / 活动 / 发送量卡片。 |
| `campaigns.webp` | 营销活动列表：状态、送达、错误与统计列。 |
| `campaign-overview.webp` | 活动「营销概览」页签：发送进度与失败客户。 |
| `campaign-content.webp` | 活动「内容」页签：富文本编辑器与占位符。 |
| `campaign-report.webp` | 活动「营销统计」页签：打开率、点击率与占比。 |
| `analytics.webp` | 全局营销统计：跨活动指标与趋势。 |
| `customers.webp` | 私域客户列表。 |
| `customer-lists.webp` | 私域客户列表（列表维度）。 |
| `pool-customers.webp` | 公海客户汇总页。 |
| `pool-lists.webp` | 公海客户列表：一级公海与组织分配父子树。 |
| `pool-workbench.webp` | 组织公海分配工作台。 |
| `organizations.webp` | 我的组织：成员数、待迁移资源。 |
| `organization-workbench.webp` | 管理组织：成员、邀请码、回信邮箱、SMTP。 |
| `roles.webp` | 用户角色列表。 |
| `users.webp` | 用户列表。 |
| `audit.webp` | 业务审计日志。 |
| `settings.webp` | 设置页：常规标签。 |
| `settings-smtp.webp` | 设置 › SMTP 投递设置。 |
| `settings-inbound-ai.webp` | 设置 › 回信 AI 分类。 |
| `delivery-provider.webp` | 发件服务：发送分析与投递日志。 |
| `bounces.webp` | 退信列表。 |
| `media-library.webp` | 媒体库：组织 / 所有者筛选。 |
| `templates.webp` | 邮件模板列表。 |
| `forms.webp` | 公开订阅表单：列表选择与可嵌入 HTML。 |

## 重新生成

截图不是手工裁剪的。流程为：启动本仓库后端与演示数据 → 用 Playwright 以 1560×1000、DPR 2 的视口登录并逐页截图 → 统一缩放至 1400px 宽并转为 WebP（质量 82）。

字段与列名如发生变化，请重新采集而不是沿用旧图。截图使用宿主机 Chrome，因此图中的中文字体渲染与最终读者环境可能略有差异。

## 约定

- 图内不得出现真实客户、真实邮箱或真实凭据；示例数据统一使用 `example.com` / `example.org`。
- 只收录能体现**本仓库独有能力**的页面。通用营销功能（导入向导、维护工具、
  移动端适配等）不作为配图，避免把上游已有能力当成本项目卖点。
- 新增或替换配图时，同步更新本清单与根 `README.md` 中引用它的位置；
  删除配图时一并删除磁盘文件，本目录只保留仍被引用的图。
- `hero.svg` 与 `architecture.svg` 为矢量图，可直接编辑源码；其余为位图，需按上文流程重新生成。
