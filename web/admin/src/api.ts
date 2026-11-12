// Thin client for /admin/api/v1. The session lives in an HttpOnly cookie;
// the readable uc_csrf cookie is echoed in a header on every write.

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

function csrfToken(): string {
  const hit = document.cookie.split("; ").find((c) => c.startsWith("uc_csrf="));
  return hit ? decodeURIComponent(hit.slice("uc_csrf=".length)) : "";
}

async function call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (method === "POST") {
    headers["Content-Type"] = "application/json";
    headers["X-CSRF-Token"] = csrfToken();
  }
  let res: Response;
  try {
    res = await fetch("/admin/api/v1" + path, {
      method,
      headers,
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "NETWORK", "нет связи с сервером");
  }
  let json: { data?: T; error?: { code?: string; message?: string } } = {};
  try {
    json = await res.json();
  } catch {
    // Non-JSON answer is reported by status below.
  }
  if (!res.ok || json.error) {
    throw new ApiError(res.status, json.error?.code ?? "HTTP_" + res.status, json.error?.message ?? "ошибка " + res.status);
  }
  return json.data as T;
}

export interface Me {
  id: string;
  email: string;
  display_name: string;
  roles: string[];
}
export interface Player {
  id: string;
  email: string;
  display_name: string;
  is_ai: boolean;
  blocked: boolean;
  roles: string[];
}
export interface Company {
  id: string;
  name: string;
  owner_kind: string;
  status: string;
  cash: string;
  reserved: string;
}
export interface Agent {
  id: string;
  name: string;
  profile: string;
  specialization: string;
  paused: boolean;
  strategy: unknown;
  loss_streak: number;
}
export interface ConfigItem {
  key: string;
  version: string;
  value: string;
  effective_at: string;
}
export interface AuditRow {
  id: string;
  action: string;
  reason: string;
  at: string;
  actor: string;
  before: unknown;
  after: unknown;
}

export const ROLES = [
  "player",
  "admin_read",
  "catalog_editor",
  "economy_admin",
  "moderator",
  "asset_editor",
  "test_operator",
  "superadmin",
] as const;

export const api = {
  login: (email: string, password: string) => call<{ mfa_token: string }>("POST", "/auth/login", { email, password }),
  totp: (mfa_token: string, code: string) => call<unknown>("POST", "/auth/totp", { mfa_token, code }),
  logout: () => call<unknown>("POST", "/auth/logout"),
  me: () => call<Me>("GET", "/me"),
  players: () => call<Player[] | null>("GET", "/players").then((x) => x ?? []),
  setRoles: (id: string, roles: string[], reason: string) => call<unknown>("POST", `/players/${id}/roles`, { roles, reason }),
  vip: (user_id: string, hours: string, reason: string) => call<unknown>("POST", "/vip", { user_id, hours, reason }),
  companies: () => call<Company[] | null>("GET", "/companies").then((x) => x ?? []),
  agents: () => call<Agent[] | null>("GET", "/agents").then((x) => x ?? []),
  pause: (id: string, paused: boolean, reason: string) => call<unknown>("POST", `/agents/${id}/pause`, { paused, reason }),
  config: () => call<ConfigItem[]>("GET", "/config"),
  setConfig: (key: string, value: string, expected_version: string) =>
    call<unknown>("POST", "/config", { key, value, expected_version }),
  audit: () => call<AuditRow[] | null>("GET", "/audit").then((x) => x ?? []),
};
