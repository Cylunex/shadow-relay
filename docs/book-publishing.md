# 书源怎样发布给阅读客户端

发布是把已批准的书源规则保存为一个可下载版本。客户端导入的是这个版本中的 `legado/books.json`。发布成功不代表书站搜索或正文已经经过实网验证。

## 第一次发布

1. **导入书源**：得到一个书源记录和待审版本。一个书源记录可以包含多条站点规则。
2. **批准版本**：让待审规则成为该源的生效版本。尚未批准时不能启用。
3. **启用源**：批准与启用是两个动作，只批准仍不会进入发布。
4. **建立编排组**：把要一起给客户端的源加入同一个组。首次建议选一个确定能发布的源，媒体类型选小说，不加语言、地区或设备过滤。
5. **检查发布资格**：默认成员最低健康分是 50；仅结构检查的源通常为 60 分，可以导出阅读规则，但不能据此认定书站可用。隔离、失败、未启用和纯目录模式的源会被排除。
6. **编译并发布**：成功后保存一个不可变版本，并把这个组的稳定订阅指向它。只建组或只保存源不会生成发布版本。
7. **创建客户端绑定**：选择这个组，至少允许 `legado/books.json`，设置有效期，保存当次返回的订阅令牌。
8. **在阅读导入书源地址**：使用 `https://relay.example.com/p/REPLACE_SUBSCRIPTION_TOKEN/legado/books.json`。仅打开 `/p/REPLACE_SUBSCRIPTION_TOKEN` 不是文件地址。

管理员令牌只用于管理 API；客户端用独立的订阅令牌。订阅令牌仅在创建/轮换时显示，丢失后可以轮换，旧地址立即失效。

## 选哪个文件

| 文件 | 用途 |
|---|---|
| `legado/books.json` | 给阅读等兼容客户端导入原始书源规则，是普通书源发布的主要入口 |
| `shadow.json` | 全媒体配置索引；只有规则的组可以没有可调用的 Provider |
| `hub/plugins.json` | 给 relay-bridge 安装声明式 Hub 插件，需要独立的 Hub 和同步工具，不能当作阅读书源导入 |
| 管理接口的 `hub.zip` | 单个源的转换插件下载包；不是一个编排组的稳定订阅 |

Hub 转换失败不必然阻止阅读规则导出。不支持的规则不进入 Hub 插件清单，`formatWarnings` 返回跳过数量；完整原因在源的 `book-plugins` 接口。清单可为零插件，用于同步时清理同一编排组曾安装的插件；零插件不能理解为书站已经可用。

阅读格式仍然执行发布安全校验。如果真实数据含当前不接受的 URL 或凭据结构，会明确拒绝，并保留旧发布。本版本没有自动删改这些规则来凑出“成功”结果。

## 为什么改了书源，客户端还没变

```text
修改/同步规则 → 新待审版本 → 批准新版本 → 再次发布 → 客户端刷新稳定订阅
```

待审版本不会进入发布。批准也不会直接修改已有的发布文件。自动发布只在已开启时定时执行，并受最少可用源数和最多排除比例约束；它不会替你批准待审规则。

| 操作 | 稳定订阅的变化 |
|---|---|
| 修改或同步出待审规则 | 保持旧发布 |
| 批准规则但未重新发布 | 保持旧发布 |
| 再次发布成功 | 切换到新发布 |
| 编译/资格检查失败 | 保持旧发布 |
| 回滚发布版本 | 切回选中的历史发布 |
| 回滚源版本 | 源恢复旧规则并固定，仍须再发布才影响订阅 |
| 吊销或轮换绑定 | 旧令牌的稳定和固定版本地址都不可再访问 |

稳定地址为 `/p/{token}/legado/books.json`；固定版本地址为 `/p/{token}/v/{publicationId}/legado/books.json`。固定版本规则不随新发布变化，但仍受令牌权限、过期和吊销限制。已离线下载到客户端的配置不会被远程擦除。

## 只调接口的顺序

以下管理请求都带 `Authorization: Bearer REPLACE_ADMIN_TOKEN`，写请求带 `Content-Type: application/json`。所有 ID 均使用前一步返回的实际值。

| 步骤 | 接口 | 主要请求内容 |
|---|---|---|
| 导入 | `POST /api/v1/sources/import` | `name`、`protocol: legado-book`，以及 `url` 或 `content` |
| 批准 | `POST /api/v1/sources/{id}/approve` | `{}`，或指定待审 `revision` |
| 启用 | `POST /api/v1/sources/{id}/enable` | `{}` |
| 建组 | `POST /api/v1/source-sets` | `{"name":"我的书源","members":[{"sourceId":"REPLACE_SOURCE_ID","minScore":50}]}` |
| 预览 | `POST /api/v1/source-sets/{id}/preview` | `{}`；返回文件类型/大小、纳入版本、排除原因和兼容警告，不写入发布历史 |
| 发布 | `POST /api/v1/source-sets/{id}/publish` | `{}`；成功为 201，返回发布 ID 和实际文件 |
| 绑定 | `POST /api/v1/bindings` | `name`、`setId`、`formats: ["legado/books.json"]`、未来的 RFC3339 `expiresAt` |
| 下载 | `GET /p/{token}/legado/books.json` | 无需管理员令牌，返回 JSON 数组 |

预览和发布共用编译逻辑；预览后如果源状态发生变化，以实际发布结果为准。成功发布的 `exclusions` 可能非空，说明只有合格成员进入新版本。

失败返回中的 `exclusions` 按源 ID 标注 `disabled`、`no_approved_revision`、`below_minimum_health`、`catalog_only`、`media_filtered`、`item_filter_empty`、`runtime_unavailable` 等原因；`sourceErrors` 定位书源规则校验失败。用 `GET /api/v1/sources` 把 ID 对应回源名称。

订阅 404 的常见原因是格式未生成、绑定未允许该格式、令牌过期/已吊销、组还没有发布，或固定版本属于另一个组。绑定允许某格式，不会自动让发布生成该格式。
