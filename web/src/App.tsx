import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  Activity,
  ArrowDownToLine,
  ArrowRight,
  ArrowUpRight,
  BookOpen,
  Check,
  ChevronDown,
  ChevronRight,
  Compass,
  Copy,
  FileJson,
  Globe,
  Headphones,
  Layers3,
  LayoutDashboard,
  Link2,
  LoaderCircle,
  LogOut,
  MoreHorizontal,
  Plus,
  Radio,
  RefreshCw,
  Search,
  Send,
  Server,
  Settings2,
  ShieldCheck,
  Trash2,
  Video,
  Volume2,
} from "lucide-react";
import QRCode from "qrcode";
import { api, defaultMember, label, setCredential, short, time } from "./api";
import type {
  Catalog,
  Data,
  Meta,
  Normalized,
  Probe,
  Publication,
  Revision,
  Runtime,
  Source,
  SourceSet,
} from "./types";
import { Badge, Empty, ErrorBox, External, Field, Modal } from "./ui";

type Page =
  | "overview"
  | "sources"
  | "discover"
  | "sets"
  | "publish"
  | "runtimes"
  | "jobs";
const navigation = [
  { id: "overview" as Page, title: "总览", icon: LayoutDashboard },
  { id: "sources" as Page, title: "源库", icon: Layers3 },
  { id: "discover" as Page, title: "发现与候选箱", icon: Compass },
  { id: "sets" as Page, title: "源编排组", icon: Settings2 },
  { id: "publish" as Page, title: "发布与客户端", icon: Send },
  { id: "runtimes" as Page, title: "运行时", icon: Server },
  { id: "jobs" as Page, title: "任务与审计", icon: Activity },
];
const emptyData: Data = {
  sources: [],
  catalogs: [],
  candidates: [],
  sets: [],
  publications: [],
  bindings: [],
  runtimes: [],
  jobs: [],
  audits: [],
  meta: { adapters: [], connectors: {}, formats: [] },
};
type Dialog =
  | { type: "import" }
  | { type: "source"; source: Source }
  | { type: "set"; set?: SourceSet }
  | { type: "runtime"; runtime?: Runtime }
  | { type: "catalog"; catalog?: Catalog }
  | { type: "binding" }
  | { type: "token"; baseUrl: string; formats: string[] }
  | { type: "publication"; publication: Publication }
  | null;
