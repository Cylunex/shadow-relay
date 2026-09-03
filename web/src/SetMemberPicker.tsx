import { useState, type ReactNode } from "react";
import { defaultMember, label } from "./api";
import type { Member, Source } from "./types";
import { Empty, Field } from "./ui";

export function SetMemberPicker({
  sources,
  members,
  change,
  children,
}: {
  sources: Source[];
  members: Member[];
  change: (members: Member[]) => void;
  children: (source: Source) => ReactNode;
}) {
  const [query, setQuery] = useState("");
  const [protocol, setProtocol] = useState("");
  const [media, setMedia] = useState("");
  const [status, setStatus] = useState("");
  const [membership, setMembership] = useState("");
  const [role, setRole] = useState("");
  const [priority, setPriority] = useState("");
  const [score, setScore] = useState("");
  const selected = new Set(members.map((m) => m.sourceId));
  const matches = sources.filter(
    (s) =>
      `${s.name} ${s.protocol} ${s.url ?? ""}`
        .toLowerCase()
        .includes(query.trim().toLowerCase()) &&
      (!protocol || s.protocol === protocol) &&
      (!media || s.mediaTypes.some((m) => m.startsWith(media))) &&
      (!membership ||
        (membership === "selected"
          ? selected.has(s.id)
          : !selected.has(s.id))) &&
      (!status ||
        (status === "enabled"
          ? s.enabled
          : status === "disabled"
            ? !s.enabled
            : status === "pending"
              ? !!s.stagedRevision
              : s.health === status)),
  );
  const visible = new Set(matches.map((s) => s.id));
  const selectedMatches = members.filter((m) => visible.has(m.sourceId));
  const additions = matches.filter((s) => !selected.has(s.id));
  const validNumber = (v: string, max: number) =>
    v === "" ||
    (Number.isInteger(Number(v)) && Number(v) >= 0 && Number(v) <= max);
  const valid = validNumber(priority, 10000) && validNumber(score, 100);
  const edited = role !== "" || priority !== "" || score !== "";
  function reset() {
    setQuery("");
    setProtocol("");
    setMedia("");
    setStatus("");
    setMembership("");
  }
  return (
    <section className="set-member-picker">
      <div className="toolbar">
        <input
          className="grow"
          aria-label="搜索编排成员"
          placeholder="搜索源名称、协议或地址…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <select
          aria-label="成员协议筛选"
          value={protocol}
          onChange={(e) => setProtocol(e.target.value)}
        >
          <option value="">全部协议</option>
          {[...new Set(sources.map((s) => s.protocol))].sort().map((p) => (
            <option key={p}>{p}</option>
          ))}
        </select>
        <select
          aria-label="成员媒体筛选"
          value={media}
          onChange={(e) => setMedia(e.target.value)}
        >
          <option value="">全部媒体</option>
          {[
            ["video.", "视频与直播"],
            ["text.", "阅读"],
            ["image.", "漫画"],
            ["audio.", "音频"],
            ["speech.", "语音"],
            ["support.", "辅助"],
          ].map(([v, n]) => (
            <option value={v} key={v}>
              {n}
            </option>
          ))}
        </select>
        <select
          aria-label="成员状态筛选"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
        >
          <option value="">全部状态</option>
          <option value="enabled">已启用</option>
          <option value="disabled">未启用</option>
          <option value="pending">待审核</option>
          {["healthy", "degraded", "failing", "quarantined", "unknown"].map(
            (s) => (
              <option value={s} key={s}>
                {label(s)}
              </option>
            ),
          )}
        </select>
        <select
          aria-label="成员选择筛选"
          value={membership}
          onChange={(e) => setMembership(e.target.value)}
        >
          <option value="">全部源</option>
          <option value="selected">已加入</option>
          <option value="unselected">未加入</option>
        </select>
        <button className="text-button" onClick={reset}>
          清除筛选
        </button>
      </div>
      <p className="muted">
        匹配 {matches.length} 个源 · 当前已加入 {selectedMatches.length} 个 ·
        编排组共 {members.length} 个成员
      </p>
      <div className="selection-bar">
        <button
          disabled={
            !additions.length || members.length + additions.length > 500
          }
          onClick={() =>
            change([
              ...members,
              ...additions.map((s, i) =>
                defaultMember(
                  s.id,
                  Math.max(0, 100 - (members.length + i) * 10),
                ),
              ),
            ])
          }
        >
          加入筛选结果（{additions.length}）
        </button>
        <button
          disabled={!selectedMatches.length}
          onClick={() =>
            change(members.filter((m) => !visible.has(m.sourceId)))
          }
        >
          移除筛选内成员（{selectedMatches.length}）
        </button>
      </div>
      {members.length + additions.length > 500 && (
        <p className="warning">编排组最多支持 500 个成员，请缩小筛选范围。</p>
      )}
      <details className="member-batch-settings">
        <summary>批量设置筛选内成员（{selectedMatches.length}）</summary>
        <div className="form-grid">
          <Field label="批量角色">
            <select value={role} onChange={(e) => setRole(e.target.value)}>
              <option value="">保持不变</option>
              {["primary", "backup", "auxiliary"].map((r) => (
                <option value={r} key={r}>
                  {label(r)}
                </option>
              ))}
            </select>
          </Field>
          <Field label="批量优先级">
            <input
              type="number"
              min={0}
              max={10000}
              placeholder="保持不变"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
            />
          </Field>
          <Field label="批量最低健康分">
            <input
              type="number"
              min={0}
              max={100}
              placeholder="保持不变"
              value={score}
              onChange={(e) => setScore(e.target.value)}
            />
          </Field>
        </div>
        <button
          className="secondary"
          disabled={!selectedMatches.length || !edited || !valid}
          onClick={() =>
            change(
              members.map((m) =>
                visible.has(m.sourceId)
                  ? {
                      ...m,
                      ...(role ? { role } : {}),
                      ...(priority !== ""
                        ? { priority: Number(priority) }
                        : {}),
                      ...(score !== "" ? { minScore: Number(score) } : {}),
                    }
                  : m,
              ),
            )
          }
        >
          应用到筛选内 {selectedMatches.length} 个成员
        </button>
        <p className="muted">
          仅修改当前筛选内已加入的成员，留空的参数保持不变。点击“保存编排组”后生效。
        </p>
      </details>
      {matches.map(children)}
      {!matches.length && sources.length > 0 && (
        <Empty
          title="没有匹配的源"
          description="调整筛选条件；已加入的其他成员仍会保留。"
        />
      )}
    </section>
  );
}
