# 适配器与运行时支持矩阵

## 内置配置适配器

| 协议 | 导入与结构校验 | 当前体检深度 | 发布 |
|---|---|---|---|
| M3U / TXT | EXTINF 属性、分组、相对地址解析、地址检查 | 首个频道 Range/首包；HLS 继续检查首个条目 | M3U + Bundle |
| XMLTV | XML 语法、频道 ID、programme 频道关联、start/title | 结构 | 合并频道/节目、XMLTV + Bundle |
| TVBox JSON/JSONC / 多仓 | 注释/尾逗号、HTTP 站点与仓地址 | 首个安全 CMS 的 class/list JSON | 多仓与安全站点配置 + Bundle |
| Legado book / RSS / TTS | 必需字段、显式凭据排除，规则仅作为数据，Relay 不执行 | 结构；绑定运行时时额外检查服务 API | 可直接导出到兼容客户端；绑定合适运行时后再加入 Bundle Provider |
| RSS / Atom / JSON Feed | 标题/版本/条目、链接 | 首个正文/附件地址可达 | OPML、内联 Feed 文件、Bundle |
| OPML | 递归订阅与分类 | 结构 | 去重 OPML + Bundle |
| OPDS 1 / 2 | Atom / JSON 目录，条目与链接 | 首个条目地址可达 | 统一 Atom OPDS 目录 + Bundle |
| Shadow Bundle v1 | schema、白名单 driver、执行方式、端点 | 结构 | 合并 Provider 描述，保留客户端本地鉴权 |
| Mihon repository | meta / sources 仓库元数据 | 结构；绑定 Suwayomi 后检查服务 API | 运行时 Provider；不下载、安装或重打包扩展 |
| 名称/URL JSON 目录 | `[{name,url,protocol?}]`、`{entries:[...]}`、`{sources:[...]}` | 结构 | 候选箱，不作为内容发布格式 |

单个输入最多 8 MiB，规范化条目最多 20,000 条，XML 嵌套最多 64 层。XML DTD/外部实体/非 XML 处理指令被拒绝。TVBox type 0/1 HTTP CMS 可进入安全配置；JAR、csp、type 3/4 和解析脚本不会发布到安全 TVBox 配置中。JSONC 转换不修改字符串内的 URL 和注释样式文字。

TXT 接受 `频道,URL` 与 `分组,#genre#`。不支持 RTP/UDP/RTSP、登录态写在 URL 的流、Xtream 密码路径或需要播放器自定义 Cookie 指令的列表；应使用已认证的专用直播运行时。

第三方目录各自的分类 JSON 不存在统一标准。yuanc 类目录须先转换为上面的 name/url 结构；不声称能直接解析任意社区仓库的页面或所有私有目录格式。

OPDS 输出是聚合导航/获取链接目录，不缓存 EPUB、PDF 或封面，不代理第三方阅读鉴权。媒体服务、Feed 以及受保护下载仍可能需要客户端自己的凭据。导入其他 Relay Bundle 不继承原发布物的客户端访问令牌。

## 外部运行时连接器

| 服务 | 连接检测 | 状态读取 | 认证输入 |
|---|---|---|---|
| Emby / Jellyfin | `/System/Info/Public`，验证 Version | `/Library/VirtualFolders` | 加密请求头，例如 X-Emby-Token |
| Dispatcharr | `/api/core/version/`，验证 version | `/api/channels/channels/` | 加密 Authorization |
| LegadoHub | `/api/auth/entrypoint`，验证 entrypoint | 同接口，记录入口状态 | 按服务配置的加密请求头 |
| Suwayomi | `/api/v1/settings/about`，验证 name | `/api/v1/source/list` | 加密 Authorization |
| Audiobookshelf | `/api/libraries`，验证 libraries | 同接口，记录数量 | 加密 Bearer Authorization |
| Miniflux | `/v1/me`，验证 id | `/v1/feeds` | 加密 X-Auth-Token |

`GET /api/v1/adapters` 返回解析器和连接器的自描述信息。运行时状态只保存版本、数量和检查深度，不把上游完整用户资料、内容标题或 Token 保存在状态接口里。HTTP 200 登录页面不算成功。

运行时接入是连接、能力映射、服务检测和 **pull-only 状态同步**。安装扩展、推送源规则、小说全文探测、漫画分页和 TTS 合成等由原生后台完成。各项目的写 API 和登录模型不同，Relay 没有伪造一个通用 pushConfig，也没有把服务检测称作搜索到正文的完整测试。尤其 LegadoHub 的读者专属 code 链接与管理入口不同，不会把 code 复制进公共 Bundle。

连接器通过契约模拟响应回归测试；实际版本升级后应在自己的运行时上检测兼容性。

## 参考接口

- [Suwayomi Global API](https://github.com/Suwayomi/Suwayomi-Server/blob/master/server/src/main/kotlin/suwayomi/tachidesk/global/GlobalAPI.kt)
- [Suwayomi Manga API](https://github.com/Suwayomi/Suwayomi-Server/blob/master/server/src/main/kotlin/suwayomi/tachidesk/manga/MangaAPI.kt)
- [LegadoHub 文档](https://github.com/XziXmn/legado-hub)
- [Audiobookshelf API](https://api.audiobookshelf.org/)，其文档说明部分内容不再维护，接入以返回结构验证为准。
- [Dispatcharr](https://github.com/Dispatcharr/Dispatcharr)