const descriptions: Record<Page, string> = {
  overview: "让每一条源，稳稳抵达。",
  sources: "统一管理看、读、听、说的每一个入口。",
  discover: "先发现，再审核。让可信的源进入你的媒体世界。",
  sets: "把独立的源，编排成适合自己的媒体集合。",
  publish: "一次编排，发布到每一台设备。",
  runtimes: "连接领域服务，让专业的执行器各司其职。",
  jobs: "每一次更新与变更，都有迹可循。",
};
const domains = [
  { name: "看", sub: "视频与直播", prefix: "video.", icon: Video },
  { name: "读", sub: "小说、漫画与文章", prefix: "text.", icon: BookOpen },
  { name: "听", sub: "有声书与播客", prefix: "audio.", icon: Headphones },
  { name: "说", sub: "语音与朗读", prefix: "speech.", icon: Volume2 },
];
export default function App() {
  const [authed, setAuthed] = useState(false);
  const [data, setData] = useState<Data>(emptyData);
  const [page, setPage] = useState<Page>("overview");
  const [dialog, setDialog] = useState<Dialog>(null);
  const [error, setError] = useState("");
  const [toast, setToast] = useState("");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const refresh = useCallback(async () => {
    const [
      sources,
      catalogs,
      candidates,
      sets,
      publications,
      bindings,
      runtimes,
      jobs,
      audits,
      meta,
    ] = await Promise.all([
      api<Data["sources"]>("sources"),
      api<Data["catalogs"]>("catalogs"),
      api<Data["candidates"]>("candidates"),
      api<Data["sets"]>("source-sets"),
      api<Data["publications"]>("publications"),
      api<Data["bindings"]>("bindings"),
      api<Data["runtimes"]>("runtimes"),
      api<Data["jobs"]>("jobs"),
      api<Data["audits"]>("audits"),
      api<Meta>("adapters"),
    ]);
    setData({
      sources,
      catalogs,
      candidates,
      sets,
      publications,
      bindings,
      runtimes,
      jobs,
      audits,
      meta,
    });
    setError("");
  }, []);
  useEffect(() => {
    if (!authed) return;
    const id = setInterval(() => {
      refresh().catch((e) => setError(e.message));
    }, 10000);
    return () => clearInterval(id);
  }, [authed, refresh]);
  useEffect(() => {
    if (!toast) return;
    const id = setTimeout(() => setToast(""), 4500);
    return () => clearTimeout(id);
  }, [toast]);
  const run = async (fn: () => Promise<unknown>, message = "已保存") => {
    setBusy(true);
    setError("");
    try {
      await fn();
      await refresh();
      setToast(message);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  async function login(token: string) {
    setCredential(token);
    await refresh();
    setAuthed(true);
  }
  if (!authed) return <Login login={login} />;
  const pending =
    data.sources.filter((s) => s.stagedRevision).length +
    data.candidates.filter((c) => c.status === "pending").length;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a
          className="brand"
          href="#"
          onClick={(e) => {
            e.preventDefault();
            setPage("overview");
          }}
        >
          <img src="/relay.svg" alt="" />
          <div>
            Shadow Relay<span>全媒体源编排中心</span>
          </div>
        </a>
        <div className="workspace">
          <span className="workspace-icon">S</span>
          <div>
            个人媒体空间<small>PRIVATE WORKSPACE</small>
          </div>
          <ShieldCheck size={16} />
        </div>
        <div className="nav-label">工作台</div>
        <nav>
          {navigation.map((n) => (
            <button
              key={n.id}
              aria-label={n.title}
              className={page === n.id ? "active" : ""}
              onClick={() => setPage(n.id)}
            >
              <n.icon size={18} />
              <span>{n.title}</span>
              {n.id === "discover" && pending > 0 && <b>{pending}</b>}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="private-note">
            <ShieldCheck size={19} />
            <div>
              你的源，私有可控<span>凭据加密 · 发布可追溯</span>
            </div>
          </div>
          <button
            onClick={() => {
              setCredential("");
              setAuthed(false);
              setData(emptyData);
              setDialog(null);
            }}
          >
            <LogOut size={16} />
            退出控制台
          </button>
          <small>
            SHADOW RELAY <span>v0.1.0</span>
          </small>
        </div>
      </aside>
      <main>
        <header className="topbar">
          <div>
            <span>工作台</span>
            <ChevronRight size={13} />
            {navigation.find((n) => n.id === page)?.title}
          </div>
          <div className="topbar-right">
            <span className="online">
              <i />
              私有控制台
            </span>
            <button
              className="icon-button"
              aria-label="刷新数据"
              onClick={() => {
                setLoading(true);
                refresh()
                  .catch((e) => setError(e.message))
                  .finally(() => setLoading(false));
              }}
            >
              <RefreshCw size={16} className={loading ? "spin" : ""} />
            </button>
            <div className="avatar">S</div>
          </div>
        </header>
        <div className="page">
          <div className="page-heading">
            <div>
              <div className="eyebrow">
                {page === "overview"
                  ? "YOUR MEDIA, CONNECTED."
                  : "SHADOW RELAY / " + page.toUpperCase()}
              </div>
              <h1>{navigation.find((n) => n.id === page)?.title}</h1>
              <p>{descriptions[page]}</p>
            </div>
            <button
              className="primary"
              onClick={() =>
                setDialog({
                  type:
                    page === "sets"
                      ? "set"
                      : page === "runtimes"
                        ? "runtime"
                        : page === "discover"
                          ? "catalog"
                          : page === "publish"
                            ? "binding"
                            : "import",
                })
              }
            >
              <Plus size={16} />
              {page === "sets"
                ? "新建编排组"
                : page === "runtimes"
                  ? "连接运行时"
                  : page === "discover"
                    ? "订阅上游目录"
                    : page === "publish"
                      ? "绑定客户端"
                      : "添加源"}
            </button>
          </div>
          <ErrorBox error={error} />
          {page === "overview" && (
            <Overview data={data} navigate={setPage} open={setDialog} />
          )}
          {page === "sources" && (
            <Sources data={data} open={setDialog} busy={busy} run={run} />
          )}
          {page === "discover" && (
            <Discover data={data} open={setDialog} busy={busy} run={run} />
          )}
          {page === "sets" && (
            <Sets data={data} open={setDialog} busy={busy} run={run} />
          )}
          {page === "publish" && (
            <Publications data={data} open={setDialog} busy={busy} run={run} />
          )}
          {page === "runtimes" && (
            <Runtimes data={data} open={setDialog} busy={busy} run={run} />
          )}
          {page === "jobs" && <Jobs data={data} busy={busy} run={run} />}
          <footer>
            连接不同的世界，保持同一份秩序。
            <span>Shadow Relay · 源与能力的控制面</span>
          </footer>
        </div>
      </main>
      {toast && (
        <div className="toast" role="status">
          <Check size={17} />
          {toast}
        </div>
      )}
      {dialog?.type === "import" && (
        <ImportDialog
          meta={data.meta}
          runtimes={data.runtimes}
          close={() => setDialog(null)}
          saved={async () => {
            await refresh();
            setDialog(null);
            setPage("sources");
            setToast("源已导入，请在详情中审核版本并启用");
          }}
        />
      )}
      {dialog?.type === "source" && (
        <SourceDialog
          source={
            data.sources.find((s) => s.id === dialog.source.id) ?? dialog.source
          }
          data={data}
          close={() => setDialog(null)}
          refresh={refresh}
        />
      )}
      {dialog?.type === "set" && (
        <SetDialog
          set={dialog.set}
          sources={data.sources}
          close={() => setDialog(null)}
          saved={async () => {
            await refresh();
            setDialog(null);
          }}
        />
      )}
      {dialog?.type === "runtime" && (
        <RuntimeDialog
          runtime={dialog.runtime}
          meta={data.meta}
          close={() => setDialog(null)}
          saved={async () => {
            await refresh();
            setDialog(null);
          }}
        />
      )}
      {dialog?.type === "catalog" && (
        <CatalogDialog
          catalog={dialog.catalog}
          close={() => setDialog(null)}
          saved={async () => {
            await refresh();
            setDialog(null);
          }}
        />
      )}
      {dialog?.type === "binding" && (
        <BindingDialog
          data={data}
          close={() => setDialog(null)}
          saved={async (baseUrl, formats) => {
            await refresh();
            setDialog({ type: "token", baseUrl, formats });
          }}
        />
      )}
      {dialog?.type === "token" && (
        <TokenDialog
          baseUrl={dialog.baseUrl}
          formats={dialog.formats}
          close={() => setDialog(null)}
        />
      )}
      {dialog?.type === "publication" && (
        <PublicationDialog
          publication={dialog.publication}
          close={() => setDialog(null)}
        />
      )}
    </div>
  );
}
function Login({ login }: { login: (token: string) => Promise<void> }) {
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await login(token);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-page">
      <div className="login-brand">
        <img src="/relay.svg" alt="" />
        Shadow Relay
      </div>
      <div className="login-art">
        <div className="orbit orbit-one" />
        <div className="orbit orbit-two" />
        <div className="orbit orbit-three" />
        <div className="orbit-core">
          <img src="/relay.svg" alt="" />
        </div>
        <span className="orbit-node node-a">
          <Video />看
        </span>
        <span className="orbit-node node-b">
          <BookOpen />读
        </span>
        <span className="orbit-node node-c">
          <Headphones />听
        </span>
        <span className="orbit-node node-d">
          <Volume2 />说
        </span>
      </div>
      <form className="login-card" onSubmit={submit}>
        <div className="eyebrow">ONE SPACE. EVERY SOURCE.</div>
        <h1>
          让每一条源，
          <br />
          稳稳抵达。
        </h1>
        <p>
          从发现到发布，在一个私有空间里
          <br />
          编排你的整个媒体世界。
        </p>
        <Field
          label="管理员令牌"
          hint="使用服务启动时配置的令牌，仅保存在当前页面内存中。"
        >
          <input
            type="password"
            autoComplete="current-password"
            required
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="输入管理员访问令牌"
          />
        </Field>
        <ErrorBox error={error} />
        <button className="primary" disabled={busy}>
          {busy ? (
            <LoaderCircle className="spin" size={16} />
          ) : (
            <ArrowRight size={16} />
          )}
          进入控制台
        </button>
        <div className="login-security">
          <ShieldCheck size={14} />
          私有访问 · 凭据加密 · 可控发布
        </div>
      </form>
      <div className="login-foot">SHADOW RELAY / 全媒体源编排与发布中心</div>
    </div>
  );
}
function Overview({
  data,
  navigate,
  open,
}: {
  data: Data;
  navigate: (p: Page) => void;
  open: (d: Dialog) => void;
}) {
  const healthy = data.sources.filter(
    (s) => s.enabled && s.health === "healthy",
  ).length;
  const degraded = data.sources.filter(
    (s) => s.enabled && ["degraded", "failing", "unknown"].includes(s.health),
  ).length;
  const quarantined = data.sources.filter(
    (s) => s.health === "quarantined",
  ).length;
  const staged = data.sources.filter((s) => s.stagedRevision).length;
  const recent = [...data.audits]
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    .slice(0, 5);
  return (
    <>
      <div className="metrics">
        {[
          {
            title: "正常运行",
            num: healthy,
            sub: "功能抽样通过",
            color: "green",
          },
          {
            title: "需要关注",
            num: degraded,
            sub: "等待体检或状态降级",
            color: "amber",
          },
          {
            title: "已隔离",
            num: quarantined,
            sub: "不进入新发布",
            color: "red",
          },
          {
            title: "待审核更新",
            num: staged,
            sub: "当前版本继续可用",
            color: "gray",
          },
        ].map((m, i) => (
          <button
            className="metric"
            key={m.title}
            onClick={() => navigate("sources")}
          >
            <div>
              <span>{m.title}</span>
              <span className={"metric-dot " + m.color} />
            </div>
            <strong>{String(m.num).padStart(2, "0")}</strong>
            <small>
              {m.sub}
              <ArrowUpRight size={14} />
            </small>
            <div className={"metric-line line-" + i} />
          </button>
        ))}
      </div>
      <div className="overview-layout">
        <div>
          <section className="panel domain-panel">
            <div className="section-title">
              <div>
                <h2>你的媒体世界</h2>
                <p>不同的内容，共用一套秩序</p>
              </div>
              <button
                className="text-button"
                onClick={() => navigate("sources")}
              >
                查看源库
                <ArrowUpRight size={14} />
              </button>
            </div>
            <div className="domains">
              {domains.map((d) => {
                const count = data.sources.filter((s) =>
                  s.mediaTypes.some(
                    (m) =>
                      m.startsWith(d.prefix) ||
                      (d.name === "读" && m === "image.comic"),
                  ),
                ).length;
                return (
                  <button
                    key={d.name}
                    className="domain"
                    onClick={() => navigate("sources")}
                  >
                    <span className={"domain-icon icon-" + d.name}>
                      <d.icon size={22} />
                    </span>
                    <h3>
                      {d.name}
                      <span>{count} 个源</span>
                    </h3>
                    <p>{d.sub}</p>
                    <div className="domain-rule" />
                    <small>
                      {count ? "已纳入源库" : "等待连接你的第一个源"}
                      <ArrowRight size={13} />
                    </small>
                  </button>
                );
              })}
            </div>
          </section>
          <section className="panel">
            <div className="section-title">
              <div>
                <h2>从源头，到设备</h2>
                <p>每一步都可审核，每个版本都可追溯</p>
              </div>
              <span className="tag">控制面流水线</span>
            </div>
            <div className="pipeline">
              {[
                {
                  title: "发现",
                  icon: Compass,
                  count: data.candidates.filter((c) => c.status === "pending")
                    .length,
                  sub: "候选条目",
                },
                {
                  title: "审核",
                  icon: ShieldCheck,
                  count: staged,
                  sub: "暂存版本",
                },
                {
                  title: "编排",
                  icon: Layers3,
                  count: data.sets.length,
                  sub: "源编排组",
                },
                {
                  title: "发布",
                  icon: Send,
                  count: data.publications.length,
                  sub: "不可变版本",
                },
              ].map((step, i) => (
                <div className="pipeline-step" key={step.title}>
                  <div>
                    <step.icon size={20} />
                    {i < 3 && (
                      <span className="pipeline-arrow">
                        <ArrowRight size={15} />
                      </span>
                    )}
                  </div>
                  <h3>{step.title}</h3>
                  <small>
                    <b>{step.count}</b> {step.sub}
                  </small>
                </div>
              ))}
            </div>
          </section>
          <section className="panel">
            <div className="section-title">
              <h2>最近活动</h2>
              <button className="text-button" onClick={() => navigate("jobs")}>
                全部记录
                <ArrowUpRight size={14} />
              </button>
            </div>
            {recent.length ? (
              recent.map((a) => (
                <div className="activity-row" key={a.id}>
                  <span className="activity-dot" />
                  <div>
                    <b>{actionLabel(a.action)}</b>
                    <small>{short(a.targetId)}</small>
                  </div>
                  <time>{time(a.createdAt)}</time>
                </div>
              ))
            ) : (
              <Empty
                title="新的媒体空间，已就绪"
                description="添加第一个源后，同步、审核和发布记录会出现在这里。"
                action={
                  <button
                    className="secondary"
                    onClick={() => open({ type: "import" })}
                  >
                    <Plus size={15} />
                    添加第一个源
                  </button>
                }
              />
            )}
          </section>
        </div>
        <div>
          <section className="feature-card">
            <span className="tag">SOURCE SET</span>
            <div className="set-illustration">
              <div>
                <Layers3 size={34} />
              </div>
              <span />
              <span />
              <span />
            </div>
            <h2>
              一次编排，
              <br />
              连接每一台设备。
            </h2>
            <p>将主源、备用源和辅助能力组成一套配置，按设备需要发布。</p>
            <button onClick={() => open({ type: "set" })}>
              创建源编排组
              <ArrowUpRight size={16} />
            </button>
          </section>
          <section className="panel runtime-summary">
            <div className="section-title">
              <h2>运行时</h2>
              <span className="tag">{data.runtimes.length} 个连接</span>
            </div>
            {data.runtimes.length ? (
              data.runtimes.slice(0, 4).map((rt) => (
                <div className="runtime-mini" key={rt.id}>
                  <Server size={17} />
                  <div>
                    {rt.name}
                    <small>{rt.driver}</small>
                  </div>
                  <Badge value={rt.health} />
                </div>
              ))
            ) : (
              <p className="muted">
                连接 Emby、Suwayomi、LegadoHub 等领域服务，由它们执行内容能力。
              </p>
            )}
            <button
              className="text-button"
              onClick={() => navigate("runtimes")}
            >
              管理运行时
              <ArrowRight size={14} />
            </button>
          </section>
          <div className="note">
            <ShieldCheck size={19} />
            <p>
              发布保留最后可用版本。
              <br />
              <span>上游故障，不会抹掉已有配置。</span>
            </p>
          </div>
        </div>
      </div>
    </>
  );
}
type PanelProps = {
  data: Data;
  busy: boolean;
  run: (fn: () => Promise<unknown>, message?: string) => Promise<void>;
  open?: (d: Dialog) => void;
};
function Sources({ data, open, busy, run }: PanelProps) {
  const [query, setQuery] = useState("");
  const [domain, setDomain] = useState("全部");
  const [health, setHealth] = useState("");
  const [protocol, setProtocol] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const matches = data.sources.filter(
    (s) =>
      (s.name + " " + s.protocol + " " + s.url)
        .toLowerCase()
        .includes(query.toLowerCase()) &&
      (!health || s.health === health) &&
      (!protocol || s.protocol === protocol) &&
      (domain === "全部" || s.mediaTypes.some((m) => m.startsWith(domain))),
  );
  const batch = (action: string) =>
    run(
      async () => {
        for (const id of selected) await api(`sources/${id}/${action}`, "POST");
        setSelected([]);
      },
      action === "probe" ? "已加入体检队列" : "批量操作已完成",
    );
  return (
    <>
      <div className="tabs">
        {[
          ["全部", "全部"],
          ["视频与直播", "video."],
          ["阅读", "text."],
          ["漫画", "image."],
          ["音频", "audio."],
          ["TTS", "speech."],
          ["辅助", "support."],
        ].map(([name, prefix]) => (
          <button
            key={name}
            className={domain === prefix ? "active" : ""}
            onClick={() => setDomain(prefix)}
          >
            {name}
            {prefix === "全部" && <span>{data.sources.length}</span>}
          </button>
        ))}
      </div>
      <div className="toolbar">
        <label className="search">
          <Search size={16} />
          <input
            aria-label="搜索源"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索名称、协议或地址…"
          />
        </label>
        <select
          aria-label="健康筛选"
          value={health}
          onChange={(e) => setHealth(e.target.value)}
        >
          <option value="">全部状态</option>
          {[
            "healthy",
            "degraded",
            "failing",
            "quarantined",
            "disabled",
            "unknown",
          ].map((h) => (
            <option key={h} value={h}>
              {label(h)}
            </option>
          ))}
        </select>
        <select
          aria-label="协议筛选"
          value={protocol}
          onChange={(e) => setProtocol(e.target.value)}
        >
          <option value="">全部协议</option>
          {[...new Set(data.sources.map((s) => s.protocol))].map((p) => (
            <option key={p}>{p}</option>
          ))}
        </select>
        <span className="result-count">{matches.length} 个源</span>
      </div>
      {selected.length > 0 && (
        <div className="selection-bar">
          <span>已选 {selected.length} 项</span>
          {[
            ["enable", "启用"],
            ["disable", "停用"],
            ["probe", "体检"],
            ["quarantine", "隔离"],
          ].map(([a, n]) => (
            <button disabled={busy} key={a} onClick={() => batch(a)}>
              {n}
            </button>
          ))}
        </div>
      )}
      <div className="panel table-panel">
        {matches.length ? (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>
                    <input
                      type="checkbox"
                      aria-label="选择全部源"
                      checked={
                        matches.length > 0 &&
                        matches.every((s) => selected.includes(s.id))
                      }
                      onChange={(e) =>
                        setSelected(
                          e.target.checked ? matches.map((s) => s.id) : [],
                        )
                      }
                    />
                  </th>
                  <th>源名称</th>
                  <th>协议 / 执行方式</th>
                  <th>健康状态</th>
                  <th>版本</th>
                  <th>最近更新</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {matches.map((src) => (
                  <tr key={src.id}>
                    <td>
                      <input
                        type="checkbox"
                        aria-label={"选择 " + src.name}
                        checked={selected.includes(src.id)}
                        onChange={(e) =>
                          setSelected(
                            e.target.checked
                              ? [...selected, src.id]
                              : selected.filter((id) => id !== src.id),
                          )
                        }
                      />
                    </td>
                    <td>
                      <button
                        className="source-name"
                        onClick={() => open?.({ type: "source", source: src })}
                      >
                        <span className="source-icon">
                          <SourceIcon source={src} />
                        </span>
                        <div>
                          {src.name}
                          <small>{src.mediaTypes.join(" · ")}</small>
                        </div>
                      </button>
                    </td>
                    <td>
                      <strong className="mono">{src.protocol}</strong>
                      <small>{label(src.mode)}</small>
                    </td>
                    <td>
                      <Badge value={src.health} />
                      <small>
                        {src.enabled ? "已启用" : "未启用"} · {src.score} 分
                      </small>
                    </td>
                    <td>
                      <code>{short(src.activeRevision)}</code>
                      {src.stagedRevision && (
                        <span className="pending-label">待审核</span>
                      )}
                    </td>
                    <td className="muted">{time(src.updatedAt)}</td>
                    <td>
                      <button
                        className="icon-button"
                        aria-label={"查看 " + src.name}
                        onClick={() => open?.({ type: "source", source: src })}
                      >
                        <MoreHorizontal size={18} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty
            title={
              data.sources.length ? "没有匹配的源" : "源库等待你的第一个连接"
            }
            description="粘贴 URL、上传文件、导入配置正文，或连接已有媒体服务。"
            action={
              <button
                className="secondary"
                onClick={() => open?.({ type: "import" })}
              >
                <Plus size={15} />
                添加源
              </button>
            }
          />
        )}
      </div>
      <div className="hint-line">
        <ShieldCheck size={14} />
        未知 JAR、JavaScript 和 Python 规则不会在 Relay 核心进程执行。
      </div>
    </>
  );
}
function SourceIcon({ source }: { source: Source }) {
  const m = source.mediaTypes[0] ?? "";
  const Icon = m.startsWith("video.live")
    ? Radio
    : m.startsWith("video")
      ? Video
      : m.startsWith("audio")
        ? Headphones
        : m.startsWith("speech")
          ? Volume2
          : BookOpen;
  return <Icon size={19} />;
}
function Discover({ data, open, busy, run }: PanelProps) {
  const [status, setStatus] = useState("pending");
  return (
    <>
      <div className="section-title">
        <h2>上游目录</h2>
        <span className="muted">更新只进入候选箱</span>
      </div>
      <div className="catalog-grid">
        {data.catalogs.map((c) => (
          <div className="panel compact-card" key={c.id}>
            <div className="card-top">
              <Globe size={20} />
              <Badge value={c.enabled ? "healthy" : "disabled"} />
            </div>
            <h3>{c.name}</h3>
            <p className="truncate">{c.url}</p>
            <small>最近同步 {time(c.lastSync)}</small>
            <div className="card-actions">
              <button
                className="secondary"
                disabled={busy}
                onClick={() =>
                  run(
                    () => api(`catalogs/${c.id}/sync`, "POST"),
                    "已加入同步队列",
                  )
                }
              >
                <RefreshCw size={14} />
                同步
              </button>
              <button
                className="text-button"
                onClick={() => open?.({ type: "catalog", catalog: c })}
              >
                设置
              </button>
            </div>
          </div>
        ))}
      </div>
      {!data.catalogs.length && (
        <div className="panel">
          <Empty
            title="订阅一个上游目录"
            description="支持名称/URL JSON 目录、OPML 和 TVBox 多仓。候选条目需要审核后才能启用。"
            action={
              <button
                className="secondary"
                onClick={() => open?.({ type: "catalog" })}
              >
                添加上游目录
              </button>
            }
          />
        </div>
      )}
      <div className="section-title spacer">
        <h2>候选箱</h2>
        <select
          aria-label="候选状态"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
        >
          {["pending", "accepted", "ignored", "blocked"].map((s) => (
            <option key={s} value={s}>
              {label(s)}
            </option>
          ))}
        </select>
      </div>
      <div className="panel">
        {data.candidates
          .filter((c) => c.status === status)
          .map((c) => (
            <div key={c.id} className="candidate-row">
              <div className="source-icon">
                <Compass size={18} />
              </div>
              <div className="grow">
                <h3>{c.name}</h3>
                <p>{c.url}</p>
                <small>
                  {data.catalogs.find((x) => x.id === c.catalogId)?.name} ·{" "}
                  {time(c.discoveredAt)}
                </small>
              </div>
              <Badge value={c.status} />
              <div className="button-row">
                {c.status === "pending" ? (
                  <>
                    <button
                      className="secondary"
                      disabled={busy}
                      onClick={() =>
                        run(
                          () => api(`candidates/${c.id}/accept`, "POST"),
                          "已接纳，前往源库审核版本",
                        )
                      }
                    >
                      检测并接纳
                    </button>
                    <button
                      className="text-button"
                      disabled={busy}
                      onClick={() =>
                        run(() => api(`candidates/${c.id}/ignore`, "POST"))
                      }
                    >
                      忽略
                    </button>
                    <button
                      className="text-button danger"
                      disabled={busy}
                      onClick={() =>
                        run(() => api(`candidates/${c.id}/block`, "POST"))
                      }
                    >
                      屏蔽
                    </button>
                  </>
                ) : c.status !== "accepted" ? (
                  <button
                    className="text-button"
                    disabled={busy}
                    onClick={() =>
                      run(() => api(`candidates/${c.id}/reset`, "POST"))
                    }
                  >
                    恢复审核
                  </button>
                ) : null}
              </div>
            </div>
          ))}
        {!data.candidates.some((c) => c.status === status) && (
          <Empty
            title="这里暂时没有条目"
            description="同步上游后，新增条目将在这里等待你审核。"
          />
        )}
      </div>
    </>
  );
}
function Sets({ data, open, busy, run }: PanelProps) {
  return data.sets.length ? (
    <div className="set-grid">
      {data.sets.map((set) => (
        <section className="panel set-card" key={set.id}>
          <div className="card-top">
            <span className="source-icon">
              <Layers3 size={22} />
            </span>
            <span className="tag">{set.members.length} 个源</span>
          </div>
          <h2>{set.name}</h2>
          <p>{set.description || "为你的设备编排媒体入口"}</p>
          <div className="set-members">
            {[...set.members]
              .sort((a, b) => b.priority - a.priority)
              .slice(0, 6)
              .map((m) => (
                <div key={m.sourceId}>
                  <span className="tree-line" />
                  <b>
                    {data.sources.find((s) => s.id === m.sourceId)?.name ??
                      short(m.sourceId)}
                  </b>
                  <span>{label(m.role)}</span>
                  <code>{m.priority}</code>
                </div>
              ))}
          </div>
          <small>
            当前发布 <code>{short(set.currentPublication)}</code>
          </small>
          <div className="card-actions">
            <button
              className="primary"
              disabled={busy}
              onClick={() =>
                run(
                  () => api(`source-sets/${set.id}/publish`, "POST"),
                  "编译完成，已原子发布",
                )
              }
            >
              <Send size={14} />
              编译并发布
            </button>
            <button
              className="secondary"
              onClick={() => open?.({ type: "set", set })}
            >
              编辑编排
            </button>
          </div>
        </section>
      ))}
    </div>
  ) : (
    <div className="panel">
      <Empty
        title="把源连接成一套配置"
        description="选择成员，设置顺序、主备关系和健康阈值，再编译成设备所需的订阅。"
        action={
          <button className="primary" onClick={() => open?.({ type: "set" })}>
            <Plus size={15} />
            新建编排组
          </button>
        }
      />
    </div>
  );
}
function Publications({ data, open, busy, run }: PanelProps) {
  return (
    <>
      <div className="section-title">
        <h2>发布历史</h2>
        <span className="tag">不可变快照</span>
      </div>
      <div className="panel table-panel">
        {data.publications.length ? (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>发布版本</th>
                  <th>编排组</th>
                  <th>格式 / 源</th>
                  <th>发布时间</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {[...data.publications]
                  .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
                  .map((p) => (
                    <tr key={p.id}>
                      <td>
                        <button
                          className="text-button mono"
                          onClick={() =>
                            open?.({ type: "publication", publication: p })
                          }
                        >
                          {short(p.id)}
                          <FileJson size={14} />
                        </button>
                        {data.sets.some(
                          (s) => s.currentPublication === p.id,
                        ) && <span className="tag current">当前</span>}
                      </td>
                      <td>{data.sets.find((s) => s.id === p.setId)?.name}</td>
                      <td>
                        {Object.keys(p.artifacts).length} 个文件 ·{" "}
                        {Object.keys(p.sourceRevisions).length} 个源
                      </td>
                      <td>{time(p.createdAt)}</td>
                      <td>
                        <button
                          disabled={
                            busy ||
                            data.sets.some((s) => s.currentPublication === p.id)
                          }
                          className="secondary"
                          onClick={() =>
                            run(
                              () =>
                                api(`publications/${p.id}/rollback`, "POST"),
                              "稳定订阅已切换到此版本",
                            )
                          }
                        >
                          回滚至此
                        </button>
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty
            title="还没有发布版本"
            description="在源编排组中点击「编译并发布」，创建第一份不可变配置。"
          />
        )}
      </div>
      <div className="section-title spacer">
        <h2>客户端绑定</h2>
        <span className="muted">令牌只在创建或轮换时显示</span>
      </div>
      <div className="binding-grid">
        {data.bindings.map((b) => (
          <div className="panel compact-card" key={b.id}>
            <div className="card-top">
              <Link2 size={21} />
              <Badge
                value={
                  b.revoked
                    ? "disabled"
                    : new Date(b.expiresAt) < new Date()
                      ? "failing"
                      : "healthy"
                }
              />
            </div>
            <h3>{b.name}</h3>
            <p>{data.sets.find((s) => s.id === b.setId)?.name}</p>
            <div className="chip-list">
              {b.formats.map((f) => (
                <code key={f}>{f}</code>
              ))}
            </div>
            <small>
              过期 {time(b.expiresAt)} · 第 {b.generation} 代令牌
            </small>
            <div className="card-actions">
              <button
                className="secondary"
                disabled={busy || new Date(b.expiresAt) < new Date()}
                onClick={() =>
                  run(async () => {
                    const res = await api<{ baseUrl: string }>(
                      `bindings/${b.id}/rotate`,
                      "POST",
                    );
                    open?.({
                      type: "token",
                      baseUrl: res.baseUrl,
                      formats: b.formats,
                    });
                  }, "旧令牌已失效")
                }
              >
                轮换令牌
              </button>
              <button
                disabled={busy || b.revoked}
                className="text-button danger"
                onClick={() =>
                  run(
                    () => api(`bindings/${b.id}/revoke`, "POST"),
                    "客户端访问已吊销",
                  )
                }
              >
                吊销
              </button>
            </div>
          </div>
        ))}
      </div>
      {!data.bindings.length && (
        <div className="panel">
          <Empty
            title="让设备订阅你的编排"
            description="为每个客户端创建独立令牌，按格式授权，随时吊销。"
            action={
              <button
                className="secondary"
                onClick={() => open?.({ type: "binding" })}
              >
                绑定客户端
              </button>
            }
          />
        </div>
      )}
    </>
  );
}
function Runtimes({ data, open, busy, run }: PanelProps) {
  return (
    <>
      <div className="note wide-note">
        <Server size={20} />
        <p>
          Relay 管理源与能力，领域运行时负责内容执行。
          <span>
            状态同步读取服务信息；安装扩展和修改运行时配置请进入原生后台。
          </span>
        </p>
      </div>
      {data.runtimes.length ? (
        <div className="runtime-grid">
          {data.runtimes.map((rt) => (
            <div className="panel compact-card" key={rt.id}>
              <div className="card-top">
                <span className="runtime-logo">
                  {rt.name.slice(0, 1).toUpperCase()}
                </span>
                <Badge value={rt.health} />
              </div>
              <h2>{rt.name}</h2>
              <p>
                {data.meta.connectors[rt.driver]?.name}{" "}
                {rt.version && "· " + rt.version}
              </p>
              <External url={rt.url}>打开原生后台</External>
              <div className="chip-list">
                {rt.capabilities.map((c) => (
                  <span className="tag" key={c}>
                    {c}
                  </span>
                ))}
              </div>
              <small>
                最近检测 {time(rt.lastChecked)}
                <br />
                同步状态 {time(rt.lastSync)}
                {rt.state?.itemCount !== undefined &&
                  ` · ${rt.state.itemCount} 个条目`}
              </small>
              <div className="card-actions">
                <button
                  className="secondary"
                  disabled={busy}
                  onClick={() =>
                    run(
                      () => api(`runtimes/${rt.id}/test`, "POST"),
                      "已加入连接检测队列",
                    )
                  }
                >
                  检测连接
                </button>
                <button
                  className="secondary"
                  disabled={busy}
                  onClick={() =>
                    run(
                      () => api(`runtimes/${rt.id}/sync`, "POST"),
                      "已加入状态同步队列",
                    )
                  }
                >
                  同步状态
                </button>
                <button
                  className="text-button"
                  onClick={() => open?.({ type: "runtime", runtime: rt })}
                >
                  设置
                </button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="panel">
          <Empty
            title="连接你已有的媒体服务"
            description="支持 Emby、Jellyfin、Dispatcharr、LegadoHub、Suwayomi、Audiobookshelf 和 Miniflux。"
            action={
              <button
                className="primary"
                onClick={() => open?.({ type: "runtime" })}
              >
                <Plus size={15} />
                连接运行时
              </button>
            }
          />
        </div>
      )}
    </>
  );
}
function Jobs({ data, busy, run }: PanelProps) {
  const [tab, setTab] = useState("jobs");
  return (
    <>
      <div className="tabs">
        <button
          className={tab === "jobs" ? "active" : ""}
          onClick={() => setTab("jobs")}
        >
          后台任务<span>{data.jobs.length}</span>
        </button>
        <button
          className={tab === "audits" ? "active" : ""}
          onClick={() => setTab("audits")}
        >
          操作审计
        </button>
        <button
          className={tab === "feedback" ? "active" : ""}
          onClick={() => setTab("feedback")}
        >
          客户端反馈
        </button>
      </div>
      {tab === "feedback" && <FeedbackPanel sources={data.sources} />}
      <div className="panel table-panel" hidden={tab === "feedback"}>
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>{tab === "jobs" ? "任务" : "操作"}</th>
                <th>目标</th>
                <th>{tab === "jobs" ? "状态" : "记录"}</th>
                <th>时间</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {tab === "jobs"
                ? data.jobs.map((j) => (
                    <tr key={j.id}>
                      <td>
                        {actionLabel(j.kind)}
                        <small>{short(j.id)}</small>
                      </td>
                      <td>
                        {data.sources.find((s) => s.id === j.targetId)?.name ??
                          data.runtimes.find((r) => r.id === j.targetId)
                            ?.name ??
                          data.catalogs.find((c) => c.id === j.targetId)
                            ?.name ??
                          short(j.targetId)}
                      </td>
                      <td>
                        <Badge value={j.status} />
                        <small>{j.error ?? ""}</small>
                      </td>
                      <td>
                        {time(j.createdAt)}
                        <small>尝试 {j.attempts} 次</small>
                      </td>
                      <td>
                        {j.status === "failed" && (
                          <button
                            className="secondary"
                            disabled={busy}
                            onClick={() =>
                              run(
                                () => api(`jobs/${j.id}/retry`, "POST"),
                                "已重新加入队列",
                              )
                            }
                          >
                            重试
                          </button>
                        )}
                      </td>
                    </tr>
                  ))
                : [...data.audits]
                    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
                    .slice(0, 200)
                    .map((a) => (
                      <tr key={a.id}>
                        <td>{actionLabel(a.action)}</td>
                        <td>
                          <code>{short(a.targetId)}</code>
                        </td>
                        <td>
                          <ShieldCheck size={16} />
                        </td>
                        <td>{time(a.createdAt)}</td>
                        <td />
                      </tr>
                    ))}
            </tbody>
          </table>
        </div>
        {(tab === "jobs" ? data.jobs : data.audits).length === 0 && (
          <Empty
            title="暂无记录"
            description="同步、体检和发布操作会在这里留下记录。"
          />
        )}
      </div>
    </>
  );
}
function actionLabel(action: string) {
  const map: Record<string, string> = {
    "source.import": "导入源",
    "source.sync": "同步源",
    "source.probe": "源体检",
    "source.approve": "批准源版本",
    "source.enable": "启用源",
    "source.disable": "停用源",
    "source.quarantine": "隔离源",
    "source.release": "解除隔离",
    "source.edit": "更新源配置",
    "source.delete": "删除源",
    "source.rollback": "回滚源版本",
    "source.reject": "拒绝更新",
    "source_set.save": "保存编排组",
    "publication.publish": "发布新版本",
    "publication.rollback": "回滚发布",
    "catalog.save": "保存上游目录",
    "catalog.sync": "同步上游目录",
    "candidate.accept": "接纳候选源",
    "candidate.ignore": "忽略候选源",
    "candidate.block": "屏蔽候选源",
    "binding.create": "绑定客户端",
    "binding.rotate": "轮换访问令牌",
    "binding.revoke": "吊销访问令牌",
    "runtime.save": "保存运行时",
    "runtime.test": "检测运行时",
    "runtime.sync": "同步运行时",
    "secret.replace": "更新加密凭据",
    "job.retry": "重试任务",
  };
  return map[action] ?? action;
}
function useSubmit() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return { busy, error, submit };
}
function NetworkFields({
  network,
  trust,
  change,
}: {
  network: string;
  trust: string;
  change: (key: string, value: string) => void;
}) {
  return (
    <div className="form-grid">
      <Field label="网络区域">
        <select
          value={network}
          onChange={(e) => change("network", e.target.value)}
        >
          <option value="internet">互联网</option>
          <option value="trusted-lan">受信内网</option>
        </select>
      </Field>
      <Field label="信任等级">
        <select value={trust} onChange={(e) => change("trust", e.target.value)}>
          {["untrusted", "reviewed", "trusted"].map((t) => (
            <option key={t} value={t}>
              {label(t)}
            </option>
          ))}
        </select>
      </Field>
    </div>
  );
}
function HeadersField({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <Field
      label="加密请求头（JSON，可选）"
      hint="例如 Authorization 或 X-Api-Key；保存后仅显示头名称，原值不可回读。不要将凭据写进 URL。"
    >
      <textarea
        className="code-input"
        rows={3}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={'{ "Authorization": "Bearer …" }'}
      />
    </Field>
  );
}
function parseHeaders(value: string) {
  if (!value.trim()) return undefined;
  const x = JSON.parse(value);
  if (
    !x ||
    typeof x !== "object" ||
    Array.isArray(x) ||
    Object.values(x).some((v) => typeof v !== "string")
  )
    throw new Error("请求头必须为字符串键值 JSON 对象");
  return x as Record<string, string>;
}
function ImportDialog({
  meta,
  runtimes,
  close,
  saved,
}: {
  meta: Meta;
  runtimes: Runtime[];
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [tab, setTab] = useState("url");
  const [form, setForm] = useState({
    name: "",
    url: "",
    content: "",
    protocol: "",
    network: "internet",
    trust: "reviewed",
    mode: "",
    runtimeId: "",
    updatePolicy: "review",
    intervalMinutes: 360,
  });
  const [headers, setHeaders] = useState("");
  const [preview, setPreview] = useState<Normalized | null>(null);
  const { busy, error, submit } = useSubmit();
  const change = (key: string, value: string | number) => {
    setForm((f) => ({ ...f, [key]: value }));
    setPreview(null);
  };
  const payload = () => ({
    ...form,
    url: tab === "text" || tab === "file" ? "" : form.url,
    content: tab === "url" || tab === "service" ? "" : form.content,
    headers: parseHeaders(headers),
  });
  return (
    <Modal
      title="添加一个新源"
      subtitle="识别协议、预览内容，再交给你审核启用。"
      close={close}
      wide
    >
      <div className="modal-body">
        <div className="tabs">
          {[
            ["url", "粘贴 URL"],
            ["file", "上传文件"],
            ["text", "配置正文"],
            ["service", "连接服务"],
          ].map(([t, n]) => (
            <button
              key={t}
              className={tab === t ? "active" : ""}
              onClick={() => {
                setTab(t);
                setPreview(null);
                setForm((f) => ({ ...f, protocol: "", mode: "" }));
              }}
            >
              {n}
            </button>
          ))}
        </div>
        <Field label="源名称">
          <input
            required
            value={form.name}
            onChange={(e) => change("name", e.target.value)}
            placeholder="例如：家庭直播、我的播客"
          />
        </Field>
        <div className="form-grid">
          <Field label="协议">
            <select
              value={form.protocol}
              onChange={(e) => change("protocol", e.target.value)}
            >
              <option value="">
                {tab === "service" ? "选择服务协议" : "自动识别"}
              </option>
              {tab === "service"
                ? Object.entries(meta.connectors).map(([id, c]) => (
                    <option key={id} value={id}>
                      {c.name}
                    </option>
                  ))
                : meta.adapters
                    .filter((a) => a.protocol !== "catalog")
                    .map((a) => <option key={a.protocol}>{a.protocol}</option>)}
            </select>
          </Field>
          <Field label="更新策略">
            <select
              value={form.updatePolicy}
              onChange={(e) => change("updatePolicy", e.target.value)}
            >
              {["review", "auto", "pinned", "manual"].map((p) => (
                <option key={p} value={p}>
                  {label(p)}
                </option>
              ))}
            </select>
          </Field>
        </div>
        {tab === "url" || tab === "service" ? (
          <Field label={tab === "service" ? "服务基础地址" : "订阅 URL"}>
            <input
              type="url"
              value={form.url}
              onChange={(e) => change("url", e.target.value)}
              placeholder="https://media.example.com/source.json"
            />
          </Field>
        ) : (
          <>
            {tab === "file" && (
              <Field label="配置文件">
                <input
                  type="file"
                  accept=".json,.jsonc,.m3u,.m3u8,.txt,.xml,.opml"
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file)
                      void submit(async () => {
                        if (file.size > 8 * 1024 * 1024)
                          throw new Error("文件不得超过 8 MiB");
                        change("content", await file.text());
                        if (!form.name) change("name", file.name);
                      });
                  }}
                />
              </Field>
            )}
            <Field label="配置正文">
              <textarea
                rows={7}
                className="code-input"
                value={form.content}
                onChange={(e) => change("content", e.target.value)}
                placeholder={
                  "#EXTM3U\n#EXTINF:-1,示例频道\nhttps://media.example.com/live.m3u8"
                }
              />
            </Field>
          </>
        )}
        <details>
          <summary>
            凭据、网络与运行时
            <ChevronDown size={14} />
          </summary>
          <NetworkFields
            network={form.network}
            trust={form.trust}
            change={change}
          />
          <HeadersField value={headers} onChange={setHeaders} />
          <div className="form-grid">
            <Field label="绑定运行时">
              <select
                value={form.runtimeId}
                onChange={(e) => {
                  change("runtimeId", e.target.value);
                  change("mode", e.target.value ? "runtime-backed" : "");
                }}
              >
                <option value="">不绑定，自动选择执行方式</option>
                {runtimes.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name} · {r.driver}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="同步间隔（分钟）">
              <input
                type="number"
                min={5}
                max={43200}
                value={form.intervalMinutes}
                onChange={(e) =>
                  change("intervalMinutes", Number(e.target.value))
                }
              />
            </Field>
          </div>
        </details>
        <ErrorBox error={error} />
        {preview && (
          <div className="preview-box">
            <div className="section-title">
              <h3>
                <Check size={16} />
                已识别 {preview.protocol}
              </h3>
              <span className="tag">{preview.items.length} 条内容</span>
            </div>
            <p>
              {preview.mediaTypes.join(" · ")}
              <br />
              {preview.capabilities.join(" · ")}
            </p>
            {preview.warnings.map((w, i) => (
              <div className="warning" key={i}>
                {w}
              </div>
            ))}
            <ul>
              {preview.items.slice(0, 5).map((i, n) => (
                <li key={n}>{i.name}</li>
              ))}
            </ul>
            <small>导入后进入暂存区，批准版本并启用后才可发布。</small>
          </div>
        )}
      </div>
      <div className="modal-footer">
        <button className="secondary" onClick={close}>
          取消
        </button>
        <button
          className="secondary"
          disabled={busy}
          onClick={() =>
            submit(async () =>
              setPreview(
                await api<Normalized>("sources/preview", "POST", payload()),
              ),
            )
          }
        >
          识别与预览
        </button>
        <button
          className="primary"
          disabled={busy || !preview}
          onClick={() =>
            submit(async () => {
              await api("sources/import", "POST", payload());
              await saved();
            })
          }
        >
          {busy ? (
            <LoaderCircle size={15} className="spin" />
          ) : (
            <ArrowDownToLine size={15} />
          )}
          导入待审核源
        </button>
      </div>
    </Modal>
  );
}
function SourceDialog({
  source,
  data,
  close,
  refresh,
}: {
  source: Source;
  data: Data;
  close: () => void;
  refresh: () => Promise<void>;
}) {
  const [tab, setTab] = useState("overview");
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [probes, setProbes] = useState<Probe[]>([]);
  const [form, setForm] = useState({
    name: source.name,
    url: source.url ?? "",
    protocol: source.protocol,
    mediaTypes: source.mediaTypes,
    mode: source.mode,
    runtimeId: source.runtimeId ?? "",
    network: source.network,
    trust: source.trust,
    updatePolicy: source.updatePolicy,
    intervalMinutes: source.intervalMinutes,
  });
  const [headers, setHeaders] = useState("");
  const [headerNames, setHeaderNames] = useState<string[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [notice, setNotice] = useState("");
  const { busy, error, submit } = useSubmit();
  const load = useCallback(async () => {
    const [rs, ps, hs] = await Promise.all([
      api<Revision[]>(`sources/${source.id}/revisions`),
      api<Probe[]>(`sources/${source.id}/probes`),
      api<{ headerNames: string[] }>(`secrets/${source.id}`),
    ]);
    setRevisions(rs.sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
    setProbes(ps.sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
    setHeaderNames(hs.headerNames);
  }, [source.id]);
  useEffect(() => {
    void submit(load);
  }, [load, source.updatedAt]);
  const action = (a: string, revision?: string) =>
    submit(async () => {
      await api(`sources/${source.id}/${a}`, "POST", { revision });
      await refresh();
      await load();
      setNotice(
        a === "sync" || a === "probe" ? "已加入后台任务队列" : "操作已完成",
      );
    });
  return (
    <Modal
      title={source.name}
      subtitle={source.protocol + " · " + source.mediaTypes.join(" / ")}
      close={close}
      wide
    >
      <div className="modal-body">
        <div className="detail-status">
          <Badge value={source.health} />
          <span>{source.score} 分</span>
          <span>{source.enabled ? "已启用" : "未启用"}</span>
          <code>{short(source.activeRevision)}</code>
        </div>
        <div className="tabs">
          {[
            ["overview", "概览"],
            ["revisions", "版本与差异"],
            ["probes", "健康记录"],
            ["settings", "配置与凭据"],
          ].map(([id, name]) => (
            <button
              key={id}
              className={tab === id ? "active" : ""}
              onClick={() => setTab(id)}
            >
              {name}
            </button>
          ))}
        </div>
        <ErrorBox error={error} />
        {notice && (
          <p className="success-note" role="status">
            {notice}
          </p>
        )}
        {tab === "overview" && (
          <>
            <div className="detail-grid">
              <div>
                <small>执行方式</small>
                <strong>{label(source.mode)}</strong>
              </div>
              <div>
                <small>更新策略</small>
                <strong>{label(source.updatePolicy)}</strong>
              </div>
              <div>
                <small>网络 / 信任</small>
                <strong>
                  {label(source.network)} / {label(source.trust)}
                </strong>
              </div>
              <div>
                <small>最近体检</small>
                <strong>{time(source.lastChecked)}</strong>
              </div>
            </div>
            <Field label="主端点">
              <input readOnly value={source.url ?? "内联配置，无远程端点"} />
            </Field>
            <div className="chip-list">
              {source.capabilities.map((c) => (
                <span className="tag" key={c}>
                  {c}
                </span>
              ))}
            </div>
            {source.stagedRevision && (
              <div className="preview-box">
                <h3>有一个版本等待审核</h3>
                <p>查看版本差异，确认来源与内容后批准。批准不会自动启用源。</p>
                <button
                  className="secondary"
                  onClick={() => setTab("revisions")}
                >
                  查看待审核版本
                  <ArrowRight size={14} />
                </button>
              </div>
            )}
            <div className="button-row spacer">
              <button
                className="primary"
                disabled={busy || (!source.enabled && !source.activeRevision)}
                onClick={() => action(source.enabled ? "disable" : "enable")}
              >
                {source.enabled ? "停用源" : "启用源"}
              </button>
              <button
                className="secondary"
                disabled={busy}
                onClick={() => action("probe")}
              >
                执行体检
              </button>
              <button
                className="secondary"
                disabled={
                  busy || !source.url || source.updatePolicy === "pinned"
                }
                onClick={() => action("sync")}
              >
                同步更新
              </button>
              <button
                className="text-button danger"
                disabled={busy}
                onClick={() =>
                  action(
                    source.health === "quarantined" ? "release" : "quarantine",
                  )
                }
              >
                {source.health === "quarantined" ? "解除隔离" : "隔离源"}
              </button>
            </div>
            <h3 className="spacer">所属编排组</h3>
            <div className="chip-list">
              {data.sets
                .filter((s) => s.members.some((m) => m.sourceId === source.id))
                .map((s) => (
                  <span className="tag" key={s.id}>
                    {s.name}
                  </span>
                ))}
            </div>
          </>
        )}
        {tab === "revisions" &&
          revisions.map((r) => (
            <div className="revision-card" key={r.id}>
              <div className="section-title">
                <div>
                  <code>{short(r.id)}</code> <Badge value={r.status} />
                  {r.id === source.activeRevision && (
                    <span className="tag current">当前</span>
                  )}
                </div>
                <small>{time(r.createdAt)}</small>
              </div>
              <div className="diff-line">
                <span>+ {r.diff.added} 新增</span>
                <span>− {r.diff.removed} 删除</span>
                <span>~ {r.diff.changed} 变更</span>
              </div>
              {r.diff.requiresReview && (
                <div className="warning">
                  检测到大量删除或域名变化，必须人工审核。
                </div>
              )}
              {r.diff.domainChanges?.length > 0 && (
                <p>新增域名：{r.diff.domainChanges.join("、")}</p>
              )}
              <details>
                <summary>
                  规范化内容 · {r.normalized.items.length} 条
                  <ChevronDown size={14} />
                </summary>
                <pre>{JSON.stringify(r.normalized, null, 2)}</pre>
              </details>
              <div className="button-row">
                {r.status === "staged" && r.id === source.stagedRevision ? (
                  <>
                    <button
                      className="primary"
                      disabled={busy}
                      onClick={() => action("approve", r.id)}
                    >
                      批准此版本
                    </button>
                    <button
                      className="secondary"
                      disabled={busy}
                      onClick={() => action("reject")}
                    >
                      拒绝此更新
                    </button>
                  </>
                ) : r.status === "approved" &&
                  r.id !== source.activeRevision ? (
                  <button
                    className="secondary"
                    disabled={busy}
                    onClick={() => action("rollback", r.id)}
                  >
                    回滚并固定此版本
                  </button>
                ) : null}
              </div>
            </div>
          ))}
        {tab === "probes" &&
          (probes.length ? (
            probes.map((p) => (
              <div className="activity-row" key={p.id}>
                <Badge value={p.success ? "healthy" : "failing"} />
                <div className="grow">
                  <b>
                    {label(p.level)} · {p.latencyMs} ms
                  </b>
                  <small>{p.checks.join(" → ")}</small>
                  <small>{p.code}</small>
                </div>
                <time>{time(p.createdAt)}</time>
              </div>
            ))
          ) : (
            <Empty
              title="还没有体检记录"
              description="结构校验和功能抽样会分别标记，不代表所有条目都已测试。"
            />
          ))}
        {tab === "settings" && (
          <>
            <Field label="源名称">
              <input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </Field>
            <div className="form-grid">
              <Field label="执行方式">
                <select
                  value={form.mode}
                  onChange={(e) => setForm({ ...form, mode: e.target.value })}
                >
                  {[
                    "compiled",
                    "direct-client",
                    "runtime-backed",
                    "catalog-only",
                  ].map((m) => (
                    <option key={m} value={m}>
                      {label(m)}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="绑定运行时">
                <select
                  value={form.runtimeId}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      runtimeId: e.target.value,
                      mode: e.target.value ? "runtime-backed" : form.mode,
                    })
                  }
                >
                  <option value="">不绑定</option>
                  {data.runtimes.map((rt) => (
                    <option value={rt.id} key={rt.id}>
                      {rt.name}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            <NetworkFields
              network={form.network}
              trust={form.trust}
              change={(k, v) => setForm({ ...form, [k]: v })}
            />
            <div className="form-grid">
              <Field label="更新策略">
                <select
                  value={form.updatePolicy}
                  onChange={(e) =>
                    setForm({ ...form, updatePolicy: e.target.value })
                  }
                >
                  {["review", "auto", "pinned", "manual"].map((p) => (
                    <option key={p} value={p}>
                      {label(p)}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="同步间隔（分钟）">
                <input
                  type="number"
                  min={5}
                  value={form.intervalMinutes}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      intervalMinutes: Number(e.target.value),
                    })
                  }
                />
              </Field>
            </div>
            <HeadersField value={headers} onChange={setHeaders} />
            <small>
              已有请求头：{headerNames.join(", ") || "无"}。留空保留原值，输入{" "}
              {"{}"} 清空。
            </small>
            <div className="card-actions">
              <button
                className="primary"
                disabled={busy}
                onClick={() =>
                  submit(async () => {
                    await api(`sources/${source.id}`, "PUT", {
                      ...form,
                      headers: parseHeaders(headers),
                    });
                    await refresh();
                    await load();
                    setHeaders("");
                    setNotice("配置已保存");
                  })
                }
              >
                保存配置
              </button>
              <button
                className="text-button danger"
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 size={14} />
                删除源
              </button>
            </div>
            {confirmDelete && (
              <div className="warning">
                删除源及其体检、版本记录；已经发布的快照仍保留。请先从编排组移除。
                <div className="button-row">
                  <button
                    className="secondary danger"
                    disabled={busy}
                    onClick={() =>
                      submit(async () => {
                        await api(`sources/${source.id}`, "DELETE");
                        await refresh();
                        close();
                      })
                    }
                  >
                    确认删除
                  </button>
                  <button
                    className="text-button"
                    onClick={() => setConfirmDelete(false)}
                  >
                    取消
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </Modal>
  );
}
function SetDialog({
  set,
  sources,
  close,
  saved,
}: {
  set?: SourceSet;
  sources: Source[];
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [name, setName] = useState(set?.name ?? "");
  const [description, setDescription] = useState(set?.description ?? "");
  const [members, setMembers] = useState(set?.members ?? []);
  const { busy, error, submit } = useSubmit();
  return (
    <Modal
      title={set ? "编辑源编排组" : "新建源编排组"}
      subtitle="优先级越高越靠前；低于健康阈值的源不会进入新发布。"
      close={close}
      wide
    >
      <div className="modal-body">
        <div className="form-grid">
          <Field label="编排组名称">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="家庭默认"
            />
          </Field>
          <Field label="描述">
            <input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="家里的看、读、听、说"
            />
          </Field>
        </div>
        <div className="section-title">
          <h3>选择源与顺序</h3>
          <span className="tag">{members.length} 个成员</span>
        </div>
        {sources.map((src) => {
          const m = members.find((x) => x.sourceId === src.id);
          const change = (k: string, v: unknown) =>
            setMembers(
              members.map((x) =>
                x.sourceId === src.id ? { ...x, [k]: v } : x,
              ),
            );
          return (
            <div
              className={"member-editor " + (m ? "selected" : "")}
              key={src.id}
            >
              <div className="member-heading">
                <label>
                  <input
                    type="checkbox"
                    checked={!!m}
                    onChange={(e) =>
                      setMembers(
                        e.target.checked
                          ? [
                              ...members,
                              defaultMember(
                                src.id,
                                Math.max(0, 100 - members.length * 10),
                              ),
                            ]
                          : members.filter((x) => x.sourceId !== src.id),
                      )
                    }
                  />
                  <strong>{src.name}</strong>
                  <code>{src.protocol}</code>
                </label>
                <Badge value={src.health} />
              </div>
              {m && (
                <>
                  <div className="form-grid member-grid">
                    <Field label="优先级">
                      <input
                        type="number"
                        min={0}
                        max={10000}
                        value={m.priority}
                        onChange={(e) =>
                          change("priority", Number(e.target.value))
                        }
                      />
                    </Field>
                    <Field label="角色">
                      <select
                        value={m.role}
                        onChange={(e) => change("role", e.target.value)}
                      >
                        {["primary", "backup", "auxiliary"].map((r) => (
                          <option key={r} value={r}>
                            {label(r)}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label="最低健康分">
                      <input
                        type="number"
                        min={0}
                        max={100}
                        value={m.minScore}
                        onChange={(e) =>
                          change("minScore", Number(e.target.value))
                        }
                      />
                    </Field>
                    <Field label="权重">
                      <input
                        type="number"
                        min={1}
                        max={10000}
                        value={m.weight}
                        onChange={(e) =>
                          change("weight", Number(e.target.value))
                        }
                      />
                    </Field>
                  </div>
                  <details>
                    <summary>
                      过滤条件与客户端约束
                      <ChevronDown size={13} />
                    </summary>
                    <div className="form-grid">
                      {[
                        ["mediaTypes", "媒体类型"],
                        ["languages", "语言"],
                        ["regions", "地区"],
                        ["devices", "设备"],
                        ["networks", "网络"],
                      ].map(([k, n]) => (
                        <Field key={k} label={n + "（逗号分隔）"}>
                          <input
                            defaultValue={(
                              (m[k as keyof typeof m] as string[]) ?? []
                            ).join(",")}
                            onBlur={(e) =>
                              change(
                                k,
                                e.target.value
                                  .split(",")
                                  .map((s) => s.trim())
                                  .filter(Boolean),
                              )
                            }
                          />
                        </Field>
                      ))}
                      <Field label="超时（毫秒）">
                        <input
                          type="number"
                          min={100}
                          max={120000}
                          value={m.timeoutMs}
                          onChange={(e) =>
                            change("timeoutMs", Number(e.target.value))
                          }
                        />
                      </Field>
                      <Field label="最大并发">
                        <input
                          type="number"
                          min={1}
                          max={32}
                          value={m.maxConcurrency}
                          onChange={(e) =>
                            change("maxConcurrency", Number(e.target.value))
                          }
                        />
                      </Field>
                    </div>
                    <small>
                      设备和网络条件由 Bundle
                      客户端选择时使用；无法表达这些约束的传统格式会排除该成员。
                    </small>
                  </details>
                </>
              )}
            </div>
          );
        })}
        {!sources.length && (
          <Empty
            title="先添加一个源"
            description="关闭此窗口，在源库导入并审核你的媒体源。"
          />
        )}
        <ErrorBox error={error} />
      </div>
      <div className="modal-footer">
        <button className="secondary" onClick={close}>
          取消
        </button>
        <button
          className="primary"
          disabled={busy || !name || members.length === 0}
          onClick={() =>
            submit(async () => {
              await api(
                set ? `source-sets/${set.id}` : "source-sets",
                set ? "PUT" : "POST",
                { name, description, members },
              );
              await saved();
            })
          }
        >
          保存编排组
        </button>
      </div>
    </Modal>
  );
}
function RuntimeDialog({
  runtime,
  meta,
  close,
  saved,
}: {
  runtime?: Runtime;
  meta: Meta;
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [form, setForm] = useState({
    name: runtime?.name ?? "",
    driver: runtime?.driver ?? "emby",
    url: runtime?.url ?? "",
    network: runtime?.network ?? "internet",
    trust: runtime?.trust ?? "reviewed",
  });
  const [headers, setHeaders] = useState("");
  const { busy, error, submit } = useSubmit();
  return (
    <Modal title={runtime ? "运行时设置" : "连接领域运行时"} close={close}>
      <div className="modal-body">
        <Field label="显示名称">
          <input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="我的媒体库"
          />
        </Field>
        <Field label="服务类型">
          <select
            value={form.driver}
            disabled={!!runtime}
            onChange={(e) => setForm({ ...form, driver: e.target.value })}
          >
            {Object.entries(meta.connectors).map(([key, c]) => (
              <option key={key} value={key}>
                {c.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="基础 URL">
          <input
            type="url"
            value={form.url}
            onChange={(e) => setForm({ ...form, url: e.target.value })}
            placeholder="https://media.example.com"
          />
        </Field>
        <NetworkFields
          network={form.network}
          trust={form.trust}
          change={(k, v) => setForm({ ...form, [k]: v })}
        />
        <HeadersField value={headers} onChange={setHeaders} />
        <small>
          保存后执行连接检测。运行时管理操作与内容鉴权仍由原服务负责。
        </small>
        <ErrorBox error={error} />
      </div>
      <div className="modal-footer">
        <button className="secondary" onClick={close}>
          取消
        </button>
        <button
          className="primary"
          disabled={busy}
          onClick={() =>
            submit(async () => {
              await api(
                runtime ? `runtimes/${runtime.id}` : "runtimes",
                runtime ? "PUT" : "POST",
                { ...form, headers: parseHeaders(headers) },
              );
              await saved();
            })
          }
        >
          保存连接
        </button>
      </div>
    </Modal>
  );
}
function CatalogDialog({
  catalog,
  close,
  saved,
}: {
  catalog?: Catalog;
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [form, setForm] = useState({
    name: catalog?.name ?? "",
    url: catalog?.url ?? "",
    network: catalog?.network ?? "internet",
    trust: catalog?.trust ?? "reviewed",
    intervalMinutes: catalog?.intervalMinutes ?? 360,
    enabled: catalog?.enabled ?? true,
  });
  const { busy, error, submit } = useSubmit();
  return (
    <Modal title={catalog ? "上游目录设置" : "订阅上游目录"} close={close}>
      <div className="modal-body">
        <Field label="目录名称">
          <input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </Field>
        <Field
          label="目录 URL"
          hint="支持 [{name, url, protocol?}]、{entries: [...]}、OPML、TVBox 多仓。"
        >
          <input
            type="url"
            value={form.url}
            onChange={(e) => setForm({ ...form, url: e.target.value })}
            placeholder="https://sources.example.com/catalog.json"
          />
        </Field>
        <NetworkFields
          network={form.network}
          trust={form.trust}
          change={(k, v) => setForm({ ...form, [k]: v })}
        />
        <Field label="同步间隔（分钟）">
          <input
            type="number"
            min={5}
            max={43200}
            value={form.intervalMinutes}
            onChange={(e) =>
              setForm({ ...form, intervalMinutes: Number(e.target.value) })
            }
          />
        </Field>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
          />
          启用定时同步
        </label>
        <ErrorBox error={error} />
      </div>
      <div className="modal-footer">
        <button className="secondary" onClick={close}>
          取消
        </button>
        <button
          className="primary"
          disabled={busy}
          onClick={() =>
            submit(async () => {
              await api(
                catalog ? `catalogs/${catalog.id}` : "catalogs",
                catalog ? "PUT" : "POST",
                form,
              );
              await saved();
            })
          }
        >
          保存目录
        </button>
      </div>
    </Modal>
  );
}
function BindingDialog({
  data,
  close,
  saved,
}: {
  data: Data;
  close: () => void;
  saved: (url: string, formats: string[]) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [setId, setSetId] = useState(
    data.sets.find((s) => s.currentPublication)?.id ?? data.sets[0]?.id ?? "",
  );
  const [days, setDays] = useState(90);
  const [formats, setFormats] = useState(data.meta.formats);
  const { busy, error, submit } = useSubmit();
  return (
    <Modal
      title="绑定客户端"
      subtitle="每个设备使用独立令牌，可单独轮换或吊销。"
      close={close}
    >
      <div className="modal-body">
        <Field label="客户端名称">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="例如：客厅电视"
          />
        </Field>
        <Field label="源编排组">
          <select value={setId} onChange={(e) => setSetId(e.target.value)}>
            {data.sets.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
                {!s.currentPublication ? "（尚未发布）" : ""}
              </option>
            ))}
          </select>
        </Field>
        <Field label="有效期（天）">
          <input
            type="number"
            min={1}
            max={3650}
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
          />
        </Field>
        <h3>允许读取的格式</h3>
        <div className="formats-list">
          {data.meta.formats.map((f) => (
            <label key={f}>
              <input
                type="checkbox"
                checked={formats.includes(f)}
                onChange={(e) =>
                  setFormats(
                    e.target.checked
                      ? [...formats, f]
                      : formats.filter((x) => x !== f),
                  )
                }
              />
              <code>{f}</code>
            </label>
          ))}
        </div>
        <ErrorBox error={error} />
      </div>
      <div className="modal-footer">
        <button className="secondary" onClick={close}>
          取消
        </button>
        <button
          className="primary"
          disabled={
            busy || !setId || formats.length === 0 || days < 1 || days > 3650
          }
          onClick={() =>
            submit(async () => {
              const res = await api<{ baseUrl: string }>("bindings", "POST", {
                name,
                setId,
                formats,
                expiresAt: new Date(Date.now() + days * 86400000).toISOString(),
              });
              await saved(res.baseUrl, formats);
            })
          }
        >
          创建绑定
        </button>
      </div>
    </Modal>
  );
}
function TokenDialog({
  baseUrl,
  formats,
  close,
}: {
  baseUrl: string;
  formats: string[];
  close: () => void;
}) {
  const [selected, setSelected] = useState(
    formats.includes("shadow.json") ? "shadow.json" : formats[0],
  );
  const [qr, setQR] = useState("");
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const url = baseUrl + "/" + selected;
  useEffect(() => {
    let active = true;
    QRCode.toDataURL(url, {
      width: 240,
      margin: 2,
      color: { dark: "#303d30", light: "#ffffff" },
    })
      .then((q) => {
        if (active) setQR(q);
      })
      .catch(() => setError("二维码生成失败，请复制地址"));
    return () => {
      active = false;
    };
  }, [url]);
  return (
    <Modal
      title="客户端订阅已创建"
      subtitle="访问地址只显示这一次，请现在保存。关闭后可通过轮换生成新地址。"
      close={close}
    >
      <div className="modal-body">
        <div className="qr-display">
          {qr && <img src={qr} alt="客户端订阅地址二维码" />}
          <span>扫描或复制，添加到你的客户端</span>
        </div>
        <Field label="订阅格式">
          <select
            value={selected}
            onChange={(e) => {
              setSelected(e.target.value);
              setCopied(false);
            }}
          >
            {formats.map((f) => (
              <option key={f}>{f}</option>
            ))}
          </select>
        </Field>
        <div className="copy-field">
          <input aria-label="订阅地址" readOnly value={url} />
          <button
            className="secondary"
            onClick={() => {
              navigator.clipboard
                .writeText(url)
                .then(() => setCopied(true))
                .catch(() => setError("无法使用剪贴板，请选中地址手动复制"));
            }}
          >
            {copied ? <Check size={16} /> : <Copy size={16} />}
          </button>
        </div>
        <small>
          只有发布版本实际包含的格式才会返回内容。不可变版本地址：在格式前加入
          v/发布ID/。
        </small>
        <ErrorBox error={error} />
      </div>
      <div className="modal-footer">
        <button className="primary" onClick={close}>
          已保存，完成绑定
        </button>
      </div>
    </Modal>
  );
}
function PublicationDialog({
  publication,
  close,
}: {
  publication: Publication;
  close: () => void;
}) {
  const [p, setP] = useState(publication);
  const [path, setPath] = useState("shadow.json");
  const { error, submit } = useSubmit();
  useEffect(() => {
    void submit(async () =>
      setP(await api<Publication>(`publications/${publication.id}`)),
    );
  }, [publication.id]);
  return (
    <Modal
      title={"发布版本 " + short(p.id)}
      subtitle={p.revision}
      close={close}
      wide
    >
      <div className="modal-body">
        <Field label="发布文件">
          <select value={path} onChange={(e) => setPath(e.target.value)}>
            {Object.keys(p.artifacts)
              .sort()
              .map((f) => (
                <option key={f}>{f}</option>
              ))}
          </select>
        </Field>
        <pre>
          {(() => {
            const body = p.artifacts[path]?.body ?? "";
            try {
              return JSON.stringify(JSON.parse(body), null, 2);
            } catch {
              return body;
            }
          })()}
        </pre>
        {Object.keys(p.exclusions).length > 0 && (
          <>
            <h3>本次排除的源</h3>
            {Object.entries(p.exclusions).map(([id, reason]) => (
              <p key={id}>
                <code>{short(id)}</code> · {reason}
              </p>
            ))}
          </>
        )}
        <ErrorBox error={error} />
      </div>
    </Modal>
  );
}

function FeedbackPanel({ sources }: { sources: Source[] }) {
  const [reports, setReports] = useState<
    {
      id: string;
      sourceId: string;
      bindingId: string;
      code: string;
      createdAt: string;
    }[]
  >([]);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api<typeof reports>("feedback")
      .then((value) => {
        if (active)
          setReports(
            value.sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
          );
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, []);
  const names: Record<string, string> = {
    timeout: "请求超时",
    unavailable: "源不可用",
    parse_error: "解析失败",
    unauthorized: "需要鉴权",
    unsupported: "客户端不支持",
  };
  return (
    <div className="panel">
      <ErrorBox error={error} />
      <div className="section-title">
        <h2>脱敏错误报告</h2>
        <span className="tag">不包含内容与 URL</span>
      </div>
      {reports.length ? (
        reports.slice(0, 200).map((r) => (
          <div className="activity-row" key={r.id}>
            <Activity size={16} />
            <div>
              <b>
                {sources.find((s) => s.id === r.sourceId)?.name ??
                  short(r.sourceId)}
              </b>
              <small>
                {names[r.code] ?? r.code} · 客户端 {short(r.bindingId)}
              </small>
            </div>
            <time>{time(r.createdAt)}</time>
          </div>
        ))
      ) : (
        <Empty
          title="暂无客户端反馈"
          description="客户端可提交错误类型，Relay 不接收播放内容、完整地址或凭据。"
        />
      )}
    </div>
  );
}
