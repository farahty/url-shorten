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
