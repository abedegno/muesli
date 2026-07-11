const STORAGE_KEY = "muesli_admin_token";

/**
 * SessionStore holds the admin session token in memory and mirrors it to
 * localStorage so a page reload stays logged in. The token is sent as
 * `Authorization: Bearer <token>` on every authenticated request.
 */
export class SessionStore {
  private token: string | null;

  constructor() {
    this.token = localStorage.getItem(STORAGE_KEY);
  }

  getToken(): string | null {
    return this.token;
  }

  isAuthenticated(): boolean {
    return this.token !== null && this.token !== "";
  }

  setToken(token: string): void {
    this.token = token;
    localStorage.setItem(STORAGE_KEY, token);
  }

  clear(): void {
    this.token = null;
    localStorage.removeItem(STORAGE_KEY);
  }

  authHeader(): Record<string, string> {
    return this.token ? { Authorization: `Bearer ${this.token}` } : {};
  }
}

// A shared singleton for app use; tests construct their own instances.
export const session = new SessionStore();
