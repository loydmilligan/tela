// Thin REST client over the Tela backend. Bearer-auth via TELA_API_KEY env;
// retries 5xx once with a small backoff. Errors are surfaced as TelaApiError
// so tool handlers can pass the {error, code} envelope back to the LLM
// verbatim — the `code` field is load-bearing for the agent's reaction.

export interface TelaErrorEnvelope {
  error: string;
  code: string;
}

export class TelaApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, envelope: TelaErrorEnvelope) {
    super(envelope.error);
    this.status = status;
    this.code = envelope.code;
    this.name = "TelaApiError";
  }
}

export interface TelaClientOptions {
  baseUrl: string;
  apiKey: string;
  fetchImpl?: typeof fetch;
  retryDelayMs?: number;
}

export class TelaClient {
  private readonly baseUrl: string;
  private readonly apiKey: string;
  private readonly fetchImpl: typeof fetch;
  private readonly retryDelayMs: number;

  constructor(opts: TelaClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, "");
    this.apiKey = opts.apiKey;
    this.fetchImpl = opts.fetchImpl ?? fetch;
    this.retryDelayMs = opts.retryDelayMs ?? 250;
  }

  async getJSON<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
    const url = this.buildURL(path, params);
    return this.requestWithRetry<T>(url, { method: "GET" });
  }

  async postJSON<T>(path: string, body: unknown): Promise<T> {
    const url = this.buildURL(path);
    return this.requestWithRetry<T>(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  async patchJSON<T>(path: string, body: unknown): Promise<T> {
    const url = this.buildURL(path);
    return this.requestWithRetry<T>(url, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  // deleteVoid issues DELETE and returns nothing on 2xx — the backend uses 204
  // for hard-deletes (pages) and 204 for the soft-delete shape too. Non-2xx
  // still throws TelaApiError so callers see the {error, code} envelope.
  async deleteVoid(path: string): Promise<void> {
    const url = this.buildURL(path);
    await this.requestVoid(url, { method: "DELETE" });
  }

  // postMultipart sends a `multipart/form-data` POST. `form` MUST already be a
  // FormData carrying both the scalar fields (e.g. `parent_id`, `dry_run`) and
  // the file parts. The undici fetch sets the boundary header automatically;
  // we deliberately do NOT set Content-Type, that would clobber the boundary.
  async postMultipart<T>(path: string, form: FormData): Promise<T> {
    const url = this.buildURL(path);
    return this.requestWithRetry<T>(url, { method: "POST", body: form });
  }

  private buildURL(path: string, params?: Record<string, string | number | undefined>): string {
    const u = new URL(this.baseUrl + path);
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        if (v === undefined || v === null) continue;
        u.searchParams.set(k, String(v));
      }
    }
    return u.toString();
  }

  private async requestWithRetry<T>(url: string, init: RequestInit): Promise<T> {
    const headers = {
      Authorization: `Bearer ${this.apiKey}`,
      Accept: "application/json",
      ...(init.headers ?? {}),
    };
    let lastErr: unknown;
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const res = await this.fetchImpl(url, { ...init, headers });
        if (res.status >= 500 && attempt === 0) {
          lastErr = new Error(`HTTP ${res.status}`);
          await sleep(this.retryDelayMs);
          continue;
        }
        if (!res.ok) {
          const env = await safeParseEnvelope(res);
          throw new TelaApiError(res.status, env);
        }
        return (await res.json()) as T;
      } catch (err) {
        if (err instanceof TelaApiError) throw err;
        lastErr = err;
        if (attempt === 0) {
          await sleep(this.retryDelayMs);
          continue;
        }
      }
    }
    throw lastErr instanceof Error ? lastErr : new Error("request failed");
  }

  // requestVoid is identical to requestWithRetry but discards the response
  // body. Used for DELETE which returns 204 (empty) on success.
  private async requestVoid(url: string, init: RequestInit): Promise<void> {
    const headers = {
      Authorization: `Bearer ${this.apiKey}`,
      Accept: "application/json",
      ...(init.headers ?? {}),
    };
    let lastErr: unknown;
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const res = await this.fetchImpl(url, { ...init, headers });
        if (res.status >= 500 && attempt === 0) {
          lastErr = new Error(`HTTP ${res.status}`);
          await sleep(this.retryDelayMs);
          continue;
        }
        if (!res.ok) {
          const env = await safeParseEnvelope(res);
          throw new TelaApiError(res.status, env);
        }
        return;
      } catch (err) {
        if (err instanceof TelaApiError) throw err;
        lastErr = err;
        if (attempt === 0) {
          await sleep(this.retryDelayMs);
          continue;
        }
      }
    }
    throw lastErr instanceof Error ? lastErr : new Error("request failed");
  }
}

async function safeParseEnvelope(res: Response): Promise<TelaErrorEnvelope> {
  try {
    const j = (await res.json()) as Partial<TelaErrorEnvelope>;
    return {
      error: typeof j.error === "string" ? j.error : `HTTP ${res.status}`,
      code: typeof j.code === "string" ? j.code : "http_error",
    };
  } catch {
    return { error: `HTTP ${res.status}`, code: "http_error" };
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
