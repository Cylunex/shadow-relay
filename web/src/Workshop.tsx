import { useEffect, useState } from "react";
import {
  ArrowDownToLine,
  BookOpen,
  ExternalLink,
  RefreshCw,
  WandSparkles,
} from "lucide-react";
import { api, apiDownload } from "./api";
import { Badge, Empty, ErrorBox, External, Field } from "./ui";
import type { Data, Revision, SourceSet, Item, ChannelRule } from "./types";

export type ImportSeed = {
  name: string;
  url?: string;
  protocol?: string;
  content?: string;
};
type PluginReport = {
  supported: number;
  unsupported: number;
  entries: {
    id: string;
    name: string;
    blockers: string[];
    warnings: string[];
    recipe?: unknown;
  }[];
};
type Recipe = {
  id: string;
  name: string;
  category: string;
  projectUrl: string;
  feedUrl?: string;
  protocol?: string;
  kind: string;
  coverage: string;
  note: string;
};

function useAction() {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return { busy, error, notice, setNotice, run };
}

export function BookWorkshop({
  data,
  importSource,
}: {
  data: Data;
  importSource: (seed: ImportSeed) => void;
}) {
  const [tab, setTab] = useState("sources"),
    [selected, setSelected] = useState(""),
    [content, setContent] = useState("");
  const [report, setReport] = useState<PluginReport | null>(null);
  const action = useAction();
  const sources = data.sources.filter((s) =>
    ["legado-book", "so-novel", "relay-book"].includes(s.protocol),
  );
  return (
    <div className="workshop">
      <section className="workshop-intro">
        <BookOpen size={26} />
        <div>
          <h2>从规则到一条聚合书源</h2>
          <p>
            转换并发布标准插件 → 同步到官方 Hub → 运行真实体检 → 阅读订阅 Hub
            的专属入口。Hub 负责搜索、缓存与换源。
          </p>
        </div>
      </section>
      <div className="tabs">
        {[
          ["sources", "已有书源"],
          ["convert", "检查规则包"],
          ["create", "新建站点规则"],
        ].map(([id, text]) => (
          <button
            key={id}
            className={tab === id ? "active" : ""}
            onClick={() => {
              setTab(id);
              setReport(null);
            }}
          >
            {text}
          </button>
        ))}
      </div>
      {tab === "sources" && (
        <section className="panel workshop-card">
          <Field label="选择书源包">
            <select
              value={selected}
              onChange={(e) => {
                setSelected(e.target.value);
                setReport(null);
              }}
            >
              <option value="">请选择</option>
              {sources.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} · {s.protocol}
                </option>
              ))}
            </select>
          </Field>
          <div className="button-row">
            <button
              className="primary"
              disabled={!selected || action.busy}
              onClick={() =>
                action.run(async () =>
                  setReport(
                    await api<PluginReport>(`sources/${selected}/book-plugins`),
                  ),
                )
              }
            >
              查看兼容报告
            </button>
            <button
              className="secondary"
              disabled={!selected || action.busy}
              onClick={() =>
                action.run(async () => {
                  await apiDownload(
                    `sources/${selected}/hub.zip`,
                    "shadow-relay-hub-plugins.zip",
                  );
                  action.setNotice(
                    "插件包已下载；包含可转换规则与逐条兼容报告，smoke fixture 初始为待采集草稿。",
                  );
                })
              }
            >
              <ArrowDownToLine size={15} />
              下载 Hub 插件
            </button>
          </div>
          <p className="muted">
            下载默认使用待审核版本，方便检查。自动同步只消费编排组已批准并发布的
            hub/plugins.json。
          </p>
        </section>
      )}
      {tab === "convert" && (
        <section className="panel workshop-card">
          <Field label="阅读 JSON / so-novel 规则">
            <textarea
              className="code-input"
              rows={12}
              value={content}
              onChange={(e) => {
                setContent(e.target.value);
                setReport(null);
              }}
              placeholder="粘贴单条规则或数组，先查看可转换范围"
            />
          </Field>
          <div className="button-row">
            <button
              className="primary"
              disabled={!content || action.busy}
              onClick={() =>
                action.run(async () =>
                  setReport(
                    await api<PluginReport>("book-tools/convert", "POST", {
                      content,
                      name: "规则兼容检查",
                    }),
                  ),
                )
              }
            >
              检查兼容性
            </button>
            <button
              className="secondary"
              disabled={!report}
              onClick={() => importSource({ name: "书源规则包", content })}
            >
              进入导入预览
            </button>
          </div>
        </section>
      )}
      {tab === "create" && <RecipeBuilder importSource={importSource} />}
      {report && (
        <section className="panel workshop-card">
          <div className="section-title">
            <h3>
              {report.supported} 条可转换 · {report.unsupported} 条需人工处理
            </h3>
            <span className="tag">尚未进行实网验证</span>
          </div>
          {report.entries.map((entry, index) => (
            <div className="compatibility-entry" key={entry.id + index}>
              <div>
                <Badge value={entry.blockers.length ? "blocked" : "approved"} />
                <b>{entry.name || "未命名规则"}</b>
                <code>{entry.id}</code>
              </div>
              {entry.blockers.length > 0 && (
                <ul>
                  {entry.blockers.map((b, i) => (
                    <li key={i}>{b}</li>
                  ))}
                </ul>
              )}
              {entry.warnings.length > 0 && (
                <details>
                  <summary>转换说明 ({entry.warnings.length})</summary>
                  <ul>
                    {entry.warnings.map((w, i) => (
                      <li key={i}>{w}</li>
                    ))}
                  </ul>
                </details>
              )}
            </div>
          ))}
        </section>
      )}
      {action.notice && <div className="info-box">{action.notice}</div>}
      <ErrorBox error={action.error} />
    </div>
  );
}

