import { beforeEach, describe, expect, it } from "vitest";
import { SessionStore } from "./session";

describe("SessionStore", () => {
  beforeEach(() => localStorage.clear());

  it("starts empty when localStorage has no token", () => {
    const s = new SessionStore();
    expect(s.getToken()).toBeNull();
    expect(s.isAuthenticated()).toBe(false);
  });

  it("stores the token in memory and localStorage on set", () => {
    const s = new SessionStore();
    s.setToken("tok-123");
    expect(s.getToken()).toBe("tok-123");
    expect(s.isAuthenticated()).toBe(true);
    expect(localStorage.getItem("muesli_admin_token")).toBe("tok-123");
  });

  it("hydrates from localStorage on construction", () => {
    localStorage.setItem("muesli_admin_token", "persisted");
    const s = new SessionStore();
    expect(s.getToken()).toBe("persisted");
  });

  it("clears the token from memory and localStorage", () => {
    const s = new SessionStore();
    s.setToken("tok");
    s.clear();
    expect(s.getToken()).toBeNull();
    expect(localStorage.getItem("muesli_admin_token")).toBeNull();
  });

  it("authHeader returns a Bearer header when set, empty otherwise", () => {
    const s = new SessionStore();
    expect(s.authHeader()).toEqual({});
    s.setToken("tok");
    expect(s.authHeader()).toEqual({ Authorization: "Bearer tok" });
  });
});
