import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import type {
  CashFlowMonth,
  DashboardBurn,
  DashboardFunnel,
  DashboardProject,
} from "@/lib/api/reports";
import type { RABvsRealisasiReport } from "@/lib/types/api";

// EXECUTIVE DASHBOARD — chart primitives. SVG/div murni (server component,
// zero dependency) agar konsisten dengan design token & tanpa bundle chart lib.
// Semua chart clickable → laporan sumber (ledger-centric drill-down).

// ── Util ──────────────────────────────────────────────────────────────────────

function num(s: string): number {
  const n = parseFloat(s);
  return isNaN(n) ? 0 : n;
}

// Format ringkas rupiah untuk label sumbu: 1.2M / 350jt / 75rb.
export function compactIDR(v: number): string {
  const abs = Math.abs(v);
  const sign = v < 0 ? "-" : "";
  if (abs >= 1_000_000_000_000) return `${sign}${(abs / 1_000_000_000_000).toFixed(1).replace(/\.0$/, "")}T`;
  if (abs >= 1_000_000_000) return `${sign}${(abs / 1_000_000_000).toFixed(1).replace(/\.0$/, "")}M`;
  if (abs >= 1_000_000) return `${sign}${(abs / 1_000_000).toFixed(0)}jt`;
  if (abs >= 1_000) return `${sign}${(abs / 1_000).toFixed(0)}rb`;
  return `${sign}${abs.toFixed(0)}`;
}

const MONTH_SHORT = ["Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"];

function monthLabel(ym: string): string {
  const m = parseInt(ym.slice(5, 7), 10);
  return MONTH_SHORT[m - 1] ?? ym;
}

// Backend kadang sudah menyertakan "%" (mis. persen_realisasi "12.50%").
function pctLabel(s: string): string {
  return s.endsWith("%") ? s : `${s}%`;
}

function ChartCard({
  title, subtitle, href, linkLabel, children,
}: {
  title: string;
  subtitle?: string;
  href?: string;
  linkLabel?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-xl border border-border bg-surface p-4 shadow-sm flex flex-col">
      <div className="mb-3 flex items-start justify-between gap-2">
        <div>
          <p className="text-sm font-semibold text-text-primary">{title}</p>
          {subtitle && <p className="text-[11px] text-text-tertiary mt-0.5">{subtitle}</p>}
        </div>
        {href && (
          <Link href={href} className="text-xs text-accent hover:underline shrink-0">
            {linkLabel ?? "Detail →"}
          </Link>
        )}
      </div>
      <div className="flex-1">{children}</div>
    </div>
  );
}

// ── Cash In vs Cash Out (grouped bar, 6 bulan) ────────────────────────────────

export function CashFlowChart({ series }: { series: CashFlowMonth[] }) {
  const max = Math.max(1, ...series.flatMap((s) => [num(s.cash_in), num(s.cash_out)]));
  const H = 120;

  return (
    <ChartCard
      title="Cash In vs Cash Out"
      subtitle="6 bulan terakhir — dari mutasi akun kas & bank (posted)"
      href="/laporan?tab=aruskas"
      linkLabel="Arus Kas →"
    >
      <div className="flex items-end gap-2" style={{ height: H + 30 }}>
        {series.map((s) => {
          const inH = Math.round((num(s.cash_in) / max) * H);
          const outH = Math.round((num(s.cash_out) / max) * H);
          return (
            <div key={s.month} className="flex-1 flex flex-col items-center gap-1 min-w-0">
              <div className="flex items-end gap-1" style={{ height: H }}>
                <div
                  className="w-3.5 rounded-t-[3px] bg-success hover:opacity-85 transition-opacity"
                  style={{ height: Math.max(inH, num(s.cash_in) > 0 ? 3 : 0) }}
                  title={`Masuk ${monthLabel(s.month)}: Rp ${Number(num(s.cash_in)).toLocaleString("id-ID")}`}
                />
                <div
                  className="w-3.5 rounded-t-[3px] bg-danger hover:opacity-85 transition-opacity"
                  style={{ height: Math.max(outH, num(s.cash_out) > 0 ? 3 : 0) }}
                  title={`Keluar ${monthLabel(s.month)}: Rp ${Number(num(s.cash_out)).toLocaleString("id-ID")}`}
                />
              </div>
              <p className="text-[10px] text-text-secondary">{monthLabel(s.month)}</p>
            </div>
          );
        })}
      </div>
      <div className="mt-2 flex items-center gap-4 text-[11px] text-text-secondary">
        <span className="flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-sm bg-success" /> Masuk
        </span>
        <span className="flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-sm bg-danger" /> Keluar
        </span>
        <span className="ml-auto tabular-nums">maks {compactIDR(max)}</span>
      </div>
    </ChartCard>
  );
}