function RecipeBuilder({
  importSource,
}: {
  importSource: (seed: ImportSeed) => void;
}) {
  const [form, setForm] = useState({
    name: "",
    baseUrl: "https://example.com",
    searchUrl: "/search?q={keyword}&page={page}",
    searchList: ".book",
    searchName: ".title",
    searchBookUrl: "a",
    searchAuthor: ".author",
    detailName: "h1",
    detailAuthor: ".author",
    tocUrl: "",
    tocList: ".chapters a",
    tocTitle: "$self",
    tocNext: "",
    chapterContent: "#content",
    chapterTitle: "h1",
    chapterNext: "",
    smokeKeyword: "",
  });
  const action = useAction();
  const css = (value: string, attr?: string, html = false) =>
    value
      ? { css: value, ...(attr ? { attr } : {}), ...(html ? { html } : {}) }
      : {};
  const recipe = () => ({
    schema: "shadow.book.recipe/v1",
    name: form.name,
    baseUrl: form.baseUrl,
    domains: [new URL(form.baseUrl).hostname],
    maxPages: 50,
    minIntervalMs: 1200,
    smokeKeyword: form.smokeKeyword,
    search: {
      url: form.searchUrl,
      method: "GET",
      list: css(form.searchList),
      fields: {
        name: css(form.searchName),
        bookUrl: css(form.searchBookUrl, "href"),
        author: css(form.searchAuthor),
      },
      next: {},
    },
    detail: {
      list: {},
      fields: {
        name: css(form.detailName),
        author: css(form.detailAuthor),
        tocUrl: css(form.tocUrl, "href"),
      },
      next: {},
    },
    toc: {
      list: css(form.tocList),
      fields: { title: css(form.tocTitle), chapterUrl: css("$self", "href") },
      next: css(form.tocNext, "href"),
    },
    chapter: {
      list: {},
      fields: {
        title: css(form.chapterTitle),
        content: css(form.chapterContent, undefined, true),
      },
      next: css(form.chapterNext, "href"),
    },
  });
  const fields: [keyof typeof form, string][] = [
    ["name", "站点名称"],
    ["baseUrl", "站点入口"],
    ["searchUrl", "搜索地址模板"],
    ["smokeKeyword", "体检书名"],
    ["searchList", "搜索结果列表 CSS"],
    ["searchName", "书名 CSS"],
    ["searchBookUrl", "详情链接 CSS（读取 href）"],
    ["searchAuthor", "搜索作者 CSS"],
    ["detailName", "详情书名 CSS"],
    ["detailAuthor", "详情作者 CSS"],
    ["tocUrl", "目录链接 CSS（同页留空）"],
    ["tocList", "完整目录链接 CSS"],
    ["tocTitle", "章节标题 CSS（$self 为当前链接）"],
    ["tocNext", "目录下一页 CSS（无分页留空）"],
    ["chapterContent", "正文区域 CSS"],
    ["chapterTitle", "正文标题 CSS"],
    ["chapterNext", "正文下一页 CSS（无分页留空）"],
  ];
  return (
    <section className="panel workshop-card">
      <h3>
        <WandSparkles size={18} /> 站点规则脚手架
      </h3>
      <p className="muted">
        填写实际页面的选择器。JSONPath、POST 表单、倒序目录可在生成的规则 JSON
        中配置。
      </p>
      <div className="form-grid">
        {fields.map(([key, caption]) => (
          <Field key={key} label={caption}>
            <input
              value={form[key]}
              onChange={(e) => setForm({ ...form, [key]: e.target.value })}
            />
          </Field>
        ))}
      </div>
      <div className="button-row">
        <button
          className="primary"
          disabled={action.busy || !form.name}
          onClick={() =>
            action.run(async () =>
              importSource({
                name: form.name,
                content: JSON.stringify(recipe(), null, 2),
                protocol: "relay-book",
              }),
            )
          }
        >
          预览并加入源库
        </button>
        <button
          className="secondary"
          disabled={action.busy || !form.name}
          onClick={() =>
            action.run(async () =>
              apiDownload(
                "book-tools/scaffold",
                "shadow-relay-site-plugin.zip",
                "POST",
                recipe(),
              ),
            )
          }
        >
          下载插件脚手架
        </button>
      </div>
      <ErrorBox error={action.error} />
    </section>
  );
}

