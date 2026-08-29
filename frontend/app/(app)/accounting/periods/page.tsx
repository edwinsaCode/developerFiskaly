import { cookies } from "next/headers";
import { PeriodManager } from "@/components/accounting/PeriodManager";
import { YearEndClosing } from "@/components/accounting/YearEndClosing";
import { AccountingNav } from "@/components/accounting/AccountingNav";

export const metadata = { title: "Periode Akuntansi — NATA ALAM RAYA" };

export default async function PeriodsPage() {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value ?? "";
  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Periode Akuntansi</h1>
        <p className="text-sm text-text-secondary mt-1">
          Kelola status buka/tutup periode bulanan, dan jalankan tutup buku tahunan di akhir tahun fiskal.
        </p>
      </div>
      <PeriodManager token={token} />
      <YearEndClosing token={token} />
    </div>
  );
}
