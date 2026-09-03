# 开发与运行

## 配置

配置仅从环境读取，应用不会自动加载 `.env`。不要把真实域名、账号、数据库连接串或密钥写入仓库。

| 变量 | 说明 |
|---|---|
| `RELAY_DATABASE_URL` | 必填，PostgreSQL 16+ 连接串 |
| `RELAY_MASTER_KEY` | 必填，Base64 编码的 32 字节 AES 主密钥 |
| `RELAY_ADMIN_TOKEN` | API 模式必填，至少 32 字符随机值；不是客户端订阅令牌 |
| `RELAY_PUBLIC_URL` | 客户端可访问的服务 origin，生产必须显式设置；用于生成订阅内的链接，拒绝从用户请求 Host 推断 |
| `RELAY_DATA_DIR` | 加密快照目录，默认 `data` |
| `RELAY_WEB_DIR` | 已构建前端目录，默认 `web/dist` |
| `RELAY_LISTEN` | HTTP 监听地址，开发默认仅回环 |
| `RELAY_TRUSTED_CIDRS` | 逗号分隔的显式受信内网网段，默认空 |
| `RELAY_WORKERS` | 1–16，默认 2；单进程 Worker 并行度 |

`go run ./cmd/relay keygen` 生成新的主密钥和管理员令牌。将结果保存在被忽略、权限 0600 的本地配置中。不要在已有实例上随意替换主密钥，否则无法解密原凭据和快照。

执行本地开发前，可在可信的本地 shell 中导入 `.env`：

```sh
set -a
. ./.env
set +a
make dev
```

`npm run dev --prefix web` 将 `/api`、`/p` 代理到本机开发 API；生产使用同源打包界面。前端不依赖外部字体或 CDN。

## 容器

根目录 `compose.yaml` 是脱敏、可配置的通用示例，不包含真实运维映射。配置独立数据库密码、与之匹配的 `RELAY_DATABASE_URL`、主密钥、管理员令牌和外部 origin 后：

```sh
docker compose config --quiet
docker compose up --build -d
```

此命令是使用说明，不表示已替你部署。Relay 容器以非 root 用户运行，根文件系统只读，删除 capabilities，独立持久卷保存快照；PostgreSQL 不对宿主发布端口。公网入口由已有 TLS 网关承担，API 和订阅接口不要开启记录完整路径的访问日志。

`relay serve` 适合单实例。需要单独 Worker 时，API 运行 `relay api`，后台进程运行 `relay worker`，二者使用同一数据库、主密钥和快照卷。迁移会在启动时加锁执行，也可单独运行 `relay migrate`。

容器文件通过 Linux 交叉编译与构建配方检查；没有 Docker 的环境不会宣称已实际构建或启动镜像。

## 测试

```sh
# 无数据库时明确跳过集成测试；网络安全测试使用受控本机 HTTP 服务。
go test -race ./...
go vet ./...
npm ci --prefix web
npm test --prefix web
npm run build --prefix web
npm run format:check --prefix web
```

完整数据库回归必须设置 `RELAY_TEST_DATABASE_URL`，使用专用测试数据库账户。每个测试创建独立 schema，结束后清理，覆盖迁移重入、候选幂等、审核、更新差异、失败隔离、任务租约、并发领取、发布原子性、令牌权限和原文访问。

浏览器测试使用已经启动的**隔离实例**，会真实创建测试源、编排组和绑定。设置 `RELAY_TEST_ADMIN_TOKEN`，可选 `RELAY_TEST_URL`，然后运行：

```sh
cd web
npx playwright install chromium
npm run test:e2e
```

可用 `RELAY_BROWSER_PATH` 指定本机 Chromium/Chrome。测试覆盖导入、审核、启用、编排、发布、二维码、订阅读取、吊销后的条件请求以及移动端横向溢出。截图放在被忽略的 `web/test-results/`。CI 在手动触发或 PR 时启动专用 PostgreSQL、生成临时密钥并验证以上流程；不上传可能包含订阅令牌的截图或 trace。

