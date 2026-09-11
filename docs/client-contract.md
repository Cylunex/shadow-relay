# 客户端订阅契约

第一次使用阅读书源发布，先看[书源发布操作与排错](book-publishing.md)。

## 新增发布格式

| 格式 | 消费者与语义 |
|---|---|
| `hub/plugins.json` | relay-bridge 的声明式原生插件清单；只给专门同步绑定此权限，不直接导入阅读 |
| `legado/replace.json` | 阅读净化规则；与 books/rss/tts 一样可一键导入 |
| `iptv/live.txt` | 按 `分组,#genre#` 输出的直播清单，应用与 M3U 相同的频道覆盖规则 |
| `podcasts/feed.xml` | 编排组内播客音频的单条 RSS；音频仍从原 HTTP URL 获取 |
| `mihon/repos.json` | 原仓地址与公开元数据目录；不是可安装的合并扩展仓，需在 Mihon 注册原始仓 URL |

播客还会生成单源 `sources/{id}/podcast.xml`，由 `feeds.opml` 或 Bundle 引用；这两种格式权限均允许获取所引用的单源播客文件。IPTV 在发布物含 XMLTV 时设置 `url-tvg`；只授权 M3U 的绑定仍需要额外勾选 `iptv/epg.xml` 才能获取节目单。

阅读深链遵循 `yuedu://bookSource|rsssource|httpTTS|replaceRule/importonline?src=<URL编码>`。二维码和 HTTP 订阅令牌都属于持有者凭据。漫画仓入口指向上游原仓，Relay 不截留或替换扩展签名信任。

## 两种地址

- 稳定订阅：`GET /p/{token}/shadow.json`，跟随绑定所属编排组的当前发布指针。
- 固定版本：`GET /p/{token}/v/{publicationId}/shadow.json`，内容来源不可变，仍受当前令牌的有效期、格式权限和吊销状态约束。

每种格式都支持稳定和固定版本路径。订阅使用随机 256 位 bearer capability，数据库只保存 SHA-256 摘要。令牌创建/轮换时仅返回一次；控制台用二维码和复制入口帮助导入，不保存令牌到浏览器持久存储。

所有请求在判断 ETag 之前检查令牌与发布所属组。吊销、过期或错误权限统一返回 404，防止通过错误信息枚举发布物。订阅返回 `Cache-Control: private, no-cache, max-age=0` 与按实际响应计算的强 ETag；支持 `If-None-Match` / 304。已经离线保存在设备上的配置无法通过吊销远程擦除，客户端需要自己的退出和本地清理机制。

## Bundle v1

```json
{
  "schema": "shadow.media.bundle/v1",
  "bundleId": "set_example",
  "name": "家庭媒体",
  "publicationId": "pub_example",
  "revision": "sha256:REPLACE_CONTENT_HASH",
  "generatedAt": "2026-09-02T00:00:00Z",
  "providers": [{
    "id": "src_example",
    "name": "主媒体库",
    "mediaTypes": ["video.movie", "video.series"],
    "driver": "emby",
    "mode": "direct-client",
    "endpoint": "https://media.example.com",
    "capabilities": ["browse", "search", "stream", "progress"],
    "priority": 100,
    "weight": 1,
    "role": "primary",
    "health": "degraded",
    "score": 60,
    "credentialMode": "client-local",
    "revision": "rev_example",
    "constraints": {"devices": [], "networks": [], "timeoutMs": 15000, "maxConcurrency": 2}
  }],
  "exports": {}
}
```

`exports` 仅列出实际生成的格式。可选的 `formatWarnings` 按格式说明兼容性限制；例如 Hub 清单只包含可转换插件，不兼容规则的数量通过警告提示，阅读导出独立保留通过校验的原规则。未绑定运行时的 Legado 规则可导出到兼容客户端，但不会伪装成可直接调用的 Bundle Provider；纯规则编排组允许 providers 为空。存储时使用占位基址，返回客户端时替换成显式配置的 `RELAY_PUBLIC_URL` 和本次请求的稳定/固定版本路径，避免从 Host 头生成带令牌的恶意链接。发布内容哈希不依赖绑定令牌；HTTP ETag 依赖最终字节，两者用途不同。

`credentialMode` 当前只支持 `client-local`。不包含 Emby Token、上游 Cookie、用户名密码或一次性配对密钥。运行时请求凭据仅供 Relay 检测/同步使用，不代表客户端自动获得运行时登录状态。

## 发布格式与权限

| 路径 | 内容 |
|---|---|
| `shadow.json` | Bundle v1 |
| `tvbox/store.json` | 多仓入口 |
| `tvbox/{sourceId}.json` | 安全 HTTP 站点配置，继承 `tvbox/store.json` 权限 |
| `iptv/live.m3u` | 合并去重的直播/电台 |
| `iptv/epg.xml` | 合并 XMLTV |
| `legado/books.json` | 书源集合 |
| `legado/rss.json` | 阅读订阅规则 |
| `legado/tts.json` | 朗读规则 |
| `feeds.opml` | Feed 订阅目录 |
| `opds/` | Atom OPDS 目录 |
| `sources/{sourceId}` 和 `sources/{sourceId}/…` | 每个源独立的 Feed、M3U、TVBox、OPML、OPDS 快照，继承 `shadow.json` 权限 |