export function ReferenceCatalog({
  importSource,
  refresh,
}: {
  importSource: (seed: ImportSeed) => void;
  refresh: () => Promise<void>;
}) {
  const [recipes, setRecipes] = useState<Recipe[]>([]),
    [filter, setFilter] = useState("全部"),
    [search, setSearch] = useState("");
  const action = useAction();
  useEffect(() => {
    void action.run(async () => setRecipes(await api<Recipe[]>("recipes")));
  }, []);
  return (
    <>
      <section className="workshop-intro">
        <ExternalLink size={26} />
        <div>
          <h2>参考项目与接入配方</h2>
          <p>
            原始沟通提及的项目逐一保留去向。协议接入表示消费它的标准输出；尚无直接接口的项目明确标注为外部工具。
          </p>
        </div>
      </section>
      <div className="filter-bar">
        <input
          aria-label="搜索参考项目"
          placeholder="搜索项目或能力"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="项目分类"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        >
          {["全部", "看", "读", "听", "漫", "工具"].map((v) => (
            <option key={v}>{v}</option>
          ))}
        </select>
      </div>
      <ErrorBox error={action.error} />
      {action.notice && <div className="info-box">{action.notice}</div>}
      <div className="reference-grid">
        {recipes
          .filter(
            (r) =>
              (filter === "全部" || r.category === filter) &&
              (r.name + r.note).toLowerCase().includes(search.toLowerCase()),
          )
          .map((r) => (
            <section className="panel workshop-card" key={r.id}>
              <div className="section-title">
                <h3>{r.name}</h3>
                <span className="tag">{r.coverage}</span>
              </div>
              <p>{r.note}</p>
              <div className="button-row">
                <External url={r.projectUrl}>上游项目</External>
                {r.feedUrl && (
                  <button
                    className="secondary"
                    disabled={action.busy}
                    onClick={() =>
                      r.kind === "catalog"
                        ? action.run(async () => {
                            await api("catalogs", "POST", {
                              name: r.name,
                              url: r.feedUrl,
                              enabled: true,
                              network: "internet",
                              trust: "reviewed",
                              intervalMinutes: 360,
                            });
                            await refresh();
                            action.setNotice(
                              "已加入目录订阅；发现的链接会进入候选箱，审核后接纳。",
                            );
                          })
                        : importSource({
                            name: r.name,
                            url: r.feedUrl,
                            protocol: r.protocol,
                          })
                    }
                  >
                    {r.kind === "catalog" ? "订阅目录" : "预览导入"}
                  </button>
                )}
              </div>
            </section>
          ))}
      </div>
    </>
  );
}