## 目录

```text
cmd/relay/          CLI 与进程生命周期
internal/adapter/   纯数据协议解析与差异
internal/fetch/     唯一出站 HTTP 边界
internal/security/ 加密、内容寻址、凭据校验
internal/model/    领域模型
internal/store/    事务与持久化
internal/service/  审核、更新、编排、发布、连接器、任务
internal/httpapi/  管理 API、订阅、静态界面
migrations/        PostgreSQL 迁移，嵌入二进制
contracts/         JSON Schema 与 OpenAPI
web/               React / TypeScript 控制台
```

当前用于私有个人规模，历史永久保留；需自行监控磁盘容量。全局写事务锁、内存编译和部分管理列表全量读取不适合大型公共目录市场。多管理员协作权限、保留策略、S3 快照和分布式 Host 限流属于后续扩展。


## 按源配置代理（API）

Relay 拉取源文件的代理与 Hub 访问书站的代理分别配置。默认直连，不继承进程的 `HTTP_PROXY`、`HTTPS_PROXY` 或 `NO_PROXY`。

在被忽略的 `.env` 或服务器秘密环境文件中设置以下变量，然后重启使用该配置的 API/worker：

```sh
RELAY_PROXIES='{"books":"http://REPLACE_USER:REPLACE_PASSWORD@proxy.example.com:8080"}'
```

支持 HTTP、HTTPS CONNECT 代理，包括代理 Basic 认证；账户密码中的特殊字符须按 URL 编码。HTTP 目标也使用 CONNECT，因此代理需要允许对应目标端口。当前不支持 SOCKS 代理。配置最多 32 项，ID 只包含字母、数字、下划线、连字符，长度不超过 64。真实地址和凭据不得提交。

管理接口均使用 `Authorization: Bearer REPLACE_ADMIN_TOKEN`：

| 接口/字段 | 用法 |
|---|---|
| `GET /api/v1/proxies` | 返回 `{"proxyIds":["books"]}`，不返回地址或密码 |
| 导入/预览/转换的 `proxyId` | 选择服务器配置；省略或 `""` 为直连 |
| `PUT /api/v1/sources/{id}` 的 `proxyId` | 省略保留已有配置，`""` 恢复直连，未知 ID 拒绝保存 |
| 源的 `hubProxyMode` | `never` 为 Hub 书站直连，`always` 使用 Hub 本机代理；更新省略保留 |

代理 ID 应用于该源的 URL 预览、导入、定时/手动同步与由 Relay 发起的内容抽样。仅 `internet` 源可用；Hub 管理 API、目录订阅和其他源不自动继承。配置不可用时请求失败，避免悄悄切换出口。

书源示例请求：

```json
{
  "name": "示例书源包",
  "url": "https://books.example.com/sources.json",
  "protocol": "legado-book",
  "proxyId": "books",
  "hubProxyMode": "always"
}
```

`hubProxyMode` 适用于 Relay 生成的 Legado/so-novel/relay-book 插件，转换报告、ZIP 和发布的 `hub/plugins.json` 均带对应策略。代理策略变化会改变插件版本及自动发布输入签名，后续须重新发布并同步到 Hub。Hub 的 `config/app_config.json` 需要启用 `proxy.enabled` 并配置 `proxy.url`；Relay 不向 Hub 传递代理地址/凭据。指定现有手工 `hubPluginId` 的源应直接在 Hub 配置该插件，Relay 拒绝代改其代理策略。传统 `legado/books.json` 不注入运行时代理设置。

接口验证可运行 `go test -race ./...`；设置 `RELAY_TEST_DATABASE_URL` 后书源代理 API 测试会使用独立 schema，覆盖预览、导入、编辑、同步和 ZIP 配置。无需启动浏览器。
