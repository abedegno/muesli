import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { ApiError } from "./types";
import { SessionStore } from "../auth/session";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("ApiClient", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  let session: SessionStore;
  let client: ApiClient;

  beforeEach(() => {
    localStorage.clear();
    fetchMock = vi.fn();
    session = new SessionStore();
    client = new ApiClient(session, "", fetchMock as unknown as typeof fetch);
  });

  it("getSetupStatus parses needs_setup", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { needs_setup: true }));
    const out = await client.getSetupStatus();
    expect(out.needs_setup).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith("/api/setup/status", expect.objectContaining({ method: "GET" }));
  });

  it("setup POSTs email+password and returns void on 201", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(201, { id: "u1", email: "o@x.com" }));
    await expect(client.setup("o@x.com", "password123")).resolves.toBeUndefined();
    const [, init] = fetchMock.mock.calls[0];
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ email: "o@x.com", password: "password123" });
  });

  it("setup throws ApiError(409) when an account already exists", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(409, { error: "account already exists" }));
    await expect(client.setup("o@x.com", "password123")).rejects.toMatchObject({
      name: "ApiError",
      status: 409,
    });
  });

  it("login stores the returned token in the session", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { token: "tok-xyz" }));
    const out = await client.login("o@x.com", "password123");
    expect(out.token).toBe("tok-xyz");
    expect(session.getToken()).toBe("tok-xyz");
  });

  it("login throws ApiError(401) on bad credentials and leaves session empty", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: "invalid credentials" }));
    await expect(client.login("o@x.com", "nope")).rejects.toBeInstanceOf(ApiError);
    expect(session.getToken()).toBeNull();
  });

  it("listPlugins sends the bearer token and returns the array", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, [
        { id: "p1", kind: "transcriber", name: "Whisper", endpoint_url: "http://w", enabled: true, is_default: true },
      ]),
    );
    const plugins = await client.listPlugins();
    expect(plugins).toHaveLength(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/plugins");
    expect(init.headers.Authorization).toBe("Bearer tok");
  });

  it("createPlugin POSTs the plugin input", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(201, { id: "p2" }));
    await client.createPlugin({
      kind: "agent",
      name: "Ollama",
      endpoint_url: "http://o",
      token: "shared-secret",
      config: { model: "llama3" },
      is_default: true,
    });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/plugins");
    expect(init.method).toBe("POST");
    // The write-only auth token is included in the create payload.
    expect(JSON.parse(init.body)).toMatchObject({
      kind: "agent",
      name: "Ollama",
      token: "shared-secret",
    });
  });

  it("updatePlugin omits an empty token from the PATCH (leaves it unchanged)", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { id: "p1" }));
    await client.updatePlugin("p1", { name: "Renamed", token: "" });
    const [, init] = fetchMock.mock.calls[0];
    const sent = JSON.parse(init.body);
    expect(sent).toEqual({ name: "Renamed" });
    expect("token" in sent).toBe(false);
  });

  it("updatePlugin includes a non-empty token to rotate it", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { id: "p1" }));
    await client.updatePlugin("p1", { token: "rotated" });
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ token: "rotated" });
  });

  it("updatePlugin PATCHes by id", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { id: "p1" }));
    await client.updatePlugin("p1", { enabled: false });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/plugins/p1");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body)).toEqual({ enabled: false });
  });

  it("deletePlugin DELETEs by id", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await client.deletePlugin("p1");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/plugins/p1");
    expect(init.method).toBe("DELETE");
  });

  it("checkPluginHealth POSTs to the health endpoint and returns the result", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { healthy: false, error: "boom" }));
    const result = await client.checkPluginHealth("p1");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/plugins/p1/health");
    expect(init.method).toBe("POST");
    expect(result).toEqual({ healthy: false, error: "boom" });
  });

  it("listJobs appends the status query param when given", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, []));
    await client.listJobs("failed");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/admin/jobs?status=failed");
  });

  it("listJobs omits the query param when status is empty", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, []));
    await client.listJobs("");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/admin/jobs");
  });

  it("cancelJob POSTs to the cancel endpoint", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { status: "cancelled" }));
    await client.cancelJob("j1");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/jobs/j1/cancel");
    expect(init.method).toBe("POST");
  });

  it("processNextJob POSTs to the process-next endpoint", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { status: "bumped", jobs_bumped: 1 }));
    await client.processNextJob("j1");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/jobs/j1/process-next");
    expect(init.method).toBe("POST");
  });

  it("throws ApiError(401) and clears the session on an expired token", async () => {
    session.setToken("stale");
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { error: "unauthorized" }));
    await expect(client.listPlugins()).rejects.toMatchObject({ status: 401 });
    expect(session.getToken()).toBeNull();
  });

  // ----- BAK01: in-app Postgres backup -----

  it("listBackups fetches the backups list", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(
      jsonResponse(200, [{ filename: "muesli-20240101120000.dump", size_bytes: 10, created_at: "2024-01-01T12:00:00Z" }]),
    );
    const backups = await client.listBackups();
    expect(backups).toHaveLength(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/backups");
    expect(init.method).toBe("GET");
    expect(init.headers.Authorization).toBe("Bearer tok");
  });

  it("createBackup POSTs and returns the new backup metadata", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(
      jsonResponse(201, { filename: "muesli-20240101120000.dump", size_bytes: 10, created_at: "2024-01-01T12:00:00Z" }),
    );
    const info = await client.createBackup();
    expect(info.filename).toBe("muesli-20240101120000.dump");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/backup");
    expect(init.method).toBe("POST");
  });

  it("createBackup throws ApiError(400) when backups are not configured", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(400, { error: "backups are not configured" }));
    await expect(client.createBackup()).rejects.toMatchObject({ status: 400 });
  });

  it("downloadBackup fetches with the bearer token and saves the blob via an anchor click", async () => {
    session.setToken("tok");
    const blob = new Blob(["dump bytes"], { type: "application/octet-stream" });
    fetchMock.mockResolvedValueOnce(new Response(blob, { status: 200 }));

    const createObjectURL = vi.fn().mockReturnValue("blob:fake-url");
    const revokeObjectURL = vi.fn();
    // jsdom doesn't implement these; stub them for the assertion.
    (URL as unknown as { createObjectURL: typeof createObjectURL }).createObjectURL = createObjectURL;
    (URL as unknown as { revokeObjectURL: typeof revokeObjectURL }).revokeObjectURL = revokeObjectURL;

    const clickSpy = vi.fn();
    const realCreateElement = document.createElement.bind(document);
    const createElementSpy = vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
      const el = realCreateElement(tag);
      if (tag === "a") el.click = clickSpy;
      return el;
    });

    await client.downloadBackup("muesli-20240101120000.dump");

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/admin/backups/muesli-20240101120000.dump");
    expect(init.headers.Authorization).toBe("Bearer tok");
    expect(createObjectURL).toHaveBeenCalled();
    expect(clickSpy).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:fake-url");

    createElementSpy.mockRestore();
  });

  it("downloadBackup throws ApiError(404) without touching the DOM on a missing file", async () => {
    session.setToken("tok");
    fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: "not found" }));
    await expect(client.downloadBackup("muesli-20240101120000.dump")).rejects.toMatchObject({ status: 404 });
  });
});
