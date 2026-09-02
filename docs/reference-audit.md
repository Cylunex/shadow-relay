# 原始沟通记录对照

2026-09-02 对照初版实现、原始沟通记录及关键上游代码。控制台「参考与接入」保留项目清单、能力去向和可选导入配方；不会把“列出了参考链接”算成接入成功。

## 本轮补齐

| 原始诉求 | 初版缺口 | 本轮实现 |
|---|---|---|
| 客户端一条书源，服务端处理多源 | 只有配置聚合、Hub 存活检测 | 原生 Hub 插件转换、交付与挂载同步；Hub 负责实际搜索、正文、缓存及换源 |
| 不 Fork Hub | 没有规则到插件的接缝 | 稳定插件 ID、版本指纹、独立 relay-bridge、显式热加载 |
| 只提供书源即可维护 | 阅读 JSON 无法直接进入 Hub | 阅读静态子集、so-novel、Relay recipe 三种输入；逐条兼容报告和插件 ZIP |
| 站点 URL + 选择器生成源 | 无 | 可视化脚手架、JSON recipe、四阶段插件、真实 fixture 采集工具 |
| 真实体检 | 主要是结构和连接检测 | Hub 搜索、详情、目录、正文；版本校验、轮换抽样、目录去重与尾章对比 |
| 少手工维护 | 固定源不再同步，也没有独立体检周期 | 独立体检周期；健康降分/隔离；可用率门槛下定时发布已批准变化 |
| 类似 yuanc 的统一目录 | 通用 name/url 目录，不能直接读取 yuanc | 原生 name/link、m3u-link/txt-link、项目根 data/ 地址、路径类别；六个目录配方进入候选箱 |
| 净化规则 | 缺协议与导出 | legado-replace、legado/replace.json、阅读导入链接 |
| TVBox 多仓整理 | 多仓输出存在，重复地址仍可能重复 | 合并后的多仓 URL 去重；继承编排优先级与审核 |
| 直播频道编排 | 只聚合清单 | 频道名称、分组、台标、EPG ID、隐藏覆盖；M3U/TXT 双导出与 EPG 关联 |
| 有声书/音频转订阅 | 能导入 RSS，不能制作 | 播客清单编辑器、标准 RSS、单条合并 feed、OPML；本地源内容可提交新版本 |
| 漫画仓 | 只接受自拟 meta/sources 结构 | 真实 repo.json/index_v2、旧 JSON 数组识别；保留原仓元数据与签名入口 |
| 一键导入 | 只有复制链接/QR | 阅读书源、RSS、TTS、净化规则 yuedu 深链；TVBox/IPTV 继续复制标准 URL |

## 核对中纠正的信息

