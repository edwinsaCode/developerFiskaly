import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { OpeningBalanceForm } from "@/components/accounting/OpeningBalanceForm";
import { fetchAccounts } from "@/lib/api/ledger";
import { AccountingNav } from "@/components/accounting/AccountingNav";

export const metadata = { title: "Saldo Awal — NATA ALAM RAYA" };

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

export default async function OpeningBalancePage() {
  const session = await getSession();
  if (!session) redirect("/login");

  const { token, role } = session;
  const readOnly = role === "viewer";

  const accounts = await fetchAccounts(token).catch(() => []);
  const activeAccounts = accounts.filter((a) => a.is_active);

  return (
    <div className="mx-auto w-full max-w-5xl">
      <AccountingNav />
      <div className="mb-6">
        <h1 className="display-lg text-2xl text-text-primary">Saldo Awal</h1>
        <p className="text-sm text-text-secondary mt-1">
          Input saldo awal akun-akun COA. Total Debit harus sama dengan Total Kredit sebelum bisa disimpan.
        </p>
      </div>
      <OpeningBalanceForm
        token={token}
        accounts={activeAccounts}
        readOnly={readOnly}
      />
    </div>
  );
}
