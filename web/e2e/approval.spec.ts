import { test, expect } from "@playwright/test";

test("restored pending sources can be reviewed and enabled without candidates", async ({
  page,
}) => {
  const source = {
    id: "src_imported",
    name: "备份直播",
    protocol: "m3u",
    mediaTypes: ["video.live"],
    capabilities: [],
    mode: "compiled",
    trust: "untrusted",
    network: "public",
    updatePolicy: "manual",
    intervalMinutes: 0,
    enabled: false,
    health: "unknown",
    score: 0,
    failures: 0,
    stagedRevision: "rev_imported",
    activeRevision: "",
    createdAt: "2026-01-01",
    updatedAt: "2026-01-01",
  };
  const revision = {
    id: "rev_imported",
    sourceId: source.id,
    status: "staged",
    normalized: {
      protocol: "m3u",
      items: [{ name: "新闻", url: "https://media.example.com/news.ts" }],
      warnings: [],
      capabilities: [],
      mediaTypes: ["video.live"],
    },
    diff: {
      added: 1,
      removed: 0,
      changed: 0,
      domainChanges: [],
      requiresReview: false,
    },
  };
  const writes: { path: string; body: unknown }[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.split("/api/v1/")[1];
    if (route.request().method() === "POST") {
      writes.push({ path, body: route.request().postDataJSON() });
      if (path === "sources/src_imported/approve-enable") {
        source.activeRevision = source.stagedRevision;
        source.stagedRevision = "";
        source.enabled = true;
      }
      await route.fulfill({ json: { ok: true } });
    } else {
      await route.fulfill({
        json:
          path === "sources"
            ? [source]
            : path.endsWith("/revisions")
              ? [revision]
              : path === "adapters"
                ? { adapters: [], connectors: {}, formats: [] }
                : [],
      });
    }
  });
  await page.goto("/");
  await page.getByLabel("管理员令牌").fill("fixture-token");
  await page.getByRole("button", { name: "进入控制台" }).click();
  await page.getByRole("button", { name: "源库", exact: true }).click();
  await page.getByRole("button", { name: "查看待审核源" }).click();
  await page.getByLabel("选择全部源", { exact: true }).check();
  await page.getByRole("button", { name: "批准并启用", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("备份直播");
  await page.getByText("查看版本内容", { exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("新闻");
  expect(writes).toEqual([]);
  await page.getByRole("button", { name: "确认批准并启用 1 个源" }).click();
  await expect(page.getByRole("dialog")).toContainText("已启用 1 个源");
  expect(writes).toEqual([
    {
      path: "sources/src_imported/approve-enable",
      body: { revision: "rev_imported" },
    },
  ]);
  await page.getByRole("button", { name: "完成", exact: true }).click();
  await page.getByLabel("待审核", { exact: true }).uncheck();
  await expect(page.locator("tbody")).toContainText("已启用");
});
