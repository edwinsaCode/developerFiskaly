import { cookies } from "next/headers";
import { JournalList } from "@/components/accounting/JournalList";
import { AccountingNav } from "@/components/accounting/AccountingNav";

export const metadata = { title: "Jurnal — NATA ALAM RAYA" };

export default async function JurnalPage() {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value ?? "";
  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Jurnal</h1>
        <p className="text-sm text-text-secondary mt-1">
          Riwayat semua jurnal — manual, sistem, dan pembalik
        </p>
      </div>
      <JournalList token={token} />
    </div>
  );
}
