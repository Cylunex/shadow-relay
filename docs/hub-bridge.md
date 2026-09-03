# 原样 Hub 的书源接入链路

Relay 负责规则管理和插件交付，LegadoHub 继续负责服务端聚合搜索、章节缓存、换源和给阅读生成一条专属书源。无需 Fork Hub，也不把阅读 JSON 当作 Python 插件直接导入。

## 从规则包到阅读客户端

1. 在「参考与接入」选择 so-novel 配方，或在「书源工坊」粘贴自己的阅读 JSON。先看逐条兼容报告。
2. 批准并启用源，加入一个编排组并发布。建立只允许 `hub/plugins.json` 的专用客户端绑定，用于同步工具。
3. 将同步工具输出的 `thirdparty` 目录作为 Hub 的第三方插件覆盖挂载。工具生成 `relay_<稳定站点ID>` 目录中的 `source.py`、`metadata.yaml`、`recipe.json`、README 和 smoke 草稿。
4. 在 Hub 原生后台重新加载插件，或为同步工具显式配置 Hub 管理请求头后自动调用热加载接口。Hub 用户授权及阅读专属链接仍在 Hub 中管理。
5. 阅读只导入 Hub 的那一条专属源；实际搜索和正文请求由 Hub 调用这些插件。
6. 在 Relay 的源设置中关联 LegadoHub，填写体检书名与定时体检间隔。Relay 生成的插件自动匹配；已有手写插件需要填写插件 ID。

「书源工坊 → 下载 Hub 插件」默认下载待审版本，适合检查后手动安装。发布订阅只包含已经批准并符合编排条件的版本，且不输出源包中的 Python/JS。

## 转换范围

| 输入 | 支持 | 明确不自动转换 |
|---|---|---|
| 阅读 JSON | searchUrl 的 `{{key}}` / `{{page}}`；CSS 与简单 class/id/tag；`@text`、`@html`、`@href`、`@src`；基础 JSONPath；四阶段规则块 | JS、XPath、默认选择器链/位置语法、变量存取、登录、搜索 URL 内嵌请求配置 |
| so-novel | `bundle/rules/*.json`；GET/平面表单 POST；`%s`；CSS、OpenGraph 详情、目录倒序与分页、正文 HTML 清理 | 脚本式 URL/响应转换、Cookie、URL 正则改写、动态签名 |
| Relay recipe | 完整声明式四阶段结构，精确域名集合、表单、页面上限、最小请求间隔、繁简转换标记 | 自定义执行代码 |

JSONPath 子集支持 `$`、`.field`、`[0]`、`[*]`；不支持递归下降、过滤器、函数和脚本。CSS 语法最终以 Hub 的 lxml/cssselect 支持范围为准。不会执行 `replaceRegex`、`filterTxt` 等导入正则；报告会标注未应用的净化规则。HTML 内容仍经 Hub 的 `clean_html` 清理。书名详情选择器为必需项，避免生成能搜索却无法打开详情的空插件。

正文分页只跟随同一路径的分页参数，或在原始章节文件名后追加 `_2` / `-2` 等页码的地址，并保持非分页查询参数一致。不会通过去掉章节编号把 `chapter-1.html` 与 `chapter-2.html` 拼接；首页自身带 `_1` 而后续页替换该编号的站点需要手工插件。目录分页循环、重复页面或达到页面上限会报错，不把截断结果宣称为完整目录。单页最多 4 MiB、目录最多 20,000 章，单章最多 20 页 / 1,000,000 字符。插件声明每主机并发 1、最小间隔至少 1,200 ms，由 Hub 管理限速和出站请求。

每个插件使用源 ID 与站点 URL 生成稳定 ID，规则或执行模板变化会改变 metadata version。一个同步清单最多 500 条（包含不兼容条目）；建议从少量站点开始。多个阅读规则包中同一站点保留独立源身份，单包内相同站点会报告重复。

so-novel 的 Cookie 字段在规范化时删除，并把对应规则标记为需要人工认证；原始响应只存在加密快照中。公共插件清单不含 Cookie。配置中其他显式凭据仍被导入检查拒绝。

## 同步工具

```sh
go build -o bin/relay-bridge ./cmd/relay-bridge
# 用忽略的 .env 或进程管理器注入下列变量，不把真实值提交到仓库。
export RELAY_BRIDGE_SUBSCRIPTION_URL='https://relay.example.com/p/REPLACE_BINDING_TOKEN/hub/plugins.json'
./bin/relay-bridge --output /path/to/shared-plugins/thirdparty --interval 15m
```

`--interval 0` 只同步一次。输出根目录不能经过符号链接。目录所有者应与 Hub 中读取插件的进程一致；文件权限 0600、目录 0700。

同步工具只接受声明式 recipe，然后用本程序内置的模板生成 Python，绝不下载源包提供的 `source.py`。它只替换或清理带有本编排组 `.shadow-relay.json` 所有权凭据的目录，不覆盖手写插件或其他编排组的目录。每个目录通过临时目录与 rename 替换，失败尝试恢复旧目录；全部处理成功后才调用 Hub 热加载。插件内容不变时不会重写，不丢失已采集的 fixture。规则更新时重新生成 smoke 草稿。