// ── Sales Funnel (lead → booking → kontrak → BAST) ────────────────────────────

export function SalesFunnelChart({ funnel }: { funnel: DashboardFunnel }) {
  // Funnel = magnitude menyempit → SATU hue (terracotta) menua bertahap,
  // bukan 4 warna berbeda (dulu sky/amber/violet/emerald = tabrakan).
  // S9/R-9: conversion % dari backend (booking_conv_pct dkk) — FE display saja.
  const stages = [
    { label: "Lead Aktif", n: funnel.leads_active, pct: null as string | null, href: "/penjualan/sales?tab=leads", cls: "bg-accent/45" },
    { label: "Booking Aktif", n: funnel.booking_active, pct: funnel.booking_conv_pct, href: "/penjualan/booking?status=active", cls: "bg-accent/60" },
    { label: "Kontrak Berjalan", n: funnel.contracts_active, pct: funnel.contract_conv_pct, href: "/penjualan", cls: "bg-accent/80" },
    { label: "Terjual (BAST)", n: funnel.units_sold, pct: funnel.sold_conv_pct, href: "/penjualan?filter=sold", cls: "bg-accent" },
  ];
  const max = Math.max(1, ...stages.map((s) => s.n));

  return (
    <ChartCard
      title="Sales Funnel"
      subtitle={`lead → booking → kontrak → serah terima · ${funnel.leads_converted} lead terkonversi`}
      href="/penjualan/sales"
      linkLabel="Sales & CRM →"
    >
      <div className="space-y-2.5">
        {stages.map((s, i) => (
          <Link key={s.label} href={s.href} className="block group">
            <div className="flex items-center justify-between text-xs mb-1">
              <span className="text-text-secondary group-hover:text-accent transition-colors">{s.label}</span>
              <span className="font-semibold tabular-nums">
                {s.n}
                {/* R5/S9 — conversion rate % dari backend */}
                {i > 0 && stages[i - 1].n > 0 && s.pct !== null && (
                  <span className="ml-1.5 font-normal text-text-tertiary">
                    ({s.pct}%)
                  </span>
                )}
              </span>
            </div>
            <div className="h-4 rounded bg-border-subtle overflow-hidden">
              <div
                className={`h-full rounded ${s.cls} group-hover:opacity-100 opacity-90 transition-opacity`}
                style={{ width: `${Math.max((s.n / max) * 100, s.n > 0 ? 4 : 0)}%` }}
              />
            </div>
          </Link>
        ))}
      </div>
    </ChartCard>
  );
}

// ── RAB vs Realisasi per kategori (bar + variance) ────────────────────────────

const CATEGORY_LABEL: Record<string, string> = {
  land: "Tanah",
  construction: "Konstruksi",
  soft: "Soft Cost",
  operational: "Operasional",
  marketing: "Marketing",
  other: "Lainnya",
};

