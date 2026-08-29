import Link from "next/link";

import { getTokenAndRole } from "@/lib/auth";
import { fetchARAging } from "@/lib/api/reports";
import { CollectionView } from "@/components/accounting/CollectionView";
import { ExportCsvButton } from "@/components/laporan/ExportCsvButton";
import { ErrorState } from "@/components/ui/ErrorState";
import type { ARAgingReport } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Penagihan — NATA ALAM RAYA" };

const today = () => todayLocalStr();

interface PageProps {
  searchParams: Promise<Record<string, string | undefined>>;
}

export default async function CollectionPage({ searchParams }: PageProps) {
  const sp = await searchParams;
  const asOf = sp.as_of ?? today();

  const { token } = await getTokenAndRole();

  let report: ARAgingReport | null = null;
  try {
    report = await fetchARAging(token, asOf);
  } catch {
    report = null;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Penagihan (Collection)</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Daftar kerja penagihan: tagihan jatuh tempo & menunggak yang harus dikejar tim finance.
          </p>
        </div>
        {report && report.rows.length > 0 && (
          <ExportCsvButton report="ar-aging" params={{ as_of: asOf }} label="Export CSV" />
        )}
      </div>

      {/* W-8/BD-1: daftar ini hanya memuat piutang yang sudah diakui (pasca-BAST
          + biaya realisasi ber-invoice). Tanpa penunjuk ini, cicilan pra-BAST
          menghilang dari pandangan tim penagihan tanpa jejak. */}
      <div className="rounded-lg border border-border bg-border-subtle/40 px-4 py-2.5 text-sm text-text-secondary">
        Cicilan pada kontrak yang <strong>belum BAST</strong> tidak muncul di sini — piutang harga
        rumah baru diakui saat serah terima. Daftar tagihannya ada di{" "}
        <Link href="/accounting/billing-schedule" className="text-accent hover:underline">
          Jadwal Penagihan
        </Link>
        .
      </div>

      {report === null ? (
        <ErrorState
          title="Gagal memuat data penagihan"
          description="Data tidak dapat diambil dari server. Periksa koneksi lalu coba lagi."
        />
      ) : (
        <CollectionView report={report} asOf={asOf} token={token} />
      )}
    </div>
  );
}