export function LiveChannels({
  data,
  refresh,
}: {
  data: Data;
  refresh: () => Promise<void>;
}) {
  const [selected, setSelected] = useState(""),
    [draft, setDraft] = useState<SourceSet | null>(null);
  const [channels, setChannels] = useState<
      (Item & { sourceId: string; sourceName: string })[]
    >([]),
    [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const action = useAction();
  async function load(id: string) {
    setSelected(id);
    setPage(0);
    setChannels([]);
    const set = data.sets.find((s) => s.id === id);
    setDraft(
      set ? { ...set, channelRules: [...(set.channelRules ?? [])] } : null,
    );
    if (!set) {
      setChannels([]);
      return;
    }
    await action.run(async () => {
      const members = set.members
        .map((m) => data.sources.find((s) => s.id === m.sourceId))
        .filter((s) => s?.protocol === "m3u" && s.activeRevision);
      const lists = await Promise.all(
        members.map(async (src) => {
          const revs = await api<Revision[]>(`sources/${src!.id}/revisions`);
          const rev = revs.find((r) => r.id === src!.activeRevision);
          return (rev?.normalized.items ?? []).map((item) => ({
            ...item,
            sourceId: src!.id,
            sourceName: src!.name,
          }));
        }),
      );
      setChannels(lists.flat());
    });
  }
  const shown = channels.filter((c) =>
    (c.name + c.group + c.sourceName)
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  function update(
    item: (typeof channels)[number],
    change: Partial<ChannelRule>,
  ) {
    if (!draft) return;
    const match = item.url ?? item.id ?? "";
    const rules = [...(draft.channelRules ?? [])];
    const index = rules.findIndex(
      (r) => r.sourceId === item.sourceId && r.match === match,
    );
    const rule = {
      sourceId: item.sourceId,
      match,
      hide: false,
      ...(index >= 0 ? rules[index] : {}),
      ...change,
    };
    if (index >= 0) rules[index] = rule;
    else rules.push(rule);
    setDraft({ ...draft, channelRules: rules });
  }
  return (
    <>
      <section className="workshop-intro">
        <RefreshCw size={26} />
        <div>
          <h2>把频道整理成自己的直播表</h2>
          <p>
            按编排组设置名称、分组、台标、EPG ID
            和隐藏规则。源更新时保留覆盖规则，发布时同步生成 M3U 与 TXT。
          </p>
        </div>
      </section>
      <section className="panel workshop-card">
        <div className="form-grid">
          <Field label="直播编排组">
            <select
              value={selected}
              disabled={action.busy}
              onChange={(e) => void load(e.target.value)}
            >
              <option value="">选择编排组</option>
              {data.sets.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="筛选频道">
            <input
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(0);
              }}
              placeholder="频道 / 分组 / 来源"
            />
          </Field>
        </div>
        <div className="button-row">
          <span>
            {shown.length} 个频道 · {draft?.channelRules?.length ?? 0} 条覆盖
          </span>
          <button
            className="primary"
            disabled={!draft || action.busy}
            onClick={() =>
              action.run(async () => {
                const saved = await api<SourceSet>(
                  `source-sets/${draft!.id}`,
                  "PUT",
                  draft,
                );
                setDraft(saved);
                await refresh();
                action.setNotice("频道覆盖已保存，发布编排组后生效。");
              })
            }
          >
            保存频道编排
          </button>
          <button
            className="secondary"
            disabled={!draft || action.busy}
            onClick={() => void load(selected)}
          >
            重新载入
          </button>
        </div>
        <ErrorBox error={action.error} />
        {action.notice && <div className="info-box">{action.notice}</div>}
      </section>
      {draft && (
        <section className="panel channel-table">
          <table>
            <thead>
              <tr>
                <th>频道 / 来源</th>
                <th>显示名称</th>
                <th>分组</th>
                <th>EPG ID</th>
                <th>台标 URL</th>
                <th>隐藏</th>
              </tr>
            </thead>
            <tbody>
              {shown.slice(page * 50, page * 50 + 50).map((item, index) => {
                const r = draft.channelRules?.find(
                  (r) =>
                    r.sourceId === item.sourceId &&
                    r.match === (item.url ?? item.id),
                );
                return (
                  <tr key={item.sourceId + (item.url ?? index)}>
                    <td>
                      <strong>{item.name}</strong>
                      <small>{item.sourceName}</small>
                    </td>
                    {(
                      [
                        ["name", item.name],
                        ["group", item.group ?? ""],
                        ["tvgId", item.id ?? ""],
                        ["logo", item.logo ?? ""],
                      ] as const
                    ).map(([key, fallback]) => (
                      <td key={key}>
                        <input
                          aria-label={`${item.name} ${key}`}
                          value={r?.[key] ?? ""}
                          placeholder={fallback}
                          onChange={(e) =>
                            update(item, { [key]: e.target.value })
                          }
                        />
                      </td>
                    ))}
                    <td>
                      <input
                        type="checkbox"
                        aria-label={`隐藏 ${item.name}`}
                        checked={r?.hide ?? false}
                        onChange={(e) =>
                          update(item, { hide: e.target.checked })
                        }
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {shown.length === 0 && (
            <Empty
              title="暂无可编排频道"
              description="把已批准的 M3U 源加入这个编排组。"
            />
          )}
          <div className="button-row">
            <button
              className="secondary"
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
            >
              上一页
            </button>
            <span>
              {page + 1} / {Math.max(1, Math.ceil(shown.length / 50))}
            </span>
            <button
              className="secondary"
              disabled={(page + 1) * 50 >= shown.length}
              onClick={() => setPage(page + 1)}
            >
              下一页
            </button>
          </div>
        </section>
      )}
    </>
  );
}
