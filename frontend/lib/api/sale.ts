import { apiFetch } from "./client";
import type {
  SaleRecord,
  SaleContract,
  TerminPayment,
  PaymentSchedule,
  BuyerBalance,
  CustomerStatement,
  AllocationView,
  TerminKind,
} from "@/lib/types/api";

export async function fetchSaleRecord(token: string, unitId: number): Promise<SaleRecord> {
  return apiFetch<SaleRecord>(`/units/${unitId}/sale-record`, { token });
}

export interface CreateContractInput {
  unit_id: number;
  buyer_name: string;
  buyer_id: string;
  payment_type: "kpr" | "tunai";
  bank_kpr?: string;
  contract_date: string; // RFC3339
  total_price: string;   // string integer rupiah
  // Increment 3 — WAJIB di backend produksi (scheme flow aktif):
  payment_scheme_id?: number;
  customer_id?: number;
  sales_person_id?: number;
  /** P1 — opsional; admin/dokumen/KPR, berbeda dari sales_person_id (komisi). */
  admin_marketing_person_id?: number;
  financing_source_id?: number; // wajib utk scheme kpr
  // Increment 7 — konversi booking → kontrak (atomik).
  booking_id?: number;
  /**
   * Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-konversi-kontrak-2026-08)
   * — berlaku baik untuk kontrak LANGSUNG maupun konversi Booking (booking_id
   * diisi). Booking sendiri tidak lagi membawa komponen tanah; dipilih di sini.
   */
  land_quantity_m2?: string;
}

export async function createContract(
  token: string,
  data: CreateContractInput,
): Promise<SaleContract> {
  return apiFetch<SaleContract>("/sale-contracts", {
    method: "POST",
    body: data,
    token,
  });
}

export interface ScheduleItemInput {
  installment_number: number;
  due_date: string; // RFC3339
  amount: string;   // string integer rupiah
  type: "dp" | "installment" | "final";
}

/** P1 — reassign Admin Marketing pada kontrak yang sudah ada. `null` = kosongkan. */
export async function setAdminMarketing(
  token: string,
  contractId: number,
  adminMarketingPersonId: number | null,
): Promise<SaleContract> {
  return apiFetch<SaleContract>(`/sale-contracts/${contractId}/admin-marketing`, {
    method: "PUT",
    body: { admin_marketing_person_id: adminMarketingPersonId },
    token,
  });
}

export async function createPaymentSchedule(
  token: string,
  contractId: number,
  items: ScheduleItemInput[],
): Promise<PaymentSchedule[]> {
  return apiFetch<PaymentSchedule[]>(`/sale-contracts/${contractId}/schedule`, {
    method: "POST",
    body: items,
    token,
  });
}

export async function getBuyerBalance(
  token: string,
  contractId: number,
): Promise<BuyerBalance> {
  return apiFetch<BuyerBalance>(`/sale-contracts/${contractId}/balance`, { token });
}

export async function fetchContractByUnit(token: string, unitId: number): Promise<SaleContract> {
  return apiFetch<SaleContract>(`/units/${unitId}/contract`, { token });
}

export async function fetchCustomerStatement(
  token: string,
  contractId: number,
  asOf?: string,
): Promise<CustomerStatement> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch<CustomerStatement>(`/sale-contracts/${contractId}/statement${qs}`, { token });
}

// FE-2 · P4 — breakdown alokasi sub-ledger untuk sebuah kontrak (read-only).
export async function fetchContractAllocations(
  token: string,
  contractId: number,
): Promise<AllocationView[]> {
  const res = await apiFetch<AllocationView[]>(`/sale-contracts/${contractId}/allocations`, { token });
  return res ?? [];
}

export async function fetchSchedulesByContract(
  token: string,
  contractId: number,
): Promise<PaymentSchedule[]> {
  const res = await apiFetch<PaymentSchedule[]>(`/sale-contracts/${contractId}/schedule`, { token });
  return res ?? [];
}

export interface RecordTerminInput {
  bank_account_code: string;
  amount: string; // string integer rupiah
  date: string;   // RFC3339
  description: string;
  /** W-13: Jenis Penerimaan terstruktur — opsional, default "other" di backend. */
  kind?: TerminKind;
  installment_no?: number;
}

