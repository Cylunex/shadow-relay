import { useState } from "react";
import { api } from "./api";
import { ErrorBox, Field } from "./ui";
import type { Source } from "./types";

export function LocalSourceEditor({
  source,
  saved,
}: {
  source: Source;
  saved: () => Promise<void>;
}) {
  const [content, setContent] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  if (source.url) return null;
  async function submit() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api(`sources/${source.id}/content`, "POST", {
        content,
        expectedUpdatedAt: source.updatedAt,
      });
      setContent("");
      await saved();
      setNotice("新版本已进入待审核，批准前继续使用原版本。");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <details className="workshop-card">
      <summary>更新本地规则 / 播客 / 播放列表</summary>
      <Field
        label="新版本完整内容"
        hint="协议保持不变；提交后需要单独批准，不影响当前订阅。"
      >
        <textarea
          className="code-input"
          rows={10}
          value={content}
          onChange={(e) => setContent(e.target.value)}
        />
      </Field>
      <button
        className="secondary"
        disabled={busy || !content}
        onClick={() => void submit()}
      >
        提交待审版本
      </button>
      <ErrorBox error={error} />
      {notice && <p>{notice}</p>}
    </details>
  );
}
