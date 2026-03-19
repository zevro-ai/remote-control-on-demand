const BASE = "";

export function getToken(): string | null {
  return localStorage.getItem("rcod_token");
}

export function setToken(token: string) {
  localStorage.setItem("rcod_token", token);
}

export function clearToken() {
  localStorage.removeItem("rcod_token");
}

export function getAuthenticatedAssetURL(path?: string): string | undefined {
  if (!path) return undefined;
  const token = getToken();
  if (!token) return path;
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}access_token=${encodeURIComponent(token)}`;
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  const token = getToken();
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }

  if (res.status === 204) return undefined as T;
  return res.json();
}

async function requestForm<T>(path: string, body: FormData): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}${path}`, {
    method: "POST",
    headers,
    body,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }

  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
  postForm: <T>(path: string, body: FormData) => requestForm<T>(path, body),
  del: (path: string) => request<void>("DELETE", path),
};