export async function recordTermin(
  token: string,
  unitId: number,
  data: RecordTerminInput,
): Promise<TerminPayment> {
  return apiFetch<TerminPayment>(`/units/${unitId}/termins`, {
    method: "POST",
    body: data,
    token,
  });
}

export async function listTermins(
  token: string,
  unitId: number,
): Promise<{ data: TerminPayment[] }> {
  return apiFetch<{ data: TerminPayment[] }>(`/units/${unitId}/termins`, { token });
}

export interface RecordInstallmentInput {
  bank_account_code: string;
  received_at: string; // RFC3339
  description: string;
}

export async function recordInstallmentPaid(
  token: string,
  scheduleId: number,
  data: RecordInstallmentInput,
): Promise<PaymentSchedule> {
  return apiFetch<PaymentSchedule>(`/schedules/${scheduleId}/received`, {
    method: "POST",
    body: data,
    token,
  });
}

// Temuan #7: RecordAkad — pengakuan pendapatan+HPP (dulu "BAST").
export interface RecordAkadInput {
  sale_price: string; // string integer rupiah (DPP)
  is_vat: boolean;
  vat_rate?: string;  // e.g. "0.11"
  buyer_ref: string;
  recognition_date: string; // RFC3339
  /** Gap 1 (UAT 2026-09-07): nominal yang benar-benar disetujui bank — source
   *  of truth Dana Jaminan Bank KPR. Wajib diisi utk kontrak KPR (dulu diminta
   *  di Konversi Kontrak sbg "Nilai Pengajuan KPR" — dipindah ke sini). */
  bank_approved_amount?: string;
}

export async function recordAkad(
  token: string,
  unitId: number,
  data: RecordAkadInput,
): Promise<SaleRecord> {
  return apiFetch<SaleRecord>(`/units/${unitId}/akad`, {
    method: "POST",
    body: data,
    token,
  });
}

// Temuan #7: RecordPhysicalHandover — serah terima fisik, murni pencatatan,
// TANPA jurnal. Terpisah dari Akad; bisa terjadi kapan saja setelahnya.
export interface RecordPhysicalHandoverInput {
  handover_date: string; // RFC3339
}

export async function recordPhysicalHandover(
  token: string,
  unitId: number,
  data: RecordPhysicalHandoverInput,
): Promise<SaleRecord> {
  return apiFetch<SaleRecord>(`/units/${unitId}/physical-handover`, {
    method: "POST",
    body: data,
    token,
  });
}

