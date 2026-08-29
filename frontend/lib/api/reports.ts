// Report API — dipanggil dari server components dengan token dari cookie.
// Semua angka DATANG DARI BACKEND; frontend TIDAK menghitung apa pun.

import { apiFetch, apiBase } from "./client";
import type {
  NeracaReport,
  PLReport,
  PipelineReport,
  TaxLiabilityReport,
  ArusKasReport,
  TrialBalance,
  LedgerEntry,
  BASTWithoutPPhItem,
  ARAgingReport,
} from "@/lib/types/api";

export async function fetchNeraca(
  token: string,
  asOf?: string,
): Promise<NeracaReport> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch<NeracaReport>(`/reports/balance-sheet${qs}`, { token });
}

export async function fetchPL(
  token: string,
  asOf?: string,
): Promise<PLReport> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch<PLReport>(`/reports/income-statement${qs}`, { token });
}

export async function fetchProjectPL(
  token: string,
  projectId: number,
  asOf?: string,
): Promise<PLReport> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch<PLReport>(`/reports/project-pl/${projectId}${qs}`, { token });
}

export async function fetchCashFlow(
  token: string,
  from?: string,
  to?: string,
): Promise<ArusKasReport> {
  const params = new URLSearchParams();
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  const qs = params.toString() ? `?${params}` : "";
  return apiFetch<ArusKasReport>(`/reports/cash-flow${qs}`, { token });
}

export async function fetchPipeline(token: string): Promise<PipelineReport> {
  return apiFetch<PipelineReport>("/reports/sales-pipeline", { token });
}

export async function fetchTaxLiability(
  token: string,
  from?: string,
  to?: string,
): Promise<TaxLiabilityReport> {
  const params = new URLSearchParams();
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  const qs = params.toString() ? `?${params}` : "";
  return apiFetch<TaxLiabilityReport>(`/reports/tax-liability${qs}`, { token });
}

export async function fetchBASTWithoutPPh(
  token: string,
  from?: string,
  to?: string,
): Promise<BASTWithoutPPhItem[]> {
  const params = new URLSearchParams();
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  const qs = params.toString() ? `?${params}` : "";
  return apiFetch<BASTWithoutPPhItem[]>(`/tax/bast-without-pph${qs}`, { token });
}

// source (W-4) menyaring sumber eksposur: "house" | "realization". Kosong atau
// "all" berarti seluruh piutang customer — satu daftar, satu total.
export async function fetchARAging(
  token: string,
  asOf?: string,
  source?: string,
): Promise<ARAgingReport> {
  const params = new URLSearchParams();
  if (asOf) params.set("as_of", asOf);
  if (source && source !== "all") params.set("source", source);
  const qs = params.toString() ? `?${params}` : "";
  return apiFetch<ARAgingReport>(`/reports/ar-aging${qs}`, { token });
}

export async function fetchTrialBalance(
  token: string,
  asOf?: string,
): Promise<TrialBalance> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch<TrialBalance>(`/reports/trial-balance${qs}`, { token });
}

export async function fetchGeneralLedger(
  token: string,
  accountId: number,
  opts?: { from?: string; to?: string; projectId?: number },
): Promise<LedgerEntry[]> {
  const params = new URLSearchParams();
  if (opts?.from) params.set("from", opts.from);
  if (opts?.to) params.set("to", opts.to);
  if (opts?.projectId) params.set("project_id", String(opts.projectId));
  const qs = params.toString() ? `?${params}` : "";
  return apiFetch<LedgerEntry[]>(`/reports/trial-balance/${accountId}${qs}`, { token });
}

// ── PS-2 — Dashboard Owner V2 (satu payload komposit) ─────────────────────────

export interface DashboardKPI {
  cash: string;
  receivable: string;
  payable: string;
  booking_deposits: string;
  commission_payable: string;
  sales_mtd: string;
  // P3 (item A, 2026-08-27) — Cash vs KPR: pendapatan inti bulan ini dipecah
  // via payment_type kontrak. cash + kpr bisa < sales_mtd bila ada pendapatan
  // yang belum/tidak ber-kontrak (tidak dipaksa ke salah satu sisi).
  pendapatan_cash_mtd: string;
  pendapatan_kpr_mtd: string;
  // Komposisi nilai SELURUH kontrak (GrossAmount, semua waktu) — portofolio,
  // bukan pendapatan diakui.
  kontrak_value_cash: string;
  kontrak_value_kpr: string;
  booking_active: number;
  booking_fee_held: string;
  units_sold_mtd: number;
  margin_mtd: string;
  cash_in_mtd: string;
  cash_out_mtd: string;
  net_cash_flow_mtd: string;
  refund_pending: number;
  cancellation_awaiting: number;
  commission_queue: number;
  trueup_awaiting: number;
  overdue_schedules: number;
}

export interface DashboardProject {
  id: number;
  name: string;
  status: string;
  budget_total: string;
  actual_cost: string;
  progress_pct: string;
  units_available: number;
  units_booked: number;
  units_reserved: number;
  units_ppjb: number;
  units_sold: number;
  units_other: number;
  units_total: number;
  revenue: string;
  hpp: string;
  margin: string;
  margin_pct: string;
  contract_value: string;
  collected: string;
  collection_pct: string;
  // R3 — progress fisik (input manual append-only; null = belum diinput).
  physical_pct?: string | null;
  physical_as_of?: string | null;
  cost_ahead_warning: boolean;
}

