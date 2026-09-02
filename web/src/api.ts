let credential = "";
export function setCredential(token: string) {
  credential = token;
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const response = await fetch("/api/v1/" + path, {
    method,
    headers: {
      Authorization: "Bearer " + credential,
      ...(method !== "GET" ? { "Content-Type": "application/json" } : {}),
    },
    ...(method !== "GET" ? { body: JSON.stringify(body ?? {}) } : {}),
  });
  const data = await response
    .json()
    .catch(() => ({ error: "服务未返回有效响应" }));
  if (!response.ok)
    throw new Error(data.error ?? `请求失败 (${response.status})`);
  return data as T;
}
export const labels: Record<string, string> = {
  healthy: "正常",
  invalid: "无效版本",
  degraded: "待完善",
  failing: "异常",
  quarantined: "已隔离",
  disabled: "已停用",
  unknown: "未体检",
  pending: "待审核",
  accepted: "已接纳",
  ignored: "已忽略",
  blocked: "已屏蔽",
  staged: "待批准",
  approved: "已批准",
  rejected: "已拒绝",
  queued: "排队中",
  running: "进行中",
  succeeded: "已完成",
  failed: "失败",
  primary: "主源",
  backup: "备用",
  auxiliary: "辅助",
  "catalog-only": "仅目录",
  compiled: "编译发布",
  "runtime-backed": "运行时",
  "direct-client": "直连客户端",
  auto: "自动更新",
  review: "更新需审核",
  manual: "仅手动",
  pinned: "固定版本",
  internet: "互联网",
  "trusted-lan": "受信内网",
  trusted: "受信",
  reviewed: "已审核",
  untrusted: "未信任",
  structure: "结构校验",
  functional: "功能抽样",
  service: "服务可达",
  "authenticated-state": "鉴权与状态",
};
export const label = (key: string) => labels[key] ?? key;
export function time(value?: string) {
  return value
    ? new Date(value).toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "尚未执行";
}
export const short = (id?: string) =>
  id ? id.split("_").at(-1)!.slice(0, 8) : "—";
export const defaultMember = (sourceId: string, priority = 100) => ({
  sourceId,
  priority,
  role: "primary",
  weight: 1,
  minScore: 50,
  mediaTypes: [],
  languages: [],
  regions: [],
  devices: [],
  networks: [],
  timeoutMs: 15000,
  maxConcurrency: 2,
});

export async function apiDownload(
  path: string,
  filename: string,
  method = "GET",
  body?: unknown,
) {
  const response = await fetch("/api/v1/" + path, {
    method,
    headers: {
      Authorization: "Bearer " + credential,
      ...(method !== "GET" ? { "Content-Type": "application/json" } : {}),
    },
    ...(method !== "GET" ? { body: JSON.stringify(body ?? {}) } : {}),
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: "下载失败" }));
    throw new Error(error.error ?? "下载失败");
  }
  const href = URL.createObjectURL(await response.blob());
  const a = document.createElement("a");
  a.href = href;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(href), 1000);
}
export function importDeepLink(
  format: string,
  url: string,
): string | undefined {
  const kind: Record<string, string> = {
    "legado/books.json": "bookSource",
    "legado/rss.json": "rsssource",
    "legado/tts.json": "httpTTS",
    "legado/replace.json": "replaceRule",
  };
  return kind[format]
    ? `yuedu://${kind[format]}/importonline?src=${encodeURIComponent(url)}`
    : undefined;
}
