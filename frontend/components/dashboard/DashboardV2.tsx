import * as React from "react";
import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import type { DashboardReport, DashboardProject } from "@/lib/api/reports";
import type { RABvsRealisasiReport } from "@/lib/types/api";
import {
  CashFlowChart,
  SalesFunnelChart,
  RabVarianceChart,
  UnitHeatmap,
  MarginChart,
  ProgressCompareChart,
  TopPerformersChart,
  BudgetHealthCard,
  BurnRateCard,
} from "./charts";
import type { SalesPerformanceReport } from "@/lib/api/crm";

export interface ProjectRabReport {
  project: DashboardProject;
  report: RABvsRealisasiReport;
}

// PS-2 — Dashboard Owner V2: owner paham kondisi perusahaan ≤10 detik.
// Server component (data di-fetch di page); setiap angka bisa diklik (U4).

// ── KPI card ──────────────────────────────────────────────────────────────────

// rupiahShort — format rupiah untuk teks caption (bukan angka utama). Dipakai
// di tempat yang hanya menerima string, sehingga <Rupiah> tidak bisa dipasang.
// Tetap sekadar memformat: frontend tidak menghitung uang.
function rupiahShort(value: string): string {
  const n = parseFloat(String(value).replace(",", "."));
  if (isNaN(n)) return value;
  return `Rp\u00a0${new Intl.NumberFormat("id-ID", { maximumFractionDigits: 0 }).format(n)}`;
}

function KPICard({
  label, value, href, caption, isCount = false, accent,
}: {
  label: string;
  value: string | number;
  href: string;
  caption?: string;
  isCount?: boolean;
  accent?: "success" | "warning" | "danger";
}) {
  // Angka utama = ink tegas (look "mahal"/ledger). Warna HANYA utk keadaan yg
  // butuh perhatian (negatif/danger); positif tetap ink, bukan hijau berlebihan.
  const valueColor =
    accent === "danger" ? "text-danger" :
    accent === "warning" ? "text-warning" : "text-text-primary";
  return (
    <Link
      href={href}
      className="group block rounded-xl border border-border bg-surface p-5 shadow-sm hover:border-accent/40 hover:shadow-md transition-all"
    >
      <p className="text-xs font-medium text-text-secondary">{label}</p>
      <p className={`mt-2 text-2xl font-bold tabular-nums track-tight ${valueColor}`}>
        {isCount ? value : <Rupiah value={String(value)} colorSign={false} />}
      </p>
      {caption && <p className="mt-1 text-[11px] text-text-tertiary">{caption}</p>}
    </Link>
  );
}

// ── Perhatian (actionable) ────────────────────────────────────────────────────

function AttentionStrip({ kpi, kprStuck = 0, costAheadProjects = 0 }: {
  kpi: DashboardReport["kpi"];
  kprStuck?: number;
  costAheadProjects?: number;
}) {
  const items: { label: string; count: number; href: string }[] = [
    { label: "cicilan jatuh tempo", count: kpi.overdue_schedules, href: "/accounting/collection" },
    { label: "pembatalan menunggu", count: kpi.cancellation_awaiting, href: "/penjualan/pembatalan" },
    { label: "refund belum dibayar", count: kpi.refund_pending, href: "/penjualan/pembatalan?tab=refund" },
    { label: "komisi menunggu proses", count: kpi.commission_queue, href: "/penjualan/komisi" },
    { label: "proyek menunggu true-up", count: kpi.trueup_awaiting, href: "/proyek" },
    // R5 — sinyal baru:
    { label: "KPR macet >30 hari di satu tahap", count: kprStuck, href: "/penjualan/kpr" },
    { label: "proyek biaya mendahului fisik", count: costAheadProjects, href: "/proyek" },
  ].filter((i) => i.count > 0);

  if (items.length === 0) return null;
  return (
    <div className="rounded-lg border border-warning bg-warning-bg px-4 py-2.5 text-sm flex flex-wrap items-center gap-x-4 gap-y-1">
      <span className="font-semibold shrink-0">⚠ Perlu perhatian:</span>
      {items.map((i) => (
        <Link key={i.label} href={i.href} className="hover:underline">
          <strong>{i.count}</strong> {i.label}
        </Link>
      ))}
    </div>
  );
}