绑定默认开放全部已知格式，可显式缩减。Bundle 指向的格式也必须在该绑定的允许列表中；客户端应将未授权/未生成格式的 404 当作不可用能力，而不是无限重试。Bundle 的源端点引用独立的版本快照，聚合导出不改变源的边界。设备和网络 constraints 只描述选择偏好，不能替代令牌授权。

## Shadow Media 接入约定

本仓库提供服务端协议与 Schema，**不修改 Shadow Media Android 仓库**。新客户端应：

1. 用户绑定一个 HTTPS 订阅，下载并校验 Schema；只注册自己识别的 driver 与能力。
2. 配置以 `bundleId + provider.id` 为托管命名空间，与本地手工配置分开保存。
3. 下载、验证、构造 Provider 成功后，再原子替换托管层；任何失败保留最后可用版本。
4. Provider 的服务凭据仍在客户端安全存储，不能覆盖为订阅中的任意字段。
5. 尊重 role、priority、健康信息和设备约束；发布的能力不等于客户端已经实现相应阅读器。
6. 不把未识别的 driver 或 TVBox 外部代码装载进持有媒体凭据的主进程。

未来 Work / Unit / Representation / Resource / ProgressAnchor 仍用于内容模型，不进入本版配置发布协议的实现范围。

## 脱敏反馈

`POST /p/{token}/feedback` 接收：

```json
{"publicationId":"pub_example","sourceId":"src_example","code":"timeout"}
```

仅允许 `timeout`、`unavailable`、`parse_error`、`unauthorized`、`unsupported`。令牌须具有 Bundle 权限，源必须存在于该发布物；未知字段直接拒绝。禁止传入完整 URL、内容标题、请求头和 Token。按绑定/源/错误/分钟去重，读取入口为管理员 `GET /api/v1/feedback`。反馈不直接改变健康等级。

## 按类型自动聚合

手工导入且 trust 为 `reviewed`/`trusted`、解析成功时，源会 **自动批准并启用**，再按协议加入系统编排组（不替代手工 SourceSet）。`untrusted`、目录接纳、以及含嵌入凭据/安全失败的导入仍保持隔离或待审。

| 类型 | SourceSet ID | 说明 |
|---|---|---|
| `tvbox` | `aggregate-tvbox` | TVBox JSON/多仓 |
| `iptv` | `aggregate-iptv` | M3U / XMLTV / 直播相关 |
| `novel` | `aggregate-novel` | 小说：legado-book、so-novel、relay-book、OPDS、legado-replace |
| `manga` | `aggregate-manga` | 漫画：mihon-repo（image.comic） |
| `audiobook` | `aggregate-audiobook` | 有声书：legado-tts、podcast（shadow.podcast） |
| `music` | `aggregate-music` | 音乐：lx-music、music-playlist；或 RSS/Atom/JSON Feed/OPML 且 mediaTypes 含 `audio.music` |
| `rss` | `aggregate-rss` | 普通 RSS / Atom / JSON Feed / OPML / legado-rss |
| `other` | `aggregate-other` | 其余协议（如 Emby 直连） |

旧的 `aggregate-legado` 已拆分；升级后调用 `POST /api/v1/aggregates/reconcile`（或重新 enable 源）会按新规则重建成员。客户端只需订阅对应聚合组的稳定地址（`GET /p/{token}/shadow.json` 等），无需手工维护 SourceSet。禁用或删除源时会从聚合组移除并撤回空聚合发布（不再投递旧成员快照）；成员仍在但全部不健康时保留上一份可用发布。运营侧用 `GET /api/v1/aggregates` 查看类型 → 组 ID → 订阅 URL（含系统绑定令牌）。轮换后列表展示 vault 中的现行令牌。系统绑定令牌密文存在 `secrets`（数据目录，不入库）。

### 音乐协议说明

- **`lx-music`**：洛雪音乐用户音源描述符（`schema: shadow.lx-music/v1`）。导出为 `lx-music/sources.json`，不是 Shadow Media Bundle 完整播放 driver；搜索/解析/播放 URL 刷新由兼容洛雪客户端（或后续 OpenSubsonic 路径）完成。文档仅含 `name` / `apiUrl` / `scriptPath`（相对 `data/`，须在 `runtime/lx-music/*.js`）；`apiKey` / `signSalt` / `fingerprint` 只能写入凭证保险库（导入时的 headers），不得出现在源正文或 git。
- **`music-playlist`**：仅含音频扩展名（mp3/m4a/aac/flac/ogg/opus/wav/wma）的 M3U/TXT；未指定 hint 时自动识别。显式 `protocol: m3u` 仍按 IPTV 处理。

