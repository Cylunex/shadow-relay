package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type ReferenceRecipe struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	ProjectURL string `json:"projectUrl"`
	FeedURL    string `json:"feedUrl,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Kind       string `json:"kind"`
	Coverage   string `json:"coverage"`
	Note       string `json:"note"`
}

// This is an opt-in onboarding catalog, never an automatic scraper or allowlist.
// Project URLs are public upstream references, not operator deployment addresses.
// Curated seed URL catalogs live under local seeds/ (gitignored); they are loaded
// at runtime when present and omitted when missing so serve still works.
func ReferenceRecipes() []ReferenceRecipe {
	// Local seed catalogs first (when present), then built-in project references.
	out := append([]ReferenceRecipe{}, loadLocalSeedRecipes()...)
	out = append(out, builtInReferenceRecipes()...)
	return out
}

func builtInReferenceRecipes() []ReferenceRecipe {
	return []ReferenceRecipe{
		{"yuanc-books", "yuanc · 书源目录", "读", "https://github.com/52liulian/yuanc", "https://raw.githubusercontent.com/52liulian/yuanc/main/data/legado/books.json", "catalog", "catalog", "已接入", "原生 link 字段、路径分类与相对链接；先进入候选箱"},
		{"yuanc-video", "yuanc · 影视目录", "看", "https://github.com/52liulian/yuanc", "https://raw.githubusercontent.com/52liulian/yuanc/main/data/ysc/videos.json", "catalog", "catalog", "已接入", "单仓、多仓候选分别审核"},
		{"yuanc-iptv", "yuanc · 直播目录", "看", "https://github.com/52liulian/yuanc", "https://raw.githubusercontent.com/52liulian/yuanc/main/data/iptv/iptv.json", "catalog", "catalog", "已接入", "目录同步后接纳 M3U/TXT 订阅"},
		{"yuanc-tts", "yuanc · 朗读目录", "听", "https://github.com/52liulian/yuanc", "https://raw.githubusercontent.com/52liulian/yuanc/main/data/legado/tts.json", "catalog", "catalog", "已接入", "TTS 规则在客户端或专门运行时执行"},
		{"yuanc-cleanup", "yuanc · 净化目录", "读", "https://github.com/52liulian/yuanc", "https://raw.githubusercontent.com/52liulian/yuanc/main/data/legado/purify.json", "catalog", "catalog", "已接入", "净化规则支持聚合导出和阅读一键导入"},
		{"yuanc-rss", "yuanc · RSS 目录", "读", "https://github.com/52liulian/yuanc", "https://raw.githubusercontent.com/52liulian/yuanc/main/data/legado/rss.json", "catalog", "catalog", "已接入", "阅读 RSS 规则与普通 RSS Feed 分开处理"},
		{"hub", "LegadoHub", "读", "https://github.com/XziXmn/legado-hub", "", "legado-hub", "runtime", "原生桥接", "不 Fork；生成标准插件、挂载同步、热加载、四阶段 live smoke；Hub 保留聚合搜索、缓存、换源职责"},
		{"sonovel", "so-novel", "读", "https://github.com/freeok/so-novel", "https://raw.githubusercontent.com/freeok/so-novel/main/bundle/rules/main.json", "so-novel", "source", "已接入", "真实规则路径为 bundle/rules；静态 CSS、表单搜索转换，JS/认证逐条报告"},
		{"xiu2", "XIU2 / Yuedu", "读", "https://github.com/XIU2/Yuedu", "", "legado-book", "source", "协议接入", "精选阅读 JSON 候选；静态子集可转 Hub，需自选实际文件"},
		{"parser", "LegadoParser", "读", "https://github.com/821938089/LegadoParser", "", "", "runtime", "可选外置", "其 CSS/JSONPath/XPath/部分 JS API 已核对；没有统一 HTTP API，不能伪装为即插即用服务"},
		{"funread", "funread", "读", "https://github.com/farfarfun/funread", "", "", "tool", "已借鉴", "解析、预览、批量去重思路由 Relay 原生实现"},
		{"reader-dev", "reader-dev", "读", "https://github.com/warpdotsys/reader-dev", "", "", "runtime", "可选外置", "复杂脚本与浏览器运行时；不自动执行源包中的 JS"},
		{"pixiv", "PixivSource", "读", "https://github.com/windyhusky/PixivSource", "https://raw.githubusercontent.com/windyhusky/PixivSource/main/pixiv.json", "legado-book", "source", "已接入", "见 seed-pixivsource-*；登录/复杂 JS 仍需专门运行时"},
		{"aoaostar", "aoaostar / legado", "读", "https://github.com/aoaostar/legado", "", "legado-book", "source", "候选", "大合集只作为候选池，不自动全量激活"},
		{"tickmao", "tickmao / Novel", "读", "https://github.com/tickmao/Novel", "", "legado-book", "source", "候选", "优先选择维护者提供的精选文件；源包去重与兼容性报告"},
		{"shidahuilang", "shidahuilang / shuyuan", "读", "https://github.com/shidahuilang/shuyuan", "", "legado-book", "source", "候选", "上游检查结果不替代本机 live smoke"},
		{"zgq", "ZGQ / source", "全部", "https://github.com/ZGQ-inc/source", "", "", "catalog", "已借鉴", "补齐 RSS、TTS、净化、TVBox、IPTV、漫画仓分类与一键导入；Markdown 页面不当 JSON 订阅"},
		{"tvbox-php", "wzh15802 / tvbox", "看", "https://github.com/wzh15802/tvbox", "", "tvbox", "tool", "已借鉴", "多仓 urls 输出、排序与统一订阅"},
		{"tvbox-ui", "tvbox-ui", "看", "https://github.com/sese972010/tvbox-ui", "", "tvbox", "tool", "已借鉴", "独立订阅后台；Relay 已有持久化管理与绑定"},
		{"tvbox-suite", "TVBox-Suite", "看", "https://github.com/zhiyuan411/TVBox-Suite", "", "tvbox", "tool", "已借鉴", "批量合并、去重、生成仓文件"},
		{"tvbox-config", "tvbox_config", "看", "https://github.com/FanchangWang/tvbox_config", "", "tvbox", "tool", "已借鉴", "定时探活与多线路生成；Relay 使用数据库编排而非另引 YAML 配置系统"},
		{"ecohub", "EcoHub", "看", "https://github.com/fe-spark/EcoHub", "", "tvbox", "runtime", "协议接入", "消费其 TVBox 输出；CMS、片库和爬虫保留在上游"},
		{"akiralereal", "akiralereal / iptv", "看", "https://github.com/akiralereal/iptv", "", "m3u", "runtime", "协议接入", "订阅其 M3U/XMLTV 输出，频道覆盖与令牌由 Relay 提供"},
		{"iptv-tool", "taksssss / iptv-tool", "看", "https://github.com/taksssss/iptv-tool", "", "xmltv", "runtime", "协议接入", "EPG、台标与代理留在领域服务，Relay 聚合其输出"},
		{"iptv-api", "Guovin / iptv-api", "看", "https://github.com/Guovin/iptv-api", "", "m3u", "runtime", "协议接入", "消费清洗测速后的 M3U/TXT，不在 Relay 重建扫描代理"},
		{"super321", "super321 / iptv-tool", "看", "https://github.com/super321/iptv-tool", "", "m3u", "runtime", "协议接入", "可接测速过滤后的直播清单"},
		{"big-mouth", "big-mouth-cn / tv", "看", "https://github.com/big-mouth-cn/tv", "", "m3u", "tool", "协议接入", "直播采集输出作为待审核来源"},
		{"fanmingming", "fanmingming / live", "看", "https://github.com/fanmingming/live", "", "m3u", "source", "候选", "列表向上游，自选可用且授权的订阅地址"},
		{"iptv-org", "iptv-org / iptv", "看", "https://github.com/iptv-org/iptv", "", "m3u", "source", "候选", "建议按语言/地区选小列表，再配置频道覆盖"},
		{"yuanzl77", "yuanzl77 / IPTV", "看", "https://github.com/yuanzl77/IPTV", "", "m3u", "source", "候选", "不默认导入或主动扫描全网频道"},
		{"folder2podcast", "folder2podcast", "听", "https://github.com/yaotutu/folder2podcast", "", "rss", "runtime", "协议接入", "本地文件索引留在服务端；接收其 RSS，也可用 Relay 播客清单生成 RSS"},
		{"miniflux", "Miniflux", "读", "https://github.com/miniflux/v2", "", "miniflux", "runtime", "已接入", "服务鉴权检测、Feed 状态、OPML 导出"},
		{"freshrss", "FreshRSS", "读", "https://github.com/FreshRSS/FreshRSS", "", "opml", "runtime", "协议接入", "通用 RSS/Atom/OPML 互通；不假定 Miniflux 登录 API 兼容"},
		{"keiyoushi", "Keiyoushi Extensions", "漫", "https://github.com/keiyoushi/extensions", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/repo.json", "mihon-repo", "source", "已接入", "识别 repo.json/index_v2 与旧 JSON 索引；保留原仓签名，不下载 APK"},
		{"uchiyomi", "Uchiyomi", "漫", "https://uchiyomi.com/", "", "mihon-repo", "runtime", "协议接入", "漫画扩展运行时外置，不放入 Hub 插件目录"},
		{"suwayomi", "Suwayomi", "漫", "https://github.com/Suwayomi/Suwayomi-Server", "", "suwayomi", "runtime", "已接入", "状态和图源列表检测；扩展安装由原生后台执行"},
		{"komga", "Komga", "漫", "https://github.com/gotson/komga", "", "opds1", "runtime", "协议接入", "通过 OPDS 导航连接本地漫画库，不复制文件入库逻辑"},
		{"kavita", "Kavita", "漫", "https://github.com/Kareadita/Kavita", "", "opds1", "runtime", "协议接入", "通过 OPDS 连接；私有认证保留在客户端"},
		{"legado-skill", "legadoSkill", "工具", "https://github.com/DandanLLab/legadoSkill", "", "legado-book", "tool", "草稿入口", "生成的阅读 JSON 可放入书源工坊检查；不自动调用外部 AI"},
		{"source-skill", "legado-book-source-skill", "工具", "https://github.com/z1131392774/legado-book-source-skill", "", "legado-book", "tool", "草稿入口", "草稿经兼容报告和 Hub live smoke 再维护"},
		{"creator-skill", "book-source-creator-skill", "工具", "https://github.com/Narylr350/book-source-creator-skill", "", "legado-book", "tool", "草稿入口", "社区 JS 不会直接转为可执行插件"},
		{"source-generator", "legado-source-generator", "工具", "https://github.com/z1131392774/legado-source-generator", "", "legado-book", "tool", "草稿入口", "浏览器选取的选择器可填入 Relay 脚手架"},
		{"precheck", "xin-verify-book-source", "工具", "https://github.com/CalmXin/xin-verify-book-source", "", "", "tool", "已改进", "主页可达仅作预筛；Relay 使用 Hub 搜索、详情、目录、正文四阶段抽检"},
		{"tthfyth", "Tthfyth / source", "工具", "https://github.com/Tthfyth/source", "", "", "tool", "不直接兼容", "阅读与异次元转换不等于 Hub 插件转换"},
		{"reader-rust", "reader-rust", "读", "https://github.com/givenge/reader-rust", "", "", "runtime", "可选外置", "独立阅读服务，不合并第二套书架和缓存模型"},
		{"reader", "hectorqin / reader", "读", "https://github.com/hectorqin/reader", "", "", "runtime", "可选外置", "与 Hub 同类的替代运行时，不自动接管账号和阅读进度"},
		{"talebook", "Talebook", "读", "https://github.com/talebook/talebook", "", "opds1", "runtime", "协议接入", "本地 EPUB/PDF 书库通过 OPDS 接入"},
		{"koreader", "legado.koplugin", "读", "https://github.com/pengcw/legado.koplugin", "", "", "client", "客户端参考", "其阅读 Web API 与 Hub 不是同一接口，不宣称直接互通"},
		{"xbs", "xbsrebuild", "读", "https://github.com/ne1llee/xbsrebuild", "", "", "tool", "不直接兼容", "香色闺阁是另一套格式，避免错误转换"},
		{"browserless", "Browserless / Playwright", "工具", "https://github.com/XziXmn/legado-hub/blob/main/docker-compose.browserless.yml", "", "", "runtime", "可选外置", "复杂站点交给 Hub 的受控浏览器；不自动部署"},
		{"cf-browser", "CloudflareBypassForScraping", "工具", "https://github.com/sarperavci/CloudflareBypassForScraping", "", "", "runtime", "外部参考", "浏览器类工具可由操作者选择；Relay 不实现绕过登录或付费限制"},
	}
}

var (
	seedRecipesOnce sync.Once
	seedRecipes     []ReferenceRecipe
)

func loadLocalSeedRecipes() []ReferenceRecipe {
	seedRecipesOnce.Do(func() {
		dir := os.Getenv("RELAY_SEEDS_DIR")
		if dir == "" {
			dir = "seeds"
		}
		path := filepath.Join(dir, "recipes.json")
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var list []ReferenceRecipe
		if err := json.Unmarshal(b, &list); err != nil {
			return
		}
		seedRecipes = list
	})
	return seedRecipes
}