// ── Funnel bar mini per proyek ────────────────────────────────────────────────

function UnitFunnel({ p }: { p: DashboardProject }) {
  const segs = [
    { n: p.units_available, cls: "bg-chart-2", label: "Tersedia" },
    { n: p.units_booked, cls: "bg-chart-4", label: "Dibooking" },
    { n: p.units_reserved, cls: "bg-chart-3", label: "Dipesan" },
    { n: p.units_ppjb, cls: "bg-chart-5", label: "PPJB" },
    { n: p.units_sold, cls: "bg-chart-1", label: "Terjual" },
    { n: p.units_other, cls: "bg-chart-6", label: "Lainnya" },
  ].filter((s) => s.n > 0);
  if (p.units_total === 0) {
    return <span className="text-xs text-text-tertiary">belum ada unit</span>;
  }
  return (
    <div>
      <div className="flex h-2 w-full overflow-hidden rounded-full bg-border-subtle">
        {segs.map((s) => (
          <div
            key={s.label}
            className={s.cls}
            style={{ width: `${(s.n / p.units_total) * 100}%` }}
            title={`${s.label}: ${s.n}`}
          />
        ))}
      </div>
      <p className="mt-1 text-[11px] text-text-secondary tabular-nums">
        {p.units_available} tersedia · {p.units_booked} booking · {p.units_reserved + p.units_ppjb} pesan/PPJB · {p.units_sold} terjual
      </p>
    </div>
  );
}

function Pct({ v }: { v: string }) {
  const n = parseFloat(v);
  // Clamp 0–100: aktual bisa negatif sesaat (persediaan dikredit saat BAST
  // sebelum realisasi biaya dicatat) — jangan tampilkan bar/angka negatif.
  const capped = isNaN(n) ? 0 : Math.min(Math.max(n, 0), 100);
  return (
    <div className="min-w-[90px]">
      <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-border-subtle">
        <div className="bg-accent" style={{ width: `${capped}%` }} />
      </div>
      <p className="mt-0.5 text-[11px] text-text-secondary tabular-nums">{v}%</p>
    </div>
  );
}

// ── Laporan quick links ───────────────────────────────────────────────────────

const REPORT_LINKS = [
  { label: "Neraca", href: "/laporan?tab=neraca" },
  { label: "Laba Rugi", href: "/laporan?tab=pl" },
  { label: "Arus Kas", href: "/laporan?tab=aruskas" },
  { label: "Neraca Saldo", href: "/laporan?tab=trial" },
  { label: "Piutang (Aging)", href: "/accounting/receivable" },
  { label: "Pajak", href: "/pajak" },
  { label: "Pipeline Unit", href: "/laporan?tab=pipeline" },
];

// ── Seksi pertanyaan bisnis ───────────────────────────────────────────────────
// Dashboard menjawab pertanyaan, bukan memajang data: tiap seksi diberi judul
// berupa pertanyaan owner + hint dari mana angkanya berasal (ledger-centric).

function Section({
  question, hint, children,
}: {
  question: string;
  hint: string;
  children: React.ReactNode;
}) {
  return (
    <section>
      <div className="mb-3">
        <h2 className="text-base font-bold text-text-primary track-tight">{question}</h2>
        <p className="text-xs text-text-tertiary mt-0.5">{hint}</p>
      </div>
      <SectionGrid>{children}</SectionGrid>
    </section>
  );
}

// Deretan KPI. Di mobile hanya muat 2 kolom; kalau jumlah kartunya ganjil,
// kartu terakhir melebar menutup baris supaya tidak ada sel kosong di kanan.
function KpiStrip({ children }: { children: React.ReactNode }) {
  const odd = React.Children.toArray(children).length % 2 === 1;
  return (
    <div
      className={`grid grid-cols-2 md:grid-cols-3 xl:grid-cols-5 gap-3 ${
        odd ? "[&>*:last-child]:col-span-2 md:[&>*:last-child]:col-span-1" : ""
      }`}
    >
      {children}
    </div>
  );
}