- so-novel 真实种子目录是 [`bundle/rules/`](https://github.com/freeok/so-novel/tree/76150dbd2827b4de97cde83cffb3ee367bc9be3a/bundle/rules)，不是仓库根 `rules/`。其规则包含 POST 表单、Cookie、脚本和 URL 改写，不能宣称整个 main.json 都能直接转换。
- Hub 的 [`sourceSeed`](https://github.com/XziXmn/legado-hub/blob/cbcdfdf5cf4626c47f4b74ed9745e5ad69d8e437/docs/architecture/source-plugin-contract.zh-CN.md) 是元数据，不是自动导入执行器。实现必须生成 `Source` 的四个 async 方法，并使用宿主 `ctx.access`。
- [yuanc 的书源目录](https://github.com/52liulian/yuanc/blob/f2c1fd43f39ae9f1d7383940e2cc7c70165d915b/data/legado/books.json) 用 `link` 字段；目录 JSON 与被指向的阅读规则 JSON 是两层数据。
- [Keiyoushi 的 repo.json](https://raw.githubusercontent.com/keiyoushi/extensions/repo/repo.json) 已使用 `index_v2` 指向 `index.pb`。[旧 index.min.json](https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.min.json) 当前主要提示客户端升级，不能再把它当作完整扩展列表。
- [LegadoParser 的公开接口](https://github.com/821938089/LegadoParser#高级api) 是 Python 库调用，不是一个统一的 HTTP 服务地址。Relay 没有将它误报为已连通的即插即用服务。

## 各组参考的去向

| 项目 | 实际采用方式 |
|---|---|
| LegadoHub / book-source-craft / create_source_plugin / validate_source_plugin | 核对原生插件与 smoke 契约；Relay 自行实现声明式转换和标准插件模板，不复制上游实现代码 |
| so-novel / XIU2 / aoaostar / tickmao / shidahuilang | 规则上游；精选包经预览、审核与兼容报告后使用。只有已核对的 so-novel 文件提供预填地址，其余由用户选文件 |
| PixivSource | 原始阅读规则可管理；登录、复杂 JS 明确不能自动生成原生插件 |
| LegadoParser / reader-dev | 保留为复杂规则的可选外部执行器参考。本轮没有执行其 JS、浏览器、OCR 或未核实的 HTTP API |
| funread / xin-verify-book-source | 采用预览、去重、校验思路；功能体检深入到四阶段，主页可达不等于正文可用 |
| legadoSkill / legado-book-source-skill / book-source-creator-skill / legado-source-generator | 生成结果可进入书源工坊；不自动安装第三方技能、不向外部 AI 发送站点凭据 |
| yuanc / ZGQ source | 统一分类、目录配方、一键导入；补全净化、TTS、RSS、漫画仓类别 |
| wzh15802/tvbox / tvbox-ui / TVBox-Suite / tvbox_config | 多仓顺序、去重、生成订阅和定期探活；不再搭建第二套 PHP/Cloudflare 管理后台 |
| EcoHub | 接收标准 TVBox 输出；CMS、影视入库与站点爬虫属于上游服务 |
| akiralereal/iptv / taksssss/iptv-tool / Guovin/iptv-api / super321/iptv-tool / big-mouth-cn/tv | M3U/TXT/XMLTV 输出接入；Relay 增加频道覆盖、EPG 指针、发布权限。全量测速、转码和直播代理仍在领域服务 |
| fanmingming/live / iptv-org/iptv / yuanzl77/IPTV | 可选源池，按地区/语言挑选；无默认全网导入或扫描 |
| folder2podcast | 导入其 RSS；Relay 自行补齐“已有 HTTP 音频清单 → RSS”链路，文件目录扫描仍交给上游 |
| Miniflux / FreshRSS | Miniflux API 检测与状态已有；双方通用 RSS/Atom/OPML 互通。没有混用登录协议 |
| keiyoushi / Uchiyomi / Suwayomi | 管理漫画仓 URL/索引；Suwayomi 状态连接已有。安装扩展、签名信任和分页阅读由漫画运行时负责 |
| Komga / Kavita / Talebook | 通过 OPDS 连接本地书库；文件入库与权限继续由原服务负责 |
| reader-rust / hectorqin reader / ColorTxt / 轻阅读 | 同类阅读服务替代方案，未在 Hub 旁重复实现第二套书架和缓存 |
| FongMi / TVBoxOSC / 阅读 | 直接消费标准导出；不把 Android 客户端合进 Relay 或在本轮构建 APK |
| legado.koplugin | 阅读 Web API 客户端参考，不能因名字相似就宣称兼容 Hub |
| Tthfyth/source / xbsrebuild | 另一类源格式转换，不冒充 Hub 插件转换能力 |
| Browserless / Playwright / CloudflareBypassForScraping | 可选受控浏览器环境；本轮不安装或部署浏览器，也不实现付费/账号绕过 |
| Caddy / Nginx | 通用外部反向代理，不写入真实部署域名或配置 |
| MediaStationGo / 淳渔 CMS | 原始沟通已明确不合并的自建片站方向，未纳入源控制面 |

“协议接入”表示 Relay 能消费该服务的标准输出，不表示已为其所有管理 API 写了专用连接器；界面使用同样的标记。无法通用化的认证、完整 JS 引擎、扩展安装、跨媒体全文搜索和听读进度同步没有伪装为已完成。

本轮没有运行单元、集成、浏览器、实网或容器测试，新增回归用例保留待执行。关键运行链路的代码与配置配方已写入，实际站点兼容性和所用 Hub 版本仍需恢复测试后验收。
