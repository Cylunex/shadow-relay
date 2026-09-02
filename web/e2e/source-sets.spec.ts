import { test, expect, type Page } from "@playwright/test";

const member = (sourceId: string) => ({
  sourceId,
  priority: 80,
  role: "primary",
  weight: 2,
  minScore: 60,
  mediaTypes: [],
  languages: [],
  regions: [],
  devices: [],
  networks: ["internet"],
  timeoutMs: 12000,
  maxConcurrency: 3,
});
function source(id: string, name: string, protocol: string, enabled = true) {
  return {
    id,
    name,
    protocol,
    mediaTypes: [protocol === "m3u" ? "video.live" : "text.novel"],
    capabilities: [],
    mode: "compiled",
    enabled,
    activeRevision: enabled ? "rev_" + id : "",
    stagedRevision: enabled ? "" : "staged_" + id,
    health: enabled ? "healthy" : "unknown",
    score: 80,
    createdAt: "2026-01-01",
    updatedAt: "2026-01-01",
  };
}
async function login(page: Page) {
  await page.goto("/");
  await page.getByLabel("管理员令牌").fill("fixture-token");
  await page.getByRole("button", { name: "进入控制台" }).click();
  await page.getByRole("button", { name: "源编排组", exact: true }).click();
}

test("member filters and bulk edits preserve members outside the filter", async ({
  page,
}) => {
  const sources = [
    source("live_a", "直播 A", "m3u"),
    source("live_b", "直播 B", "m3u", false),
    source("book", "阅读源", "legado-book"),
  ];
  const original = member("book");
  const set = {
    id: "set_home",
    name: "家庭",
    members: [original],
    autoPublish: false,
    minAvailable: 1,
    maxExcludedPercent: 25,
    updatedAt: "initial",
  };
  const writes: any[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.split("/api/v1/")[1];
    if (route.request().method() === "PUT") {
      writes.push(route.request().postDataJSON());
      await route.fulfill({ json: writes.at(-1) });
    } else
      await route.fulfill({
        json:
          path === "sources"
            ? sources
            : path === "source-sets"
              ? [set]
              : path === "adapters"
                ? { adapters: [], connectors: {}, formats: [] }
                : [],
      });
  });
  await login(page);
  await page.getByRole("button", { name: "编辑编排", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("成员协议筛选").selectOption("m3u");
  await dialog.getByLabel("成员状态筛选").selectOption("enabled");
  await dialog.getByRole("button", { name: "加入筛选结果（1）" }).click();
  await dialog.getByLabel("成员状态筛选").selectOption("");
  await dialog.getByRole("button", { name: "加入筛选结果（1）" }).click();
  await expect(
    dialog.getByRole("button", { name: "加入筛选结果（0）" }),
  ).toBeDisabled();
  await dialog.getByText("批量设置筛选内成员（2）", { exact: true }).click();
  await dialog.getByLabel("批量角色").selectOption("backup");
  await dialog.getByLabel("批量优先级").fill("240");
  await dialog.getByLabel("批量最低健康分").fill("40");
  await dialog.getByRole("button", { name: "应用到筛选内 2 个成员" }).click();
  await dialog.getByLabel("搜索编排成员").fill("直播 B");
  await dialog.getByRole("button", { name: "移除筛选内成员（1）" }).click();
  await dialog.getByLabel("成员选择筛选").selectOption("selected");
  await expect(dialog.getByText("没有匹配的源", { exact: true })).toBeVisible();
  expect(writes).toEqual([]);
  await dialog.getByRole("button", { name: "清除筛选", exact: true }).click();
  await dialog.getByRole("button", { name: "保存编排组" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(writes).toHaveLength(1);
  expect(writes[0].members).toHaveLength(2);
  expect(writes[0].members.find((m: any) => m.sourceId === "book")).toEqual(
    original,
  );
  expect(
    writes[0].members.find((m: any) => m.sourceId === "live_a"),
  ).toMatchObject({ role: "backup", priority: 240, minScore: 40 });
});

test("filtered group batches report failures and retry only failed selections", async ({
  page,
}) => {
  const sets = ["a", "b", "c"].map((id, i) => ({
    id,
    name: i < 2 ? "直播 " + id : "阅读 c",
    members: [member(id)],
    currentPublication: i < 2 ? "pub_" + id : "",
    autoPublish: false,
    minAvailable: 1,
    maxExcludedPercent: 25,
    updatedAt: "initial_" + id,
  }));
  const writes: { path: string; body: any }[] = [];
  let failedOnce = false;
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.split("/api/v1/")[1];
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      writes.push({ path, body });
      if (path === "source-sets/b" && !failedOnce) {
        failedOnce = true;
        await route.fulfill({
          status: 409,
          json: { error: "编排组已变化，请重试" },
        });
      } else {
        Object.assign(
          sets.find((s) => path === "source-sets/" + s.id)!,
          body,
        );
        await route.fulfill({ json: body });
      }
    } else
      await route.fulfill({
        json:
          path === "source-sets"
            ? sets
            : path === "adapters"
              ? { adapters: [], connectors: {}, formats: [] }
              : [],
      });
  });
  await login(page);
  await page.getByLabel("搜索编排组").fill("直播");
  await page.getByLabel("编排组发布筛选").selectOption("published");
  await page.getByLabel("选择当前筛选的编排组").check();
  await page.getByRole("button", { name: "开启定时发布", exact: true }).click();
  await expect(page.locator(".batch-result")).toContainText(
    "成功 1 个，失败 1 个",
  );
  await expect(
    page.getByLabel("选择编排组 直播 a", { exact: true }),
  ).not.toBeChecked();
  await expect(
    page.getByLabel("选择编排组 直播 b", { exact: true }),
  ).toBeChecked();
  await page.getByRole("button", { name: "开启定时发布", exact: true }).click();
  await expect(page.locator(".batch-result")).toContainText(
    "成功 1 个，失败 0 个",
  );
  expect(writes.map((w) => w.path)).toEqual([
    "source-sets/a",
    "source-sets/b",
    "source-sets/b",
  ]);
  expect(writes[0].body).toMatchObject({
    members: [member("a")],
    autoPublish: true,
    updatedAt: "initial_a",
  });
  await page.getByLabel("编排组定时筛选").selectOption("manual");
  await expect(
    page.getByText("没有匹配的编排组", { exact: true }),
  ).toBeVisible();
});