// Grid isi seksi — kolomnya MENGIKUTI jumlah kartu yang benar-benar dirender
// (banyak kartu bersifat kondisional). Satu kartu → lebar penuh, bukan setengah
// dengan ruang menganga di kanan. Jumlah ganjil → kartu terakhir melebar
// menutup baris, sehingga tidak ada sel kosong yang tersisa.
function SectionGrid({ children }: { children: React.ReactNode }) {
  const items = React.Children.toArray(children);
  if (items.length <= 1) return <div className="grid grid-cols-1 gap-4">{children}</div>;
  const odd = items.length % 2 === 1;
  return (
    <div
      className={`grid grid-cols-1 lg:grid-cols-2 gap-4 ${
        odd ? "lg:[&>*:last-child]:col-span-2" : ""
      }`}
    >
      {children}
    </div>
  );
}

// R5 — forecast kas masuk 30/60/90 hari dari jadwal belum lunas.
function ForecastCard({ r }: { r: NonNullable<DashboardReport["receivables"]> }) {
  const rows = [
    { label: "≤ 30 hari", v: r.forecast_30 },
    { label: "≤ 60 hari", v: r.forecast_60 },
    { label: "≤ 90 hari", v: r.forecast_90 },
  ];
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <p className="eyebrow mb-3">
        Forecast Kas Masuk (jadwal jatuh tempo)
      </p>
      {/* Nominal rupiah bisa panjang — di layar sempit ditumpuk agar tidak
          terpotong di dalam sel selebar sepertiga layar. */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-center">
        {rows.map((x) => (
          <div key={x.label} className="rounded-md border border-border p-2.5">
            <p className="text-[11px] text-text-secondary">{x.label}</p>
            <p className="mt-1 text-sm font-semibold tabular-nums">
              <Rupiah value={x.v} colorSign={false} />
            </p>
          </div>
        ))}
      </div>
      <p className="mt-2 text-[11px] text-text-tertiary">
        Kumulatif dari cicilan belum lunas — angka nol berarti belum ada jadwal ditagihkan.
      </p>
    </div>
  );
}

