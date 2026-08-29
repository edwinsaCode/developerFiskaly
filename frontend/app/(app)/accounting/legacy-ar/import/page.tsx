import Link from "next/link";
import { getTokenAndRole } from "@/lib/auth";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { LegacyARImportWizard } from "@/components/accounting/LegacyARImportWizard";

export const metadata = { title: "Impor Piutang Proyek Lama — NATA ALAM RAYA" };

export default async function LegacyARImportPage() {
  const { token } = await getTokenAndRole();

  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <Link
          href="/accounting/legacy-ar"
          className="text-sm text-accent hover:underline"
        >
          ← Piutang Proyek Lama
        </Link>
        <h1 className="display-lg mt-1 text-2xl text-text-primary">
          Impor Piutang Proyek Lama
        </h1>
        <p className="mt-0.5 text-sm text-text-secondary">
          Mengisi rincian atas saldo piutang yang sudah ada di buku besar. Impor sendiri tidak
          menghasilkan jurnal — yang menghasilkan jurnal hanya pelunasannya nanti.
        </p>
      </div>

      <LegacyARImportWizard token={token} />
    </div>
  );
}
