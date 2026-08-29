import { getTokenAndRole } from "@/lib/auth";
import { fetchARAging } from "@/lib/api/reports";
import { fetchHouseARReconciliation, type HouseARReconciliation } from "@/lib/api/sale";
import { ARAgingView } from "@/components/accounting/ARAgingView";
import { HouseARReconPanel } from "@/components/accounting/HouseARReconPanel";
import { ExportCsvButton } from "@/components/laporan/ExportCsvButton";
import { ExportPdfButton } from "@/components/laporan/ExportPdfButton";
import { ErrorState } from "@/components/ui/ErrorState";
import type { ARAgingReport, ReceivableSource } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Piutang Customer — NATA ALAM RAYA" };

const today = () => todayLocalStr();

type SourceFilter = "all" | ReceivableSource;

// Sumber dari URL divalidasi di sini. Nilai asing tidak diteruskan ke server
// (yang menolaknya dengan 400) melainkan diperlakukan sebagai "semua" — link
// rusak tidak seharusnya membuat halaman piutang gagal dimuat.
function parseSource(raw: string | undefined): SourceFilter {
  return raw === "house" || raw === "realization" || raw === "legacy" ? raw : "all";
}

interface PageProps {
  searchParams: Promise<Record<string, string | undefined>>;
}

export default async function ReceivablePage({ searchParams }: PageProps) {
  const sp = await searchParams;
  const asOf = sp.as_of ?? today();
  const source = parseSource(sp.source);

  const { token } = await getTokenAndRole();

  // Bedakan "belum ada piutang" dari "gagal memuat" — akuntan tidak boleh
  // melihat daftar kosong palsu. Biarkan error tampil, jangan ditelan.
  let report: ARAgingReport | null = null;
  try {
    report = await fetchARAging(token, asOf, source);
  } catch {
    report = null;
  }

  // W-8: tie-out GL ↔ sub-ledger. Alat KONTROL, bukan syarat membaca laporan —
  // kegagalannya tidak boleh menjatuhkan halaman piutang. Tidak ditampilkan
  // pada filter yang tidak memuat harga rumah, karena tidak menjawab apa pun
  // di sana.
  const showRecon = source === "all" || source === "house";
  let recon: HouseARReconciliation | null = null;
  if (showRecon) {
    try {
      recon = await fetchHouseARReconciliation(token, asOf);
    } catch {
      recon = null;
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Piutang Customer</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            Seluruh yang ditagih ke customer dalam satu daftar: harga rumah (unit yang sudah
            BAST), biaya realisasi, dan piutang proyek lama. Kontrak yang belum diserahterimakan
            ada di{" "}
            <a href="/accounting/billing-schedule" className="text-accent hover:underline">
              Jadwal Penagihan
            </a>
            .
          </p>
        </div>
        {report && report.rows.length > 0 && (
          <div className="flex items-center gap-2">
            <ExportCsvButton
              report="ar-aging"
              params={source === "all" ? { as_of: asOf } : { as_of: asOf, source }}
              label="Export CSV"
            />
            <ExportPdfButton
              token={token}
              report="ar-aging"
              params={source === "all" ? { as_of: asOf } : { as_of: asOf, source }}
              label="Export PDF"
            />
          </div>
        )}
      </div>

      {showRecon && <HouseARReconPanel recon={recon} />}

      {report === null ? (
        <ErrorState
          title="Gagal memuat Piutang Customer"
          description="Data piutang tidak dapat diambil dari server. Periksa koneksi lalu coba lagi."
        />
      ) : (
        <ARAgingView report={report} asOf={asOf} source={source} />
      )}
    </div>
  );
}
