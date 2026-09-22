<a href="https://zerodha.tech"><img src="https://zerodha.tech/static/images/github-badge.svg" align="right" /></a>

[![listmonk 标志](https://user-images.githubusercontent.com/547147/231084896-835dba66-2dfe-497c-ba0f-787564c0819e.png)](https://github.com/Ubanillx/listmonk)

# listmonk 改进版

listmonk 改进版是一个快速、功能完整的自托管邮件通讯和营销活动管理平台。它基于官方 listmonk 的核心架构进行独立扩展，使用 Go 编写，后端和管理端可以打包为一个二进制文件，数据存储在 PostgreSQL 中，也可以使用 Docker Compose 部署。

项目仓库：[Ubanillx/listmonk](https://github.com/Ubanillx/listmonk) · 上游项目：[knadh/listmonk](https://github.com/knadh/listmonk)

![listmonk 管理面板](https://github.com/user-attachments/assets/689b5fbb-dd25-4956-a36f-e3226a65f9c4)

> **项目定位**
>
> 本仓库不是官方 `knadh/listmonk` 的原版镜像，也不代表上游项目。它保留 listmonk 的基础邮件营销能力，并针对多组织协作、公海客户、组织级投递、权限隔离和企业运维场景进行了改进。使用前请以本仓库的代码、迁移和文档为准。

## 与官方原版的关系

官方原版的公开定位是“高性能、自托管的 newsletter 和 mailing list 管理器”，强调单二进制、PostgreSQL、Docker 和二进制部署。本项目在此基础上增加了面向组织协作和业务运营的扩展能力。两者的上游关系见[官方仓库](https://github.com/knadh/listmonk)及其 [README](https://raw.githubusercontent.com/knadh/listmonk/master/README.md)。

| 对比方向 | 官方原版公开 README 的定位 | 本项目改进版 |
| --- | --- | --- |
| 工作区模型 | 以单实例邮件列表管理为主 | 个人空间、组织工作区、工作区选择和组织生命周期管理。 |
| 客户数据 | 传统名单和联系人管理 | 客户视图、公海主数据、组织公海分配、脱敏读取和组织范围控制。 |
| 公海营销 | 官方 README 未列出此业务模型 | 一级公海跨组织受众、联系人去重、组织顺序轮转和统一回件邮箱。 |
| 发件路由 | 常规 SMTP 投递 | 组织成员 SMTP 池轮询、持久化游标、额度联动和发送来源快照。 |
| 回信处理 | 上游 README 未展开 AI 回信流程 | 非破坏 POP3 扫描、OpenAI 兼容 AI 分类、回信转发、去重队列和可审计动作。 |
| 权限与审计 | 上游 README 未展开工作区和动作级权限边界 | 工作区、资源所有权、动作级权限、API Key scope、脱敏边界和业务审计。 |
| 管理端 | 通用邮件营销管理面板 | 公海客户与普通客户统一视图、服务端分页、批量操作、媒体目录和中文业务文案。 |
| 工程文档 | 上游安装和开发说明 | 本项目的架构指南、API 文档、迁移记录、回归脚本和部署约束。 |

## 功能概览

- **联系人和客户列表**：维护客户资料、标签和订阅状态，支持 CSV/XLSX 导入、搜索、分组、批量操作、退订和黑名单。
- **营销活动**：创建 HTML、纯文本或可视化编辑器活动，支持模板、变量、预览、定时发送、发送队列、打开/点击追踪和活动报表。
- **SMTP 与投递管理**：支持个人 SMTP、组织 SMTP、发送限额、退信邮箱、标准一键退订，以及客户回信转发。
- **组织和工作区**：按个人空间和组织工作区隔离资源，支持组织成员、角色、细粒度权限、工作区切换、API 密钥和审计日志。
- **公海客户**：以平台公海和组织公海分配管理共享客户，兼顾组织范围、所有权、权限和邮箱脱敏。
- **媒体库**：按工作区管理媒体文件和嵌套目录，支持拖放上传、移动、重命名和空目录删除。
- **开放接口**：提供 REST API、Webhook 和 Swagger API 规范，便于接入 CRM、数据同步和自动化流程。

## 近期新增功能

### 公海客户统一视图

客户页面现在同时提供“所有客户”和“公海客户”视图。公海联系人支持服务端分页、搜索、排序、行选择、批量分配/移除/恢复、CSV 导出和行内维护；邮箱在非授权场景下自动脱敏。新增 `pools:get`、`pools:manage`、`pools:export` 三项权限，组织用户只能操作所属组织的公海分配。

### 平台级公海营销和 SMTP 轮询

拥有 `campaigns:public_pool_send` 权限的用户可以把一级公海作为跨组织受众。系统会：

- 汇总所有活跃组织的公海分配，并对重复联系人去重；
- 按持久化的组织顺序轮转投递，服务重启或并发发送不会丢失进度；
- 将组织内启用的成员 SMTP 组成发送池，按组织游标逐封轮询；
- 使用目标组织的统一回件邮箱作为 Reply-To，并保留发送来源快照；
- 在发送前检查组织回件邮箱、SMTP 和额度是否就绪，不满足条件时给出具体原因。

普通公海活动仍可以按当前组织发送；跨组织模式只接受一级公海，不会混入普通客户列表。

### 入站回信 AI 分类

回信邮箱支持基于 OpenAI 兼容接口的非破坏性 POP3 扫描。每个邮箱可以单独启用 AI，系统会对退订、垃圾/滥用投诉、产品投诉和其他回信进行分类，使用持久化队列去重并支持重试。明确的退订会将客户退订并加入黑名单，垃圾投诉会同步记录为退信，其余结果保留为审计记录，不会直接修改客户。

设置页还提供网关连通、模型清单和实际分类测试；密钥只用于出站请求，不会回显到页面或日志。

### 组织、权限与媒体库改进

- 平台管理员可以创建组织并设置初始成员，组织管理员可以维护本组织的统一回件邮箱。
- 资源访问同时受工作区、所有权和角色权限约束，角色表单提供高风险权限的明确说明。
- 媒体库支持工作区隔离、嵌套目录和拖放移动；删除用户时会正确清理其个人媒体目录。
- 仪表板按工作区显示活动数据；无权限用户不会订阅错误事件流，空数据图表会显示明确的空状态。

## 安装改进版

### Docker Compose（推荐）

根目录的 `docker-compose.yml` 与上游格式兼容，但其中的默认镜像名仍是 `listmonk/listmonk:latest`；直接使用它会启动官方镜像，而不是本改进版。要运行本仓库代码，请先构建本地镜像：

```shell
git clone https://github.com/Ubanillx/listmonk.git
cd listmonk
make dist
docker build -t listmonk-enhanced:latest .
```

然后在当前目录创建 `docker-compose.override.yml`，将应用服务切换到本地镜像：

```yaml
services:
  app:
    image: listmonk-enhanced:latest
```

启动数据库和应用：

```shell
docker compose up -d
```

默认访问地址为 <http://localhost:39100>。首次启动时可以通过环境变量创建最高管理员：

```shell
LISTMONK_ADMIN_USER=admin LISTMONK_ADMIN_PASSWORD='请替换为强密码' docker compose up -d
```

Compose 配置会自动初始化数据库，并在升级镜像时运行幂等数据库迁移。生产环境请先修改数据库凭据、站点地址、时区和上传目录，并在升级前备份 PostgreSQL。

详见仓库中的 [docker-compose.yml](docker-compose.yml)、[Dockerfile](Dockerfile) 和 [部署说明](deploy/README.md)。上游的[通用安装文档](https://listmonk.app/docs/installation)只适合作为基础参考。

### 二进制安装

当前建议从本仓库源码构建，以确保包含改进版功能：

```shell
git clone https://github.com/Ubanillx/listmonk.git
cd listmonk
make dist
```

如果本仓库已发布二进制版本，也可以从[本项目发行版页面](https://github.com/Ubanillx/listmonk/releases)下载对应平台的文件。不要使用上游发行版替代本项目二进制，否则公海、组织和 AI 回信等改进功能不会包含在内。

1. 生成配置文件并填写 PostgreSQL 连接信息：

   ```shell
   ./listmonk --new-config
   ```

2. 初始化数据库；已有实例升级时使用 `--upgrade --yes`：

   ```shell
   ./listmonk --install
   ```

3. 启动服务并访问 <http://localhost:9000>：

   ```shell
   ./listmonk
   ```

## 文档

- [本项目用户文档](docs/docs/)：安装、配置、活动、客户、组织和 API 使用说明。
- [仓库文档地图](docs/README.md)：网站、用户文档、API 规范和工程记录的入口。
- [工程架构指南](docs/ARCHITECTURE.md)：模块边界、工作区、权限、发送和部署约束。
- [Swagger API 规范](docs/swagger/)：可导入 Swagger/OpenAPI 工具的接口定义。
- [工程状态和技术记录](docs/harness/README.md)：TODO、计划、状态和技术债务。
- [上游用户文档](https://listmonk.app/docs)：官方 listmonk 的基础安装和使用参考。

## 本地开发

开发环境使用 `dev/` 下的 Docker Compose 套件：

```shell
make init-dev-docker
make dev-docker
```

管理端地址为 <http://localhost:9173>，Vite 前端开发服务器地址为 <http://localhost:8181>。常用命令：

| 命令 | 用途 |
| --- | --- |
| `make build` | 编译 Go 后端为 `./listmonk`。 |
| `make build-frontend` | 构建管理端和邮件编辑器。 |
| `make dist` | 构建并将前后端资源打包进二进制文件。 |
| `make test` | 运行 Go 测试：`go test ./...`。 |
| `cd frontend && yarn lint` | 检查 Vue/JavaScript 代码。 |
| `cd frontend && yarn cypress run` | 运行浏览器端到端测试。 |

更多开发约定请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)、[dev/README.md](dev/README.md) 和 [frontend/email-builder/README.md](frontend/email-builder/README.md)。

## 参与贡献

本改进版是自由开源软件。欢迎提交 Issue、改进文档或贡献代码。提交代码前请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)，并确保工作区、权限、迁移和发送行为的改动同时补充相应测试与文档。上游项目归 [knadh/listmonk](https://github.com/knadh/listmonk) 所有，本仓库的扩展代码和文档由本项目维护。

## 许可证

listmonk 使用 [AGPL v3](LICENSE) 许可证发布。
