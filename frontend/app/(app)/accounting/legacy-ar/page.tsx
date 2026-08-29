import { getTokenAndRole } from "@/lib/auth";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { LegacyARView } from "@/components/accounting/LegacyARView";
import { ErrorState } from "@/components/ui/ErrorState";
import { fetchLegacyReceivables, fetchLegacyReconciliation } from "@/lib/api/legacyar";
import type { LegacyListResult, LegacyReconciliation } from "@/lib/api/legacyar";

export const metadata = { title: "Piutang Proyek Lama — NATA ALAM RAYA" };

interface PageProps {
  searchParams: Promise<Record<string, string | undefined>>;
}

export default async function LegacyARPage({ searchParams }: PageProps) {
  const sp = await searchParams;
  const { token } = await getTokenAndRole();

  // Daftar kosong karena memang belum ada data ≠ daftar kosong karena gagal
  // memuat. Yang kedua harus terlihat sebagai kegagalan; kalau tidak, admin
  // menyimpulkan piutang lamanya hilang.
  let list: LegacyListResult | null = null;
  let rec: LegacyReconciliation | null = null;
  try {
    [list, rec] = await Promise.all([
      fetchLegacyReceivables(token, {}),
      fetchLegacyReconciliation(token, sp.as_of),
    ]);
  } catch {
    list = null;
    rec = null;
  }

  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Piutang Proyek Lama</h1>
        <p className="mt-0.5 text-sm text-text-secondary">
          Sisa tagihan customer dari proyek yang berjalan sebelum sistem ini dipakai. Impor di sini
          adalah pengisian <strong>rincian</strong> atas saldo piutang yang sudah ada di buku besar
          — bukan pencatatan piutang baru, karena itu tidak menghasilkan jurnal.
        </p>
      </div>

      {list === null || rec === null ? (
        <ErrorState
          title="Gagal memuat Piutang Proyek Lama"
          description="Data tidak dapat diambil dari server. Periksa koneksi lalu coba lagi."
        />
      ) : (
        <LegacyARView token={token} initial={list} initialRec={rec} />
      )}
    </div>
  );
}