export function RabVarianceChart({
  report, projectId, projectName,
}: {
  report: RABvsRealisasiReport;
  projectId: number;
  projectName: string;
}) {
  const rows = report.rows.filter((r) => num(r.budgeted) > 0 || num(r.realisasi) > 0);
  const max = Math.max(1, ...rows.flatMap((r) => [num(r.budgeted), num(r.realisasi)]));

  return (
    <ChartCard
      title={`RAB vs Realisasi — ${projectName}`}
      subtitle={`${report.plan_label} v${report.plan_version} · realisasi dari jurnal posted`}
      href={`/rab?project=${projectId}`}
      linkLabel="Kelola RAB →"
    >
      {rows.length === 0 ? (
        <p className="text-xs text-text-tertiary py-4">
          RAB aktif belum punya item berisi angka.{" "}
          <Link href={`/rab?project=${projectId}`} className="text-accent hover:underline">Susun RAB →</Link>
        </p>
      ) : (
        <div className="space-y-3">
          {rows.map((r) => {
            const b = num(r.budgeted);
            const a = num(r.realisasi);
            const over = a > b;
            return (
              <div key={r.category}>
                <div className="flex items-center justify-between text-xs mb-1">
                  <span className="text-text-secondary">{CATEGORY_LABEL[r.category] ?? r.category}</span>
                  <span className={`tabular-nums text-[11px] font-medium ${over ? "text-danger" : "text-text-tertiary"}`}>
                    {pctLabel(r.persen_realisasi)}{over ? " — lebih dari RAB" : ""}
                  </span>
                </div>
                <div className="space-y-1">
                  <div className="h-2.5 rounded bg-border-subtle overflow-hidden"
                    title={`RAB: Rp ${b.toLocaleString("id-ID")}`}>
                    <div className="h-full rounded bg-text-tertiary/45" style={{ width: `${(b / max) * 100}%` }} />
                  </div>
                  <div className="h-2.5 rounded bg-border-subtle overflow-hidden"
                    title={`Realisasi: Rp ${a.toLocaleString("id-ID")}`}>
                    <div
                      className={`h-full rounded ${over ? "bg-danger" : "bg-accent"}`}
                      style={{ width: `${Math.min((a / max) * 100, 100)}%` }}
                    />
                  </div>
                </div>
              </div>
            );
          })}
          <div className="flex items-center justify-between border-t border-border pt-2 text-xs">
            <span className="flex items-center gap-3 text-[11px] text-text-secondary">
              <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm bg-text-tertiary/45" /> RAB</span>
              <span className="flex items-center gap-1"><span className="h-2 w-2 rounded-sm bg-accent" /> Realisasi</span>
            </span>
            <span className="tabular-nums">
              Sisa RAB:{" "}
              <span className={num(report.total_selisih) < 0 ? "text-danger font-semibold" : "text-success font-semibold"}>
                <Rupiah value={report.total_selisih} colorSign={false} />
              </span>
            </span>
          </div>
        </div>
      )}
    </ChartCard>
  );
}

// ── Unit Status Heatmap (proyek × status) ─────────────────────────────────────

// Palet "Warm Ledger" tervalidasi (chart-1..6) — kohesif, colorblind-safe.
// Identitas status tak pernah warna-saja: header kolom menamai tiap status.
const HEAT_COLS: { key: keyof DashboardProject; label: string; cls: string }[] = [
  { key: "units_available", label: "Tersedia", cls: "14,139,119" },  // chart-2 teal
  { key: "units_booked", label: "Booking", cls: "62,104,168" },      // chart-4 slate-blue
  { key: "units_reserved", label: "Dipesan", cls: "185,138,30" },    // chart-3 ochre
  { key: "units_ppjb", label: "PPJB", cls: "154,70,111" },           // chart-5 plum
  { key: "units_sold", label: "Terjual", cls: "192,81,42" },         // chart-1 terracotta
  { key: "units_other", label: "Lainnya", cls: "111,122,36" },       // chart-6 olive
];

