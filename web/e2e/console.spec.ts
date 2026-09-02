import { test, expect } from "@playwright/test";
const token = process.env.RELAY_TEST_ADMIN_TOKEN ?? "";
test.skip(
  !token,
  "RELAY_TEST_ADMIN_TOKEN must point to an isolated test instance",
);
test("import → review → enable → compose → publish → bind → revoke", async ({
  page,
  request,
}) => {
  const suffix = Date.now().toString(36);
  const sourceName = "测试直播 " + suffix;
  const setName = "测试家庭 " + suffix;
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "让每一条源， 稳稳抵达。" }),
  ).toBeVisible();
  await page.getByLabel("管理员令牌").fill(token);
  await page.getByRole("button", { name: "进入控制台" }).click();
  await expect(
    page.getByRole("heading", { name: "总览", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "添加源", exact: true }).click();
  await page.getByRole("button", { name: "配置正文", exact: true }).click();
  await page.getByLabel("源名称", { exact: true }).fill(sourceName);
  await page
    .getByLabel("配置正文", { exact: true })
    .fill(
      '#EXTM3U\n#EXTINF:-1 tvg-id="demo",示例频道\nhttps://media.example.com/channel.ts',
    );
  await page.getByRole("button", { name: "识别与预览" }).click();
  await expect(page.getByRole("heading", { name: "已识别 m3u" })).toBeVisible();
  await page.getByRole("button", { name: "导入待审核源" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: sourceName + " video.live" }).click();
  await page.getByRole("button", { name: "版本与差异", exact: true }).click();
  await page.getByRole("button", { name: "批准此版本" }).click();
  await expect(page.getByRole("button", { name: "批准此版本" })).toHaveCount(0);
  await page.getByRole("button", { name: "概览", exact: true }).click();
  await page.getByRole("button", { name: "启用源", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "停用源", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "关闭弹窗" }).click();
  await page.getByRole("button", { name: "源编排组", exact: true }).click();
  await page.getByRole("button", { name: "新建编排组" }).first().click();
  await page.getByLabel("编排组名称").fill(setName);
  await page
    .getByRole("checkbox", { name: sourceName + " m3u", exact: true })
    .check();
  await page.getByRole("button", { name: "保存编排组" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const card = page
    .locator(".set-card")
    .filter({ has: page.getByRole("heading", { name: setName, exact: true }) });
  await card.getByRole("button", { name: "编译并发布" }).click();
  await expect(page.getByRole("status")).toContainText("编译完成");
  await page.getByRole("button", { name: "发布与客户端", exact: true }).click();
  await page
    .getByRole("button", { name: "绑定客户端", exact: true })
    .first()
    .click();
  await page.getByLabel("客户端名称").fill("客厅电视 " + suffix);
  await page
    .getByRole("combobox", { name: "源编排组", exact: true })
    .selectOption({ label: setName });
  await page.getByRole("button", { name: "创建绑定", exact: true }).click();
  await expect(
    page.getByRole("img", { name: "客户端订阅地址二维码" }),
  ).toBeVisible();
  const subscription = await page
    .getByRole("textbox", { name: "订阅地址" })
    .inputValue();
  const published = await request.get(subscription);
  expect(published.status()).toBe(200);
  const bundle = await published.json();
  expect(bundle.schema).toBe("shadow.media.bundle/v1");
  expect(bundle.providers[0].name).toBe(sourceName);
  expect((await request.get(bundle.exports["iptv/live.m3u"])).status()).toBe(
    200,
  );
  const etag = published.headers()["etag"];
  await page.getByRole("button", { name: "已保存，完成绑定" }).click();
  const bindingCard = page.locator(".compact-card").filter({
    has: page.getByRole("heading", {
      name: "客厅电视 " + suffix,
      exact: true,
    }),
  });
  await bindingCard.getByRole("button", { name: "吊销", exact: true }).click();
  await expect(
    bindingCard.getByRole("button", { name: "吊销", exact: true }),
  ).toBeDisabled();
  expect(
    (
      await request.get(subscription, { headers: { "If-None-Match": etag } })
    ).status(),
  ).toBe(404);
  await page.getByRole("button", { name: "总览", exact: true }).click();
  await page.screenshot({
    path: process.env.RELAY_SCREENSHOT_PATH ?? "test-results/overview.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({ path: "test-results/mobile.png", fullPage: true });
  expect(errors).toEqual([]);
});
test("login rejects invalid tokens", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("管理员令牌").fill("incorrect");
  await page.getByRole("button", { name: "进入控制台" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "authentication required",
  );
});
