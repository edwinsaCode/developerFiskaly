import { getTokenAndRole } from "@/lib/auth";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { RecurringJournalView } from "@/components/accounting/RecurringJournalView";

export const metadata = { title: "Jurnal Berulang — NATA ALAM RAYA" };

export default async function RecurringJournalPage() {
  const { token } = await getTokenAndRole();
  return (
    <div className="space-y-4">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Jurnal Berulang</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          Template beban rutin bulanan — diposting otomatis oleh accounting engine.
        </p>
      </div>
      <AccountingNav />
      <RecurringJournalView token={token} />
    </div>
  );
}