// R5 — top customer menunggak (dari jadwal overdue, urut nominal).
function TopDebtorsCard({ debtors }: { debtors: NonNullable<DashboardReport["receivables"]>["top_debtors"] }) {
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <p className="eyebrow mb-2">
        Top Penunggak
      </p>
      <ul className="divide-y divide-border text-sm">
        {debtors.map((d) => (
          <li key={d.contract_id} className="flex items-center justify-between py-2">
            <span>
              <Link href={`/accounting/receivable/${d.contract_id}`} className="text-accent hover:underline">
                {d.buyer_name}
              </Link>
              <span className="text-xs text-text-tertiary ml-1.5">{d.unit_code} · {d.overdue_count} cicilan</span>
            </span>
            <span className="font-semibold tabular-nums text-danger">
              <Rupiah value={d.overdue_total} colorSign={false} />
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// R5 — pipeline KPR ringkas (count per tahap).
function KPRSectionCard({ kpr }: { kpr: NonNullable<DashboardReport["kpr"]> }) {
  const stages = [
    { label: "Belum Diajukan", n: kpr.count_preparation },
    { label: "Pengajuan", n: kpr.count_submitted },
    { label: "SP3K", n: kpr.count_approved },
    { label: "Akad", n: kpr.count_akad },
    { label: "Dana Cair", n: kpr.count_disbursed },
    { label: "Lunas", n: kpr.count_settled },
  ];
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <p className="eyebrow mb-3">
        Pipeline KPR per Tahap
      </p>
      <div className="grid grid-cols-3 sm:grid-cols-6 gap-2 text-center">
        {stages.map((s) => (
          <Link key={s.label} href="/penjualan/kpr"
            className="rounded-md border border-border p-2 hover:border-accent transition-colors">
            <p className="text-lg font-semibold tabular-nums">{s.n}</p>
            <p className="text-[10px] text-text-secondary leading-tight">{s.label}</p>
          </Link>
        ))}
      </div>
    </div>
  );
}

// R5 — profit per unit BAST.
function UnitProfitTable({ rows }: { rows: NonNullable<DashboardReport["unit_profits"]> }) {
  return (
    <div className="lg:col-span-2 overflow-x-auto rounded-lg border border-border bg-surface">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-xs text-text-secondary">
            <th className="py-2.5 pl-4 pr-3">Unit</th>
            <th className="py-2.5 pr-3">Proyek</th>
            <th className="py-2.5 pr-3 text-right">Pendapatan</th>
            <th className="py-2.5 pr-3 text-right">HPP</th>
            <th className="py-2.5 pr-3 text-right">Margin</th>
            <th className="py-2.5 pr-4 text-right">Margin %</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.unit_id} className="border-b border-border last:border-0 hover:bg-border-subtle/40">
              <td className="py-2.5 pl-4 pr-3">
                <Link href={`/penjualan/${r.unit_id}`} className="font-medium text-accent hover:underline">
                  {r.unit_code}
                </Link>
              </td>
              <td className="py-2.5 pr-3 text-text-secondary">{r.project}</td>
              <td className="py-2.5 pr-3 text-right tabular-nums"><Rupiah value={r.revenue} colorSign={false} /></td>
              <td className="py-2.5 pr-3 text-right tabular-nums"><Rupiah value={r.hpp} colorSign={false} /></td>
              <td className="py-2.5 pr-3 text-right tabular-nums">
                <span className={r.margin.startsWith("-") ? "text-danger" : "text-success"}>
                  <Rupiah value={r.margin} colorSign={false} />
                </span>
              </td>
              <td className="py-2.5 pr-4 text-right tabular-nums">{r.margin_pct}%</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// Ringkasan penagihan — melengkapi ProgressCompareChart dengan angka absolut
// dan CTA langsung ke pusat penagihan.
function CollectionCard({ kpi }: { kpi: DashboardReport["kpi"] }) {
  const rows = [
    { label: "Piutang berjalan", value: kpi.receivable, href: "/accounting/receivable" },
    { label: "Titipan booking (legacy — booking baru langsung jadi Pendapatan Booking)", value: kpi.booking_deposits, href: "/penjualan/booking" },
  ];
  return (
    <div className="rounded-lg border border-border bg-surface p-4 flex flex-col">
      <div className="mb-3 flex items-start justify-between gap-2">
        <div>
          <p className="text-sm font-semibold text-text-primary">Ringkasan Penagihan</p>
          <p className="text-[11px] text-text-tertiary mt-0.5">saldo dari ledger posted</p>
        </div>
        <Link href="/accounting/collection" className="text-xs text-accent hover:underline shrink-0">
          Pusat Penagihan →
        </Link>
      </div>
      <div className="space-y-2">
        {rows.map((r) => (
          <Link key={r.label} href={r.href}
            className="flex items-center justify-between rounded border border-border px-3 py-2.5 hover:border-accent transition-colors">
            <span className="text-xs text-text-secondary">{r.label}</span>
            <span className="text-sm font-semibold tabular-nums">
              <Rupiah value={r.value} colorSign={false} />
            </span>
          </Link>
        ))}
        <Link href="/accounting/collection"
          className={`flex items-center justify-between rounded border px-3 py-2.5 transition-colors
            ${kpi.overdue_schedules > 0
              ? "border-warning bg-warning-bg hover:border-danger"
              : "border-border hover:border-accent"}`}>
          <span className="text-xs text-text-secondary">Cicilan jatuh tempo / menunggak</span>
          <span className={`text-sm font-semibold tabular-nums ${kpi.overdue_schedules > 0 ? "text-warning" : ""}`}>
            {kpi.overdue_schedules} cicilan
          </span>
        </Link>
      </div>
    </div>
  );
}

// ── Main ──────────────────────────────────────────────────────────────────────

export function DashboardV2({
  data,
  rabReports = [],
  salesPerf = null,
  chargePortfolio = null,
}: {
  data: DashboardReport;
  rabReports?: ProjectRabReport[];
  salesPerf?: SalesPerformanceReport | null;
  /** Billing Batch 2: ringkasan Titipan Realisasi (billing terpisah dari harga rumah). */
  chargePortfolio?: { open_groups: number; outstanding: string; residual: string; unbilled?: string } | null;
}) {
  const k = data.kpi;
  const monthName = new Date(data.as_of).toLocaleDateString("id-ID", { month: "long", year: "numeric" });
  // `projects` bisa null dari backend (slice nil Go) — dinormalkan sekali di sini.
  const projects = data.projects ?? [];
  const costAheadProjects = projects.filter((p) => p.cost_ahead_warning).length;

  return (
    <div className="space-y-5">
      <AttentionStrip
        kpi={k}
        kprStuck={data.kpr?.stuck_count ?? 0}
        costAheadProjects={costAheadProjects}
      />

      {/* BARIS 1 — Posisi hari ini */}
      <div>
        <p className="mb-2 eyebrow">
          Posisi Hari Ini
        </p>
        <KpiStrip>
          <KPICard label="Kas & Bank" value={k.cash} href="/laporan?tab=trial" />
          <KPICard
            label="Outstanding Kontrak"
            value={data.receivables?.outstanding_total ?? k.receivable}
            href="/accounting/receivable"
            caption="sisa tagihan seluruh kontrak aktif"
          />
          <KPICard label="Hutang Usaha" value={k.payable} href="/laporan?tab=trial" />
          {chargePortfolio && chargePortfolio.open_groups > 0 && (
            <KPICard
              label="Tagihan Realisasi"
              value={chargePortfolio.outstanding}
              href="/accounting/receivable"
              caption={
                // W-5: sisa terutang realisasi bukan satu jenis angka. Bagian yang
                // belum ditagihkan belum menjadi piutang — menyebutnya di sini
                // adalah cara paling murah menemukan tagihan yang menganggur.
                chargePortfolio.unbilled && chargePortfolio.unbilled !== "0" && Number(chargePortfolio.unbilled) > 0
                  ? `${chargePortfolio.open_groups} grup berjalan — ${rupiahShort(chargePortfolio.unbilled)} belum ditagihkan (belum jadi piutang)`
                  : `${chargePortfolio.open_groups} grup berjalan — seluruhnya sudah ditagihkan`
              }
            />
          )}
          <KPICard label="Titipan Booking (legacy)" value={k.booking_deposits} href="/penjualan/booking" />
          <KPICard label="Komisi Belum Dibayar" value={k.commission_payable} href="/penjualan/komisi?status=payable" />
        </KpiStrip>
      </div>

      {/* BARIS 2 — Bulan berjalan */}
      <div>
        <p className="mb-2 eyebrow">
          {monthName}
        </p>
        <KpiStrip>
          <KPICard label="Penjualan Bulan Ini" value={k.sales_mtd} href="/laporan?tab=pl" accent="success"
            caption="pendapatan diakui — bukan kas diterima" />
          <KPICard
            label="Booking Aktif"
            value={k.booking_active}
            isCount
            caption={k.booking_active > 0 ? undefined : "belum ada"}
            href="/penjualan/booking?status=active"
          />
          <KPICard label="Unit Terjual (BAST)" value={k.units_sold_mtd} isCount href="/penjualan" />
          <KPICard label="Margin Bulan Ini" value={k.margin_mtd} href="/laporan?tab=pl"
            accent={k.margin_mtd.startsWith("-") ? "danger" : "success"} />
          <KPICard label="Arus Kas Bersih" value={k.net_cash_flow_mtd} href="/laporan?tab=aruskas"
            accent={k.net_cash_flow_mtd.startsWith("-") ? "danger" : "success"}
            caption="masuk − keluar bulan ini" />
        </KpiStrip>
      </div>

      {/* P3 (item A) — Cash vs KPR: pendapatan DIAKUI (bukan kas diterima),
          dipecah via payment_type kontrak. Total = Pendapatan Bulan Ini di
          atas; selisih (bila ada) berarti sebagian pendapatan belum/tidak
          ber-kontrak — bukan dipaksa ke salah satu sisi. */}
      <div>
        <p className="mb-2 eyebrow">
          Cash vs KPR — {monthName}
        </p>
        <KpiStrip>
          <KPICard
            label="Pendapatan Cash"
            value={k.pendapatan_cash_mtd}
            href="/laporan?tab=pl"
            caption={
              k.kontrak_value_cash !== "0"
                ? `nilai kontrak cash: ${rupiahShort(k.kontrak_value_cash)}`
                : undefined
            }
          />
          <KPICard
            label="Pendapatan KPR"
            value={k.pendapatan_kpr_mtd}
            href="/laporan?tab=pl"
            caption={
              k.kontrak_value_kpr !== "0"
                ? `nilai kontrak KPR: ${rupiahShort(k.kontrak_value_kpr)}`
                : undefined
            }
          />
          <KPICard
            label="Total Pendapatan"
            value={k.sales_mtd}
            href="/laporan?tab=pl"
            accent="success"
            caption="Total Pendapatan Bulan Ini (Cash + KPR)"
          />
        </KpiStrip>
      </div>

      {/* ── Executive Control Center: tiap seksi MENJAWAB satu pertanyaan bisnis ── */}

      {/* Q1 — Kas */}
      <Section
        question="Berapa posisi kas kita — dan berapa lama bertahan?"
        hint="arus kas 6 bulan + burn rate & runway, dari akun kas/bank posted"
      >
        <CashFlowChart series={data.cash_flow ?? []} />
        <BurnRateCard burn={data.burn ?? { avg_cash_in: "0", avg_cash_out: "0", monthly_burn: "0" }} />
      </Section>

      {/* Q2 — Biaya vs RAB */}
      <Section
        question="Apakah biaya proyek masih terkendali?"
        hint="realisasi vs RAB aktif per kategori — jurnal posted, bukan estimasi"
      >
        {rabReports.slice(0, 2).map(({ project, report }) => (
          <RabVarianceChart
            key={project.id}
            report={report}
            projectId={project.id}
            projectName={project.name}
          />
        ))}
        <BudgetHealthCard
          reports={rabReports.map(({ project, report }) => ({
            projectId: project.id,
            projectName: project.name,
            report,
          }))}
        />
      </Section>

      {/* Q3 — Collection */}
      <Section
        question="Bagaimana penagihan berjalan?"
        hint="kas diterima vs nilai kontrak + cicilan bermasalah"
      >
        <ProgressCompareChart projects={projects} />
        <CollectionCard kpi={k} />
        {data.receivables && <ForecastCard r={data.receivables} />}
        {data.receivables && data.receivables.top_debtors.length > 0 && (
          <TopDebtorsCard debtors={data.receivables.top_debtors} />
        )}
      </Section>

      {/* R5 — Q3b: KPR */}
      {data.kpr && (
        <Section
          question="Bagaimana posisi KPR?"
          hint="pipeline pencairan bank — dari scheme state + termin kpr_disbursement"
        >
          <KPRSectionCard kpr={data.kpr} />
          <div className="rounded-lg border border-border bg-surface p-4">
            <p className="eyebrow mb-3">
              Dana Bank
            </p>
            <div className="space-y-3 text-sm">
              <div className="flex items-center justify-between">
                <span className="text-text-secondary">Total dana cair diterima</span>
                <span className="font-semibold tabular-nums text-success">
                  <Rupiah value={data.kpr.total_disbursed} colorSign={false} />
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-text-secondary">Outstanding kontrak KPR</span>
                <span className={`font-semibold tabular-nums ${data.kpr.total_outstanding !== "0" ? "text-danger" : "text-success"}`}>
                  <Rupiah value={data.kpr.total_outstanding} colorSign={false} />
                </span>
              </div>
              {data.kpr.stuck_count > 0 && (
                <p className="rounded-md border border-danger/30 bg-danger-bg px-3 py-2 text-xs text-danger">
                  ⚠ {data.kpr.stuck_count} kontrak &gt;30 hari diam di satu tahap — kejar bank/customer.
                </p>
              )}
              <Link href="/penjualan/kpr" className="inline-block text-xs text-accent hover:underline">
                Buka papan KPR →
              </Link>
            </div>
          </div>
        </Section>
      )}

      {/* R5 — Q3c: profit per unit */}
      {(data.unit_profits?.length ?? 0) > 0 && (
        <Section
          question="Berapa profit per unit yang sudah BAST?"
          hint="pendapatan − HPP diakui per unit (sale records; margin desc, top 10)"
        >
          <UnitProfitTable rows={data.unit_profits!} />
        </Section>
      )}

      {/* Q4 — Penjualan & kesehatan proyek */}
      <Section
        question="Bagaimana kinerja penjualan & kesehatan proyek?"
        hint="funnel, sebaran unit, margin, dan sales terbaik"
      >
        <SalesFunnelChart funnel={data.funnel ?? {
          leads_active: 0, leads_converted: 0, booking_active: k.booking_active,
          contracts_active: 0, units_sold: 0,
        }} />
        <UnitHeatmap projects={projects} />
        <MarginChart projects={projects} />
        {salesPerf?.rows && salesPerf.rows.length > 0 && (
          <TopPerformersChart rows={salesPerf.rows} />
        )}
      </Section>

      {/* BARIS 4 — Pipeline proyek */}
      <div>
        <div className="mb-2 flex items-center justify-between">
          <p className="eyebrow">
            Pipeline Proyek
          </p>
          <Link href="/proyek" className="text-xs text-accent hover:underline">Kelola Proyek →</Link>
        </div>
        {projects.length === 0 ? (
          <div className="rounded-lg border border-border bg-surface p-5 text-sm text-text-secondary">
            Belum ada proyek. <Link href="/proyek" className="text-accent hover:underline">Buat proyek pertama →</Link>
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border bg-surface">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-text-secondary">
                  <th className="py-2.5 pl-4 pr-3">Proyek</th>
                  <th className="py-2.5 pr-3">Fisik</th>
                  <th className="py-2.5 pr-3">Progress Biaya</th>
                  <th className="py-2.5 pr-3 min-w-[220px]">Unit</th>
                  <th className="py-2.5 pr-3 text-right">Pendapatan</th>
                  <th className="py-2.5 pr-3 text-right">HPP</th>
                  <th className="py-2.5 pr-3 text-right">Margin</th>
                  <th className="py-2.5 pr-4">Collection</th>
                </tr>
              </thead>
              <tbody>
                {projects.map((p) => (
                  <tr key={p.id} className="border-b border-border last:border-0 hover:bg-border-subtle/40">
                    <td className="py-3 pl-4 pr-3">
                      <Link href={`/proyek/${p.id}`} className="font-medium text-accent hover:underline">
                        {p.name}
                      </Link>
                      <p className="text-[11px] text-text-tertiary capitalize">{p.status}</p>
                    </td>
                    <td className="py-3 pr-3">
                      {/* R3 — fisik (input manual) + warning biaya mendahului */}
                      {p.physical_pct != null ? (
                        <span className="inline-flex items-center gap-1">
                          <Pct v={p.physical_pct} />
                          {p.cost_ahead_warning && (
                            <span title="Biaya berjalan >15pt di depan fisik" className="text-danger">⚠</span>
                          )}
                        </span>
                      ) : (
                        <span className="text-xs text-text-tertiary">belum diinput</span>
                      )}
                    </td>
                    <td className="py-3 pr-3">
                      <Pct v={String(Math.max(parseFloat(p.progress_pct) || 0, 0))} />
                    </td>
                    <td className="py-3 pr-3"><UnitFunnel p={p} /></td>
                    <td className="py-3 pr-3 text-right tabular-nums">
                      <Rupiah value={p.revenue} colorSign={false} />
                    </td>
                    <td className="py-3 pr-3 text-right tabular-nums">
                      <Rupiah value={p.hpp} colorSign={false} />
                    </td>
                    <td className="py-3 pr-3 text-right tabular-nums">
                      <span className={p.margin.startsWith("-") ? "text-danger" : "text-success"}>
                        <Rupiah value={p.margin} colorSign={false} />
                      </span>
                      <p className="text-[11px] text-text-tertiary">{p.margin_pct}%</p>
                    </td>
                    <td className="py-3 pr-4"><Pct v={p.collection_pct} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Laporan — semua dari dashboard */}
      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-surface px-4 py-3">
        <span className="eyebrow mr-1">
          Laporan
        </span>
        {REPORT_LINKS.map((r) => (
          <Link
            key={r.label}
            href={r.href}
            className="rounded-full border border-border px-3 py-1 text-xs text-text-primary hover:border-accent hover:text-accent transition-colors"
          >
            {r.label}
          </Link>
        ))}
      </div>
    </div>
  );
}