并发同步通过 `.relay-sync-lock` 目录拒绝。若进程崩溃留下锁，先确认没有同步进程、检查目录与上次日志，再由操作者移除该锁。同步采用单目录替换，某个目录失败时此前已成功的目录可能已经更新；全批次完成前不会调用热加载，下次同步继续收敛。配置了 Hub 地址时，工具每次启动后的首次成功同步也会重新加载，恢复“文件已更新但上次热加载未确认”的状态。它不自动重启 Hub。绑定过期或吊销阻止后续拉取，不会远程销毁 Hub 已安装的插件。

可选环境变量：

| 变量 | 用途 |
|---|---|
| RELAY_BRIDGE_NETWORK | Relay 订阅的 `internet` / `trusted-lan` 策略 |
| RELAY_BRIDGE_TRUSTED_CIDRS | 明确放行的内网网段；留空拒绝私网 |
| RELAY_BRIDGE_HUB_URL | 配置后启用热加载；留空仅交付文件 |
| RELAY_BRIDGE_HUB_NETWORK | Hub 网络策略，默认与订阅相同；Compose 示例默认 trusted-lan |
| RELAY_BRIDGE_HUB_HEADERS | JSON 格式管理请求头，例如 Cookie；仅存本机秘密配置 |

可选容器配方在 `compose.bridge.yaml` / `Dockerfile.bridge`，需要明确运行 `hub-bridge` profile。它不包含或启动 Hub，不开放新端口。挂载专用父目录是为了在同一文件系统上创建临时目录；Hub 只挂载其中的 `thirdparty` 子目录。当前没有实际构建或部署这个配方。

## 实网体检与 fixture

Relay 采用 Hub 实际接口：创建 `/api/console/search-jobs` → 查询任务/候选 → 调用 `/candidates/{id}/verify`。核对生成插件版本后，检查搜索结果、详情、目录、抽样正文、目录唯一 URL 与连续序号；若详情有最新章节，再与目录尾章核对。只保存阶段标签、延迟和错误类别，正文、书名及凭据不进入体检历史。

一个源包每轮轮换抽一条可转换插件，**不是每次检查整个包**；多个周期覆盖不同站点。无最新章锚点时会标记需要 fixture 证明完整性。尾章一致也不能证明中间无缺章，因此不把 live smoke 当作严格的目录完整验收。

书源包的新上游规则一律进入待审版本。不能用已加载旧插件或只抽到一条规则的结果批准整包新规则。自动发布只搬运已批准版本；它不绕过规则审核。体检持续失败会降分和隔离，隔离后需要管理员处理并释放。

插件包里的 smoke 文件明确为 **草稿**，没有伪造真实响应。需要采集时，在已有 Hub Python 环境显式执行：

```sh
PYTHONPATH=/path/to/hub/backend python integrations/hub/capture_smoke.py \
  /path/to/shared-plugins/thirdparty/REPLACE_PLUGIN_ID \
  --keyword 'REPLACE_BOOK_TITLE' --expected-count 123
```

`--expected-count` 应由独立查看站点完整目录得到；不提供时 fixture 仍标记为待确认。工具通过 Hub 上下文记录真实四阶段及分页响应，生成目录精确数量、首尾标题和去重/连续序号断言，不会自动运行后续 fixture 测试。采集文件含第三方内容，保留在本机运行时目录，不提交源站正文。

## 兼容性与安全边界

接口按 [Hub 插件契约](https://github.com/XziXmn/legado-hub/blob/cbcdfdf5cf4626c47f4b74ed9745e5ad69d8e437/docs/architecture/source-plugin-contract.zh-CN.md) 与 [console.py](https://github.com/XziXmn/legado-hub/blob/cbcdfdf5cf4626c47f4b74ed9745e5ad69d8e437/backend/app/api/console.py) 实现。Hub 的管理 Cookie 与读者专属 code 不是同一权限。Relay 不伪造永久管理 Token，也不在订阅中携带 Hub 管理凭据。

Relay 及同步工具的出站请求使用 Relay DNS/IP 检查。生成插件的站点请求则委托给 **Hub 的访问上下文**，其网络隔离、重定向与 DNS 策略由 Hub 容器/运行环境承担；不能把 Relay 的 SSRF 防护等同于 Hub 的防护。插件本身限制提取出的链接域名，复杂认证和浏览器仍由人工配置的外部运行时处理。


## 书源代理

通过源管理 API 设置 `hubProxyMode: "always"` 后，生成插件声明 `proxy.mode: always`、`required: true`；`never` 恢复直连。插件仍只通过 `ctx.access` 请求，实际代理地址由 Hub 本机配置提供。配置变化会更新插件版本，需要重新发布和同步；Hub live smoke 会拒绝仍加载旧版本的插件。Relay 拉取源文件使用独立的 `proxyId`，两者不会影响 Relay 到 Hub 的管理连接。具体环境变量、请求示例和范围见开发文档。


发布清单 `entries` 只包含可安装的插件，最多 500 条；`unsupported` 汇总不兼容数量，详细报告仍从源管理接口查看。不兼容条目不再占用安装清单名额或携带表达式到订阅。若本次全部不兼容，返回零插件清单，以便同步工具清理本组之前安装的插件；这不表示实网书站可用。阅读书源导出独立执行自身校验。