export async function listDueSchedules(
  token: string,
  from: string,
  to: string,
): Promise<PaymentSchedule[]> {
  return apiFetch<PaymentSchedule[]>(
    `/schedules/due?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    { token },
  );
}

// PS-4 — sweep tandai cicilan lewat jatuh tempo menjadi overdue (idempoten).
export async function markOverdueSchedules(
  token: string,
): Promise<{ marked_overdue?: number; count?: number }> {
  return apiFetch<{ marked_overdue?: number; count?: number }>(`/schedules/mark-overdue`, {
    method: "POST",
    body: { as_of: new Date().toISOString() },
    token,
  });
}

// ── Scheme events (milestone pembiayaan KPR — Increment 3 backend) ────────────

export interface SchemeEventInput {
  event: string; // submitted_to_bank | bank_approved | bank_rejected | akad | disbursed | takeover
  financing_source_id?: number;
  notes?: string;
}

export async function applySchemeEvent(
  token: string,
  contractId: number,
  data: SchemeEventInput,
): Promise<{ id: number; event: string }> {
  return apiFetch(`/sale-contracts/${contractId}/scheme-events`, {
    method: "POST",
    body: data,
    token,
  });
}

// ── R1 KPR Realization ────────────────────────────────────────────────────────

export interface ContractFinancialSummary {
  contract_id: number;
  unit_id: number;
  unit_price: string;
  price_is_snapshot: boolean;
  discount: string;
  dpp: string;
  net_contract: string;
  total_paid: string;
  outstanding: string;
  /** Komponen Kelebihan Tanah (opsional) — dibekukan sejak kontrak dibuat. */
  has_land: boolean;
  land_amount: string;
  total_contract_value: string; // net_contract + land_amount
  total_outstanding: string;    // total_contract_value − total_paid (proyeksi pra-Akad)
  /** Sisa tagihan SESUNGGUHNYA rumah+tanah (payment_schedules type=land,
   *  pasca-Akad, superseded/dibatalkan diabaikan) — sama dengan `outstanding`
   *  sebelum Akad (belum ada baris tanah formal). Beda dari total_outstanding
   *  di atas yang proyeksi snapshot kontrak; ini baca anchor AR formal jadi
   *  tidak phantom bila land_sale dibatalkan pasca-Akad. Pakai ini untuk
   *  prefill/hint nominal pembayaran pasca-Akad. */
  total_outstanding_actual: string;
  /** Pasangan total_outstanding_actual — total kas gabungan rumah+tanah yang
   *  SESUNGGUHNYA sudah diterima (bukan Σ total_paid, yang mengecualikan
   *  pembayaran yang 100% meluber ke baris tanah). */
  total_paid_actual: string;
}

export async function fetchContractFinancialSummary(
  token: string,
  contractId: number,
): Promise<ContractFinancialSummary> {
  return apiFetch(`/sale-contracts/${contractId}/financial-summary`, { token });
}

// ── R6/U1 — rencana jadwal dari skema pembayaran kontrak ─────────────────────

export interface SchedulePlanItem {
  installment_number: number;
  due_date: string; // RFC3339
  amount: string;
  type: "dp" | "installment" | "final";
}

export async function fetchSchedulePlan(
  token: string,
  contractId: number,
): Promise<SchedulePlanItem[]> {
  const res = await apiFetch<{ items: SchedulePlanItem[] }>(
    `/sale-contracts/${contractId}/schedule-plan`,
    { token },
  );
  return res.items ?? [];
}

// ── W-8 — Piutang harga rumah: tie-out GL ↔ sub-ledger ───────────────────────
//
// BD-1: piutang harga rumah LAHIR SAAT BAST. Jadwal sebelum serah terima adalah
// rencana penagihan, bukan piutang — ia dibaca lewat fetchBillingPlan di bawah.

export interface HouseARReconLayer {
  control_account_code: string;
  control_account_name?: string;
  unit_count: number;
  subledger: string;
  ledger_house: string;
  difference: string;
  matched: boolean;
  ledger_total: string;
  ledger_other: string;
}

export interface HouseARReconciliation {
  as_of_date?: string;
  layers: HouseARReconLayer[];
  subledger_total: string;
  ledger_house_total: string;
  difference: string;
  matched: boolean;
  owned_journal_count: number;
  note?: string;
}

export async function fetchHouseARReconciliation(
  token: string,
  asOf?: string,
): Promise<HouseARReconciliation> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch(`/receivables/house/reconciliation${qs}`, { token });
}

// ── W-8/P6 — Jadwal Penagihan (pra-BAST) ─────────────────────────────────────

export type BillingPlanStatus = "paid" | "overdue" | "due_today" | "scheduled";

export interface BillingPlanLine {
  schedule_id: number;
  installment_number: number;
  type: string;
  label: string;
  due_date: string;
  amount: string;
  paid: string;
  outstanding: string;
  status: BillingPlanStatus;
  days_overdue: number;
  invoice_number?: string;
}

export interface BillingPlanUnit {
  contract_id: number;
  unit_id: number;
  unit_code: string;
  buyer_name: string;
  buyer_phone?: string;
  scheme_state?: string;
  contract_value: string;
  scheduled: string;
  paid: string;
  outstanding: string;
  overdue_amount: string;
  overdue_count: number;
  next_due_date?: string;
  schedules: BillingPlanLine[];
}

export interface BillingPlan {
  as_of: string;
  total_scheduled: string;
  total_paid: string;
  total_outstanding: string;
  total_overdue: string;
  unit_count: number;
  overdue_unit_count: number;
  note: string;
  units: BillingPlanUnit[];
}

export async function fetchBillingPlan(
  token: string,
  asOf?: string,
): Promise<BillingPlan> {
  const qs = asOf ? `?as_of=${asOf}` : "";
  return apiFetch(`/receivables/house/billing-plan${qs}`, { token });
}
