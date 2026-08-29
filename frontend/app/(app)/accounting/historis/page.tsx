import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { HistoricalSnapshotView } from "@/components/accounting/HistoricalSnapshotView";
import { fetchAccounts } from "@/lib/api/ledger";

export const metadata = { title: "Laporan Historis — NATA ALAM RAYA" };

async function getSession(): Promise<{ token: string; role: string } | null> {
  const store = await cookies();
  const token = store.get("esa_session")?.value;
  if (!token) return null;
  try {
    const [, payloadB64] = token.split(".");
    const json = Buffer.from(payloadB64, "base64url").toString("utf-8");
    const payload = JSON.parse(json);
    return { token, role: payload.role ?? "viewer" };
  } catch {
    return null;
  }
}

export default async function HistoricalFinancialsPage() {
  const session = await getSession();
  if (!session) redirect("/login");

  const { token, role } = session;

  // Snapshot memakai COA yang sudah ada — klien tidak perlu mengetik ulang
  // daftar akun tahun lalu. Akun nonaktif tetap ikut karena akun yang sudah
  // dipensiunkan sekarang bisa saja masih hidup di laporan 2023.
  const accounts = await fetchAccounts(token).catch(() => []);

  return (
    <div className="mx-auto w-full max-w-5xl">
      <AccountingNav />
      <div className="mb-6">
        <h1 className="display-lg text-2xl text-text-primary">Laporan Historis</h1>
        <p className="mt-1 text-sm text-text-secondary">
          Neraca dan Laba Rugi tahun-tahun sebelum sistem ini dipakai, dicatat sebagai
          arsip. Setiap tahun berdiri sendiri dan tidak menghasilkan jurnal apa pun.
        </p>
      </div>
      <HistoricalSnapshotView token={token} accounts={accounts} role={role} />
    </div>
  );
}