export function UnitHeatmap({ projects }: { projects: DashboardProject[] }) {
  const withUnits = projects.filter((p) => p.units_total > 0);
  const max = Math.max(1, ...withUnits.flatMap((p) => HEAT_COLS.map((c) => p[c.key] as number)));

  return (
    <ChartCard
      title="Unit Status Heatmap"
      subtitle="sebaran status unit per proyek — klik sel untuk kelola unit"
      href="/proyek"
      linkLabel="Semua Proyek →"
    >
      {withUnits.length === 0 ? (
        <p className="text-xs text-text-tertiary py-4">
          Belum ada unit.{" "}
          <Link href="/proyek" className="text-accent hover:underline">Tambahkan unit dari halaman proyek →</Link>
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-text-tertiary">
                <th className="pb-1.5 pr-2 text-left font-medium">Proyek</th>
                {HEAT_COLS.map((c) => (
                  <th key={c.label} className="pb-1.5 px-1 text-center font-medium">{c.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {withUnits.map((p) => (
                <tr key={p.id}>
                  <td className="py-1 pr-2">
                    <Link href={`/proyek/${p.id}?tab=penjualan`} className="text-accent hover:underline whitespace-nowrap">
                      {p.name}
                    </Link>
                  </td>
                  {HEAT_COLS.map((c) => {
                    const n = p[c.key] as number;
                    const alpha = n === 0 ? 0.06 : 0.25 + (n / max) * 0.65;
                    return (
                      <td key={c.label} className="p-1">
                        <Link
                          href={`/proyek/${p.id}?tab=penjualan`}
                          className="flex h-8 min-w-[52px] items-center justify-center rounded font-semibold tabular-nums transition-transform hover:scale-105"
                          style={{ backgroundColor: `rgba(${c.cls},${alpha})` }}
                          title={`${p.name} — ${c.label}: ${n} unit`}
                        >
                          {n > 0 ? n : ""}
                        </Link>
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </ChartCard>
  );
}

// ── Margin per proyek ─────────────────────────────────────────────────────────

export function MarginChart({ projects }: { projects: DashboardProject[] }) {
  const withRevenue = projects.filter((p) => num(p.revenue) !== 0 || num(p.hpp) !== 0);
  const max = Math.max(1, ...withRevenue.map((p) => Math.abs(num(p.margin))));

  return (
    <ChartCard
      title="Margin per Proyek"
      subtitle="pendapatan − HPP (kumulatif, jurnal posted)"
      href="/laporan?tab=pl"
      linkLabel="Laba Rugi →"
    >
      {withRevenue.length === 0 ? (
        <p className="text-xs text-text-tertiary py-4">
          Belum ada pendapatan diakui. Margin muncul setelah BAST pertama diproses.
        </p>
      ) : (
        <div className="space-y-2.5">
          {withRevenue.map((p) => {
            const m = num(p.margin);
            const neg = m < 0;
            return (
              <Link key={p.id} href={`/laporan?tab=pl&project=${p.id}`} className="block group">
                <div className="flex items-center justify-between text-xs mb-1">
                  <span className="text-text-secondary group-hover:text-accent transition-colors">{p.name}</span>
                  <span className={`tabular-nums font-semibold ${neg ? "text-danger" : "text-success"}`}>
                    <Rupiah value={p.margin} colorSign={false} />{" "}
                    <span className="font-normal text-text-tertiary">({p.margin_pct}%)</span>
                  </span>
                </div>
                <div className="h-3 rounded bg-border-subtle overflow-hidden">
                  <div
                    className={`h-full rounded ${neg ? "bg-danger" : "bg-success"} group-hover:opacity-100 opacity-90`}
                    style={{ width: `${Math.max((Math.abs(m) / max) * 100, 3)}%` }}
                  />
                </div>
              </Link>
            );
          })}
        </div>
      )}
    </ChartCard>
  );
}

// ── Progress pembangunan vs progress biaya ────────────────────────────────────

export function ProgressCompareChart({ projects }: { projects: DashboardProject[] }) {
  const rows = projects.filter((p) => num(p.budget_total) > 0 || num(p.contract_value) > 0);

  return (
    <ChartCard
      title="Progress Biaya vs Collection"
      subtitle="realisasi biaya thd RAB · kas diterima thd nilai kontrak"
      href="/proyek"
      linkLabel="Detail Proyek →"
    >
      {rows.length === 0 ? (
        <p className="text-xs text-text-tertiary py-4">
          Susun RAB dan buat kontrak agar perbandingan progress muncul.
        </p>
      ) : (
        <div className="space-y-3">
          {rows.map((p) => {
            const cost = Math.min(Math.max(num(p.progress_pct), 0), 100);
            const coll = Math.min(Math.max(num(p.collection_pct), 0), 100);
            return (
              <div key={p.id}>
                <Link href={`/proyek/${p.id}`} className="text-xs text-text-secondary hover:text-accent transition-colors">
                  {p.name}
                </Link>
                <div className="mt-1 space-y-1">
                  <div className="flex items-center gap-2">
                    <div className="h-2 flex-1 rounded bg-border-subtle overflow-hidden"
                      title={`Biaya terealisasi ${p.progress_pct}% dari RAB`}>
                      <div className="h-full rounded bg-chart-3" style={{ width: `${cost}%` }} />
                    </div>
                    <span className="w-24 text-right text-[11px] tabular-nums text-text-secondary">
                      biaya {cost.toFixed(2)}%
                    </span>
                  </div>
                  <div className="flex items-center gap-2">
                    <div className="h-2 flex-1 rounded bg-border-subtle overflow-hidden"
                      title={`Collection ${p.collection_pct}% dari nilai kontrak`}>
                      <div className="h-full rounded bg-accent" style={{ width: `${coll}%` }} />
                    </div>
                    <span className="w-24 text-right text-[11px] tabular-nums text-text-secondary">
                      kas {p.collection_pct}%
                    </span>
                  </div>
                </div>
              </div>
            );
          })}
          <p className="text-[11px] text-text-tertiary border-t border-border pt-2">
            Bila bar biaya jauh di depan bar kas → proyek membiayai sendiri; perhatikan arus kas.
          </p>
        </div>
      )}
    </ChartCard>
  );
}

// ── Top Performers (sales) ────────────────────────────────────────────────────

import type { SalesPerformanceRow } from "@/lib/api/crm";

export function TopPerformersChart({ rows }: { rows: SalesPerformanceRow[] }) {
  const ranked = [...rows]
    .filter((r) => num(r.contract_value) > 0 || r.leads > 0)
    .sort((a, b) => num(b.bast_value) - num(a.bast_value) || num(b.contract_value) - num(a.contract_value))
    .slice(0, 5);
  const max = Math.max(1, ...ranked.map((r) => num(r.contract_value)));

  return (
    <ChartCard
      title="Top Performers"
      subtitle="sales dengan nilai kontrak & BAST tertinggi"
      href="/penjualan/sales?tab=kinerja"
      linkLabel="Kinerja Sales →"
    >
      {ranked.length === 0 ? (
        <p className="text-xs text-text-tertiary py-4">
          Belum ada penjualan teratribusi sales. Pastikan kontrak memilih sales person.
        </p>
      ) : (
        <div className="space-y-3">
          {ranked.map((r, i) => (
            <div key={r.sales_person_id} className="flex items-center gap-3">
              <span className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[11px] font-bold
                ${i === 0 ? "bg-accent text-white" : "bg-border-subtle text-text-secondary"}`}>
                {i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between text-xs mb-1">
                  <span className="font-medium truncate">{r.name}</span>
                  <span className="tabular-nums text-text-secondary shrink-0">
                    <Rupiah value={r.contract_value} colorSign={false} />
                  </span>
                </div>
                <div className="h-2 rounded bg-border-subtle overflow-hidden">
                  <div className="h-full rounded bg-accent/80"
                    style={{ width: `${Math.max((num(r.contract_value) / max) * 100, 3)}%` }} />
                </div>
                <p className="mt-0.5 text-[10px] text-text-tertiary tabular-nums">
                  {r.bast_count} BAST · {r.contracts} kontrak · terkumpul{" "}
                  <Rupiah value={r.collected} colorSign={false} />
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
    </ChartCard>
  );
}

// ── Budget Health + Burn Rate (Executive Control Center) ──────────────────────

export function BudgetHealthCard({
  reports,
}: {
  reports: { projectId: number; projectName: string; report: RABvsRealisasiReport }[];
}) {
  return (
    <ChartCard
      title="Budget Health"
      subtitle="pemakaian RAB per proyek — dari jurnal posted"
      href="/rab"
      linkLabel="Kelola RAB →"
    >
      {reports.length === 0 ? (
        <p className="text-xs text-text-tertiary py-4">
          Belum ada RAB aktif. Proyek tanpa RAB tidak punya kendali anggaran.
        </p>
      ) : (
        <div className="space-y-3">
          {reports.map(({ projectId, projectName, report }) => {
            // S9/R-9: usage_pct + status dari backend — ambang 80/100 tidak
            // lagi di-hardcode FE (satu definisi: budget.BudgetHealthStatus).
            const pct = num(report.usage_pct);
            const status =
              report.status === "over" ? { label: "Over Budget", cls: "bg-danger-bg text-danger" } :
              report.status === "waspada" ? { label: "Waspada", cls: "bg-warning-bg text-warning" } :
              { label: "Sehat", cls: "bg-success-bg text-success" };
            const barCls = report.status === "over" ? "bg-danger" : report.status === "waspada" ? "bg-warning" : "bg-success";
            return (
              <Link key={projectId} href={`/proyek/${projectId}?tab=rab`} className="block group">
                <div className="flex items-center justify-between text-xs mb-1">
                  <span className="text-text-secondary group-hover:text-accent transition-colors">
                    {projectName}
                  </span>
                  <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${status.cls}`}>
                    {status.label} · {report.usage_pct}%
                  </span>
                </div>
                <div className="h-2.5 rounded bg-border-subtle overflow-hidden">
                  <div className={`h-full rounded ${barCls}`}
                    style={{ width: `${Math.min(pct, 100)}%` }} />
                </div>
                <p className="mt-0.5 text-[10px] text-text-tertiary tabular-nums">
                  terpakai <Rupiah value={report.total_realisasi} colorSign={false} /> dari{" "}
                  <Rupiah value={report.total_budgeted} colorSign={false} />
                </p>
              </Link>
            );
          })}
        </div>
      )}
    </ChartCard>
  );
}

export function BurnRateCard({ burn }: { burn: DashboardBurn }) {
  // S9/R-9: seluruh angka dari backend (payload dashboard.burn) — FE display saja.
  const avgOut = num(burn.avg_cash_out);
  const avgIn = num(burn.avg_cash_in);
  const netBurn = num(burn.monthly_burn); // positif = kas menyusut
  const runwayMonths = burn.runway_months != null ? num(burn.runway_months) : null;

  return (
    <ChartCard
      title="Burn Rate & Runway"
      subtitle="rata-rata 3 bulan terakhir — akun kas & bank (posted)"
      href="/laporan?tab=aruskas"
      linkLabel="Arus Kas →"
    >
      <div className="grid grid-cols-2 gap-3">
        <div className="rounded border border-border p-3">
          <p className="text-[11px] text-text-secondary">Kas keluar / bulan</p>
          <p className="mt-1 text-base font-semibold tabular-nums text-danger">
            {compactIDR(avgOut)}
          </p>
        </div>
        <div className="rounded border border-border p-3">
          <p className="text-[11px] text-text-secondary">Kas masuk / bulan</p>
          <p className="mt-1 text-base font-semibold tabular-nums text-success">
            {compactIDR(avgIn)}
          </p>
        </div>
        <div className="rounded border border-border p-3">
          <p className="text-[11px] text-text-secondary">Net burn / bulan</p>
          <p className={`mt-1 text-base font-semibold tabular-nums ${netBurn > 0 ? "text-danger" : "text-success"}`}>
            {netBurn > 0 ? compactIDR(netBurn) : `+${compactIDR(-netBurn)}`}
          </p>
        </div>
        <div className="rounded border border-border p-3">
          <p className="text-[11px] text-text-secondary">Runway kas</p>
          <p className="mt-1 text-base font-semibold tabular-nums">
            {runwayMonths === null
              ? "∞"
              : runwayMonths < 1
                ? "< 1 bulan"
                : `≈ ${runwayMonths.toFixed(1)} bulan`}
          </p>
        </div>
      </div>
      {runwayMonths !== null && runwayMonths < 3 && (
        <p className="mt-2 rounded bg-danger-bg px-2.5 py-1.5 text-[11px] text-danger">
          Runway di bawah 3 bulan — percepat collection atau tunda pengeluaran non-kritis.
        </p>
      )}
    </ChartCard>
  );
}
