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