export interface CashFlowMonth {
  month: string; // "2026-02"
  cash_in: string;
  cash_out: string;
}

export interface DashboardFunnel {
  leads_active: number;
  leads_converted: number;
  booking_active: number;
  contracts_active: number;
  units_sold: number;
  // S9/R-9: conversion % antar-stage dari backend.
  booking_conv_pct: string;
  contract_conv_pct: string;
  sold_conv_pct: string;
}

// S9/R-9: burn rate & runway dihitung backend dari seri kas kanonik.
export interface DashboardBurn {
  avg_cash_in: string;
  avg_cash_out: string;
  monthly_burn: string; // positif = kas menyusut
  runway_months?: string;
}

// R5 — seksi KPR dashboard (agregat pipeline R1).
export interface DashboardKPRSection {
  count_preparation: number;
  count_submitted: number;
  count_approved: number;
  count_akad: number;
  count_disbursed: number;
  count_settled: number;
  total_disbursed: string;
  total_outstanding: string;
  stuck_count: number;
}

// R5 — outstanding + forecast kas + top penunggak.
export interface DashboardReceivables {
  outstanding_total: string;
  forecast_30: string;
  forecast_60: string;
  forecast_90: string;
  top_debtors: {
    contract_id: number;
    buyer_name: string;
    unit_code: string;
    overdue_total: string;
    overdue_count: number;
  }[];
}

// R5 — profit per unit terjual.
export interface UnitProfitRow {
  unit_id: number;
  unit_code: string;
  project: string;
  revenue: string;
  hpp: string;
  margin: string;
  margin_pct: string;
}

export interface DashboardReport {
  as_of: string;
  kpi: DashboardKPI;
  projects: DashboardProject[];
  cash_flow: CashFlowMonth[];
  funnel: DashboardFunnel;
  kpr?: DashboardKPRSection;
  receivables?: DashboardReceivables;
  unit_profits?: UnitProfitRow[];
  burn?: DashboardBurn;
}

export async function fetchDashboard(token: string): Promise<DashboardReport> {
  return apiFetch<DashboardReport>(`/reports/dashboard`, { token });
}

// ── R1 — KPR Pipeline ─────────────────────────────────────────────────────────

export interface KPRPipelineRow {
  contract_id: number;
  unit_id: number;
  unit_code: string;
  project_name: string;
  buyer_name: string;
  bank_name?: string;
  scheme_state: string;
  gross_amount: string;
  loan_amount?: string;
  disbursed_total: string;
  total_paid: string;
  outstanding: string;
  state_since?: string;
  state_age_days: number;
}

export interface KPRPipelineReport {
  as_of: string;
  rows: KPRPipelineRow[];
  count_preparation: number;
  count_submitted: number;
  count_approved: number;
  count_akad: number;
  count_disbursed: number;
  count_settled: number;
  total_disbursed: string;
  total_outstanding: string;
}

export async function fetchKPRPipeline(token: string): Promise<KPRPipelineReport> {
  return apiFetch<KPRPipelineReport>(`/reports/kpr-pipeline`, { token });
}

// ── Export PDF (cetak HTML A4, browser yang menyimpan sebagai PDF) ───────────
//
// Sama seperti fetchInvoicePrintHTML/fetchReceiptPrintHTML (lihat lib/api/billing.ts):
// backend TIDAK memakai library PDF — merender HTML siap-cetak, browser yang
// "Cetak / Simpan PDF" lewat window.print(). `report` memakai key yang SAMA
// dengan ExportCsvButton (lihat REPORT_PATHS di app/api/reports/csv/route.ts)
// supaya satu `exportParams` di laporan/page.tsx melayani CSV maupun PDF.
const PRINT_PATHS: Record<string, (p: URLSearchParams) => string> = {
  "balance-sheet": (p) => `/reports/balance-sheet/print?${p}`,
  "income-statement": (p) => `/reports/income-statement/print?${p}`,
  "project-pl": (p) => `/reports/project-pl/${p.get("project_id") ?? "0"}/print?as_of=${p.get("as_of") ?? ""}`,
  "cash-flow": (p) => `/reports/cash-flow/print?${p}`,
  "sales-pipeline": () => `/reports/sales-pipeline/print`,
  "tax-liability": (p) => `/reports/tax-liability/print?${p}`,
  "trial-balance": (p) => `/reports/trial-balance/print?${p}`,
  "ar-aging": (p) => `/reports/ar-aging/print?${p}`,
  "general-ledger": (p) => `/reports/trial-balance/${p.get("account_id") ?? "0"}/print?${p}`,
  "ap-invoices": (p) => `/ap/invoices/print?${p}`,
};

export async function fetchReportPrintHTML(
  token: string,
  report: string,
  params: Record<string, string>,
): Promise<string> {
  const builder = PRINT_PATHS[report];
  if (!builder) {
    throw new Error("Laporan ini belum mendukung ekspor PDF");
  }
  const res = await fetch(`${apiBase()}${builder(new URLSearchParams(params))}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`Gagal memuat laporan (${res.status})`);
  }
  return res.text();
}
