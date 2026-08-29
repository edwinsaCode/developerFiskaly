import { cookies } from "next/headers";
import Link from "next/link";
import { DashboardV2, type ProjectRabReport } from "@/components/dashboard/DashboardV2";
import { GlobalSearchButton } from "@/components/layout/GlobalSearchButton";
import { fetchDashboard, type DashboardReport } from "@/lib/api/reports";
import { fetchRABvsRealisasi } from "@/lib/api/budget";
import { fetchSalesPerformance, type SalesPerformanceReport } from "@/lib/api/crm";
import { fetchChargePortfolio, type ChargePortfolio } from "@/lib/api/charge";

const COOKIE_NAME = "esa_session";

// Executive Dashboard — payload komposit /reports/dashboard + RAB vs Realisasi
// per proyek (paralel; proyek tanpa RAB aktif dilewati tanpa error).
export default async function DashboardPage() {
  const cookieStore = await cookies();
  const token = cookieStore.get(COOKIE_NAME)?.value ?? "";

  let data: DashboardReport | null = null;
  try {
    data = await fetchDashboard(token);
  } catch {
    data = null;
  }

  let rabReports: ProjectRabReport[] = [];
  let salesPerf: SalesPerformanceReport | null = null;
  // Billing Batch 2: KPI Titipan Realisasi — TERPISAH dari piutang harga rumah
  // (dua billing berbeda; KPI lama tidak berubah arti). Best-effort.
  const chargePortfolio: ChargePortfolio | null = await fetchChargePortfolio(token).catch(() => null);
  // Backend mengirim `projects: null` (slice nil Go) untuk tenant tanpa proyek —
  // bukan array kosong. Dinormalkan di sini agar halaman tidak 500.
  const projects = data?.projects ?? [];
  if (projects.length > 0) {
    const [settled, perf] = await Promise.all([
      Promise.allSettled(projects.map((p) => fetchRABvsRealisasi(token, p.id))),
      fetchSalesPerformance(token).catch(() => null),
    ]);
    salesPerf = perf;
    rabReports = settled.flatMap((r, i) =>
      r.status === "fulfilled" && r.value
        ? [{ project: projects[i], report: r.value }]
        : [],
    );
  }

  const isFresh =
    data !== null &&
    projects.length === 0 &&
    data.kpi.cash === "0.0000" &&
    data.kpi.receivable === "0.0000";

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Dashboard</h1>
          <p className="text-sm text-text-secondary mt-1">
            Posisi keuangan, kinerja bulan berjalan &amp; pipeline proyek
          </p>
        </div>
        <GlobalSearchButton />
      </div>

      {isFresh && (
        <div className="bg-accent/5 border border-accent/20 rounded-lg p-5 flex items-start justify-between gap-4 flex-wrap">
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-text-primary">
              Selamat datang di NATA ALAM RAYA 👋
            </h2>
            <p className="text-sm text-text-secondary mt-1 max-w-xl">
              Belum ada data. Langkah awal: buat Proyek → tambahkan unit → susun RAB &amp;
              catat biaya, lalu mulai booking/penjualan. Dashboard akan otomatis terisi.
            </p>
          </div>
          <Link
            href="/proyek"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors shrink-0"
          >
            Mulai — Ke Proyek →
          </Link>
        </div>
      )}

      {data ? (
        <DashboardV2 data={data} rabReports={rabReports} salesPerf={salesPerf} chargePortfolio={chargePortfolio} />
      ) : (
        <div className="rounded-lg border border-danger bg-danger-bg p-4 text-sm">
          Gagal memuat data dashboard. Muat ulang halaman, atau periksa koneksi backend.
        </div>
      )}
    </div>
  );
}
