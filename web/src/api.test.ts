import { afterEach, describe, expect, it, vi } from "vitest";
import { api, apiUpload, setCredential } from "./api";
afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});
describe("API boundary", () => {
  it("uploads multipart data with the deployment prefix and browser-generated boundary", async () => {
    vi.stubEnv("BASE_URL", "/relay/");
    const fetcher = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => ({ applied: false }) });
    vi.stubGlobal("fetch", fetcher);
    const form = new FormData();
    form.set("mode", "preview");
    setCredential("test-upload-token");
    await apiUpload("data/import", form);
    const [url, options] = fetcher.mock.calls[0];
    expect(url).toBe("/relay/api/v1/data/import");
    expect(options.body).toBe(form);
    expect(options.headers).toEqual({
      Authorization: "Bearer test-upload-token",
    });
  });
  it("uses the configured deployment prefix for API requests", async () => {
    vi.stubEnv("BASE_URL", "/relay/");
    const fetcher = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => [] });
    vi.stubGlobal("fetch", fetcher);
    await api("sources");
    expect(fetcher.mock.calls[0][0]).toBe("/relay/api/v1/sources");
  });
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
