import { afterEach, describe, expect, it, vi } from "vitest";
import { api, setCredential } from "./api";
afterEach(() => vi.unstubAllGlobals());
describe("API boundary", () => {
  it("sends bearer credentials only in headers and serializes empty mutations", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => ({ ok: true }) });
    vi.stubGlobal("fetch", fetcher);
    setCredential("test-ephemeral-token");
    await api("sources/a/enable", "POST");
    const [url, options] = fetcher.mock.calls[0];
    expect(url).toBe("/api/v1/sources/a/enable");
    expect(options.headers.Authorization).toBe("Bearer test-ephemeral-token");
    expect(options.body).toBe("{}");
  });
  it("propagates API failures instead of reporting success", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 409,
        json: async () => ({ error: "configuration changed" }),
      }),
    );
    await expect(api("source-sets/a/publish", "POST")).rejects.toThrow(
      "configuration changed",
    );
  });
});
