import { useEffect, useState } from "react";
import { api, label } from "./api";
import type { Revision, Source } from "./types";
import { ErrorBox, Modal } from "./ui";

type Entry = { source: Source; revision?: Revision };

export function EnableSources({
  sources,
  close,
  updated,
}: {
  sources: Source[];
  close: () => void;
  updated: () => Promise<void>;
}) {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [completed, setCompleted] = useState(0);
  useEffect(() => {
    let cancelled = false;
    Promise.all(
      sources.map(async (source): Promise<Entry> => {
        if (source.activeRevision) return { source };
        const revisions = await api<Revision[]>(
          `sources/${source.id}/revisions`,
        );
        const revision = revisions.find(
          (r) => r.id === source.stagedRevision && r.status === "staged",
        );
        if (!revision)
          throw new Error(
            `${source.name}：没有可审核版本，请先打开源详情同步或导入配置。`,
          );
        return { source, revision };
      }),
    )
      .then((result) => {
        if (!cancelled) setEntries(result);
      })
      .catch((e) => {
        if (!cancelled) setError(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [sources]);

  async function confirm() {
    setBusy(true);
    setError("");
    const failed: Entry[] = [];
    const errors: string[] = [];
    let succeeded = 0;
    for (const entry of entries) {
      try {
        await api(
          `sources/${entry.source.id}/${entry.revision ? "approve-enable" : "enable"}`,
          "POST",
          entry.revision ? { revision: entry.revision.id } : undefined,
        );
        succeeded++;
      } catch (e) {
        failed.push(entry);
        errors.push(`${entry.source.name}：${(e as Error).message}`);
      }
    }
    setCompleted((n) => n + succeeded);
    setEntries(failed);
    try {
      await updated();
    } catch (e) {
      errors.push(`刷新失败：${(e as Error).message}`);
    }
    setError(errors.join("；"));
    setBusy(false);
  }

  const approvals = entries.filter((e) => e.revision).length;
  return (
    <Modal
      title="审核并启用源"
      subtitle="确认来源与版本内容后启用；已有批准版本的源直接启用。"
      close={() => {
        if (!busy) close();
      }}
      wide
    >
      <div className="modal-body">
        {loading && <p role="status">正在读取待审核版本…</p>}
        <ErrorBox error={error} />
        {completed > 0 && (
          <p className="success-note" role="status">
            已启用 {completed} 个源。
          </p>
        )}
        {entries.map(({ source, revision }) => (
          <div className="revision-card" key={source.id}>
            <h3>{source.name}</h3>
            <p>
              {source.protocol} · {label(source.mode)} ·{" "}
              {revision
                ? `${revision.normalized.items.length} 条 · 待审核`
                : "使用已批准版本"}
            </p>
            {revision && (
              <>
                <p>
                  新增 {revision.diff.added} · 删除 {revision.diff.removed} ·
                  变更 {revision.diff.changed}
                </p>
                {revision.diff.requiresReview && (
                  <p className="warning">
                    此版本存在大量删除或域名变化，请核对差异后确认。
                  </p>
                )}
                {revision.diff.domainChanges?.length > 0 && (
                  <p>新增域名：{revision.diff.domainChanges.join("、")}</p>
                )}
                {revision.normalized.warnings?.map((warning, i) => (
                  <p className="warning" key={i}>
                    {warning}
                  </p>
                ))}
                <RevisionContent revision={revision} />
              </>
            )}
          </div>
        ))}
        {!loading && entries.length > 0 && (
          <p>
            将批准 {approvals} 个待审核版本，并启用 {entries.length}{" "}
            个源。启用后按各源配置参与调度和编排。
          </p>
        )}
        <div className="button-row spacer">
          {entries.length > 0 && (
            <button
              className="primary"
              disabled={busy || loading}
              onClick={() => void confirm()}
            >
              {busy ? "正在处理…" : `确认批准并启用 ${entries.length} 个源`}
            </button>
          )}
          <button className="secondary" disabled={busy} onClick={close}>
            {completed > 0 ? "完成" : "取消"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function RevisionContent({ revision }: { revision: Revision }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <details onToggle={(e) => setExpanded(e.currentTarget.open)}>
      <summary>查看版本内容</summary>
      {expanded && <pre>{JSON.stringify(revision.normalized, null, 2)}</pre>}
    </details>
  );
}
