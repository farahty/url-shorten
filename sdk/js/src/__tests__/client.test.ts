import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UrlShorten } from "../client.js";

describe("UrlShorten request-id", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function okJson(body: unknown) {
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }

  it("generates an X-Request-ID when caller provides none", async () => {
    fetchMock.mockResolvedValueOnce(okJson({ code: "abc" }));
    const c = new UrlShorten({ baseUrl: "http://x", apiKey: "k" });
    await c.links.create({ url: "https://example.com" });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Request-ID"]).toMatch(/^[a-f0-9]{16}$/);
  });

  it("forwards caller-supplied request id verbatim when safe", async () => {
    fetchMock.mockResolvedValueOnce(okJson({ code: "abc" }));
    const c = new UrlShorten({ baseUrl: "http://x", apiKey: "k" });
    await c.links.create(
      { url: "https://example.com" },
      { requestId: "lead-42" },
    );

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Request-ID"]).toBe("lead-42");
  });

  it("replaces unsafe request ids", async () => {
    fetchMock.mockResolvedValueOnce(okJson({ code: "abc" }));
    const c = new UrlShorten({ baseUrl: "http://x", apiKey: "k" });
    await c.links.create(
      { url: "https://example.com" },
      { requestId: "bad id\n\r" },
    );

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["X-Request-ID"]).toMatch(/^[a-f0-9]{16}$/);
  });
});

describe("UrlShorten timeout", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("aborts the fetch after timeoutMs", async () => {
    fetchMock.mockImplementation((_url, init: RequestInit) => {
      return new Promise((_resolve, reject) => {
        init.signal?.addEventListener("abort", () => {
          const err = new Error("aborted");
          err.name = "AbortError";
          reject(err);
        });
      });
    });

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 20,
      maxRetries: 0,
    });

    await expect(c.links.create({ url: "https://e" })).rejects.toMatchObject({
      name: "TimeoutError",
    });
  });
});

describe("UrlShorten retry", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function resp(status: number, body: unknown = {}) {
    return new Response(JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    });
  }

  it("retries 5xx twice then succeeds", async () => {
    fetchMock
      .mockResolvedValueOnce(resp(502, { error: "bad gateway" }))
      .mockResolvedValueOnce(resp(503, { error: "unavailable" }))
      .mockResolvedValueOnce(resp(200, { code: "abc" }));

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    const out = await c.links.create({ url: "https://e" });
    expect(out).toMatchObject({ code: "abc" });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("does not retry on 4xx", async () => {
    fetchMock.mockResolvedValueOnce(resp(400, { error: "bad" }));
    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    await expect(c.links.create({ url: "https://e" })).rejects.toThrow();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("keeps the same X-Request-ID across retries", async () => {
    fetchMock
      .mockResolvedValueOnce(resp(503))
      .mockResolvedValueOnce(resp(200, { code: "abc" }));

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    await c.links.create({ url: "https://e" }, { requestId: "lead-7" });

    const first = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<
      string,
      string
    >;
    const second = (fetchMock.mock.calls[1][1] as RequestInit).headers as Record<
      string,
      string
    >;
    expect(first["X-Request-ID"]).toBe("lead-7");
    expect(second["X-Request-ID"]).toBe("lead-7");
  });

  it("retries a network error then succeeds", async () => {
    fetchMock
      .mockRejectedValueOnce(new TypeError("fetch failed: ECONNRESET"))
      .mockResolvedValueOnce(resp(200, { code: "abc" }));

    const c = new UrlShorten({
      baseUrl: "http://x",
      apiKey: "k",
      timeoutMs: 1000,
      maxRetries: 2,
    });
    const out = await c.links.create({ url: "https://e" });
    expect(out).toMatchObject({ code: "abc" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
