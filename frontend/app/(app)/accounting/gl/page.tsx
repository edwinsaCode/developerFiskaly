import { getTokenAndRole } from "@/lib/auth";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { GeneralLedgerView } from "@/components/accounting/GeneralLedgerView";

export const metadata = { title: "Buku Besar — NATA ALAM RAYA" };

interface PageProps {
  searchParams: Promise<{ account?: string }>;
}

export default async function GeneralLedgerPage({ searchParams }: PageProps) {
  const { token } = await getTokenAndRole();
  const { account } = await searchParams;
  const accountId = account ? parseInt(account, 10) : undefined;

  return (
    <div className="space-y-4">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Buku Besar</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          Mutasi per akun dengan saldo berjalan — klik baris untuk membuka jurnal sumber.
        </p>
      </div>
      <AccountingNav />
      <GeneralLedgerView token={token} initialAccountId={accountId && !isNaN(accountId) ? accountId : undefined} />
    </div>
  );
}
