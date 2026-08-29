const INTERNAL_API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// Server: hit backend langsung; Browser: same-origin "/health" (di-proxy Next rewrite).
const healthUrl = () => (typeof window === "undefined" ? `${INTERNAL_API}/health` : "/health");

export type HealthResponse = {
  status: "ok" | "degraded";
  db: "ok" | "error";
  service: string;
};

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await fetch(healthUrl(), {
    cache: "no-store",
    signal: AbortSignal.timeout(5000),
  });
  if (!res.ok) {
    throw new Error(`health check returned ${res.status}`);
  }
  return res.json();
}
