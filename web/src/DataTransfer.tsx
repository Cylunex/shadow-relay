import { useState } from "react";
import { ArrowDownToLine, Upload } from "lucide-react";
import { apiDownload, apiUpload } from "./api";
import { ErrorBox, Field, Modal } from "./ui";

type Summary = {
  counts: Record<string, number>;
  added: Record<string, number>;
  reused: Record<string, number>;
  snapshots: number;
  seedFiles: number;
  interruptedJobs: number;
  keyRequired: boolean;
  applied: boolean;
};
const names: Record<string, string> = {
  sources: "源",
  revisions: "版本",
  endpoints: "端点",
  catalogs: "目录",
  candidates: "候选",
  source_sets: "编排组",
  publications: "发布",
  bindings: "客户端绑定",
  runtimes: "运行时",
  secrets: "加密凭据",
  probes: "体检记录",
  feedback: "反馈",
  jobs: "任务",
  audits: "审计",
};

export function DataTransfer({
  close,
  refresh,
}: {
  close: () => void;
  refresh: () => Promise<void>;
}) {
  const [file, setFile] = useState<File>();
  const [key, setKey] = useState("");
  const [summary, setSummary] = useState<Summary>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  async function inspect(mode: "preview" | "apply") {
    if (!file) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const form = new FormData();
      form.set("file", file);
      form.set("mode", mode);
      if (key) form.set("sourceMasterKey", key);
      const result = await apiUpload<Summary>("data/import", form);
      setSummary(result);
      if (result.applied) {
        setKey("");
        setFile(undefined);
        setMessage("导入成功。数据已合并，快照和凭据已使用当前实例密钥保存。");
        await refresh();
      } else if (result.keyRequired) {
        setError(
          "无法解密备份。请填写正确的原实例主密钥，再重新预览；当前数据没有变化。",
        );
      }
    } catch (e) {
      setError((e as Error).message);
      setSummary(undefined);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="数据导入与导出"
      subtitle="迁移源、版本、快照、编排、客户端绑定和种子配方。"
      close={() => {
        if (!busy) close();
      }}
      wide
    >
      <div className="modal-body data-transfer">
        <section className="panel transfer-section">
          <h3>导出当前空间</h3>
          <p>
            下载完整数据包。加密快照和凭据仍使用当前实例主密钥；恢复到其他实例时需要单独提供该密钥，请妥善保管。
          </p>
          <button
            className="secondary"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              setError("");
              try {
                await apiDownload(
                  "data/export",
                  `shadow-relay-data-${new Date().toISOString().slice(0, 10)}.tar.gz`,
                );
              } catch (e) {
                setError((e as Error).message);
              } finally {
                setBusy(false);
              }
            }}
          >
            <ArrowDownToLine size={16} />
            导出数据包
          </button>
        </section>
        <section className="panel transfer-section">
          <h3>导入数据包</h3>
          <p>
            先预览，再导入。相同记录会跳过；发现同 ID
            内容冲突时整次取消。历史待执行任务转为失败，可在导入后手动重试。
          </p>
          <Field label="备份文件（.tar.gz，最大 64 MiB）">
            <input
              type="file"
              accept=".tar.gz,.tgz,application/gzip"
              disabled={busy}
              onChange={(e) => {
                setFile(e.target.files?.[0]);
                setSummary(undefined);
                setError("");
                setMessage("");
              }}
            />
          </Field>
          <Field
            label="原实例主密钥"
            hint="同一实例恢复可留空。此值只用于本次迁移，不会替换当前密钥。"
          >
            <input
              type="password"
              autoComplete="off"
              value={key}
              disabled={busy}
              onChange={(e) => {
                setKey(e.target.value);
                setSummary(undefined);
              }}
            />
          </Field>
          <button
            className="secondary"
            disabled={busy || !file || file.size > 64 * 1024 * 1024}
            onClick={() => inspect("preview")}
          >
            <Upload size={16} />
            {busy ? "正在处理…" : "预览导入"}
          </button>
          {file && file.size > 64 * 1024 * 1024 && (
            <ErrorBox error="文件超过 64 MiB，请拆分或使用离线备份流程。" />
          )}
          {summary && (
            <div>
              <p>
                {Object.entries(summary.counts)
                  .filter(([, n]) => n > 0)
                  .map(([name, n]) => `${names[name] ?? name} ${n}`)
                  .join(" · ") || "空数据包"}
              </p>
              <p>
                快照 {summary.snapshots} · 种子文件 {summary.seedFiles} ·
                中止历史任务 {summary.interruptedJobs}
              </p>
              {!summary.applied && !summary.keyRequired && (
                <button
                  className="primary"
                  disabled={busy}
                  onClick={() => inspect("apply")}
                >
                  确认导入
                </button>
              )}
            </div>
          )}
        </section>
        <ErrorBox error={error} />
        {message && <p role="status">{message}</p>}
      </div>
    </Modal>
  );
}
