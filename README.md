# Shadow Relay

**全媒体源编排与发布中心。** 把看、读、听、说的源放进一个私有空间，从发现、审核、体检到编排和发布，让客户端订阅一份可追溯的配置。

Relay 管理源和能力；Emby、LegadoHub、Suwayomi 等领域运行时执行内容能力；Shadow Media 与其他客户端负责呈现和消费。

## 主要功能

- 中文响应式控制台：总览、源库、候选箱、编排组、发布与绑定、运行时、任务审计。
- URL、文件与正文导入，自动识别 TVBox JSON/JSONC、多仓、M3U/TXT、XMLTV、Legado、Feed、OPML、OPDS 和 Bundle。
- 候选与正式源隔离，版本审核、差异、固定与回滚；危险更新保留最后批准版本。
- PostgreSQL 持久队列、定时同步、条件请求、抽样体检、失败退避与隔离。
- 按优先级、主备角色、媒体类型和健康分编排，多格式不可变发布与稳定订阅。
- 独立客户端令牌、格式授权、二维码、过期、轮换和吊销。
- 加密凭据与原始快照、审计、出站地址检查和脱敏客户端反馈。
- 连接 Emby、Jellyfin、Dispatcharr、LegadoHub、Suwayomi、Audiobookshelf、Miniflux，检测 API 与拉取状态。

## 开发入口

需要 Go 1.26、Node.js 22.12+ 和 PostgreSQL 16+。

```sh
cp .env.example .env
# 在被忽略的 .env 中配置开发数据库、公开访问基址和独立密钥。
go run ./cmd/relay keygen
make build
# 应用仅从环境变量读取配置；由本地 shell 或进程管理器加载 .env。
make dev
```

前端开发：`npm run dev --prefix web`。验证：`make test`；设置 `RELAY_TEST_DATABASE_URL` 后会运行真实 PostgreSQL 集成测试。完整浏览器流程见开发文档。

## 文档

- [交付范围与验证状态](docs/implementation-status.md)
- [设计与状态模型](docs/design.md)
- [支持矩阵与执行边界](docs/adapters.md)
- [开发、配置与容器使用](docs/development.md)
- [客户端协议与接入](docs/client-contract.md)
- [安全与恢复](docs/security.md)
- [OpenAPI](contracts/openapi.json) · [Bundle Schema](contracts/shadow-media-bundle.schema.json) · [Source Schema](contracts/source.schema.json)

本仓库实现源控制台与运行时连接层。Shadow Media 托管模式、跨源内容搜索、内容代理、规则执行、听读进度联动属于后续独立客户端/内容层工作；当前没有在管理界面中伪装成已经可用的功能。实际部署资料和秘密保存在仓库之外。
