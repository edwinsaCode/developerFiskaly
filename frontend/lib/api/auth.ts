// Auth API — dipanggil dari client components via Next.js route handlers.
// Token TIDAK pernah disentuh di sini; httpOnly cookie dikelola oleh route handler.

export async function loginViaProxy(
  email: string,
  password: string,
): Promise<{ user: { email: string; role: string } }> {
  const res = await fetch("/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });

  if (!res.ok) {
    let message = "Login gagal";
    try {
      const err = (await res.json()) as { error?: string };
      message = err.error ?? message;
    } catch {
      // ignore
    }
    throw new Error(message);
  }

  return res.json() as Promise<{ user: { email: string; role: string } }>;
}

export async function registerViaProxy(
  tenantName: string,
  email: string,
  password: string,
): Promise<{ user: { email: string; role: string } }> {
  const res = await fetch("/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ tenant_name: tenantName, email, password }),
  });

  if (!res.ok) {
    let message = "Registrasi gagal";
    try {
      const err = (await res.json()) as { error?: string };
      message = err.error ?? message;
    } catch {
      // ignore
    }
    throw new Error(message);
  }

  return res.json() as Promise<{ user: { email: string; role: string } }>;
}

export async function logoutViaProxy(): Promise<void> {
  await fetch("/api/auth/logout", { method: "POST" });
}
