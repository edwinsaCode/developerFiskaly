import { apiFetch, apiBase, ApiError } from "./client";

// ── W-7 — Piutang Proyek Lama (Legacy AR) ────────────────────────────────────
//
// Sisa tagihan dari proyek yang berjalan SEBELUM sistem ini dipakai. Satu
// kalimat menjelaskan seluruh modul ini:
//
//   Impor piutang lama adalah pengisian RINCIAN atas saldo yang sudah ada di
//   buku besar — bukan pencatatan piutang baru.
//
// Karena itu impor tidak pernah menghasilkan jurnal. Yang menghasilkan jurnal
// hanya PELUNASAN (Dr Kas / Cr akun kontrol), karena itu kejadian ekonomi baru.

export type LegacyBatchStatus = "draft" | "committed" | "discarded";
export type LegacyParseStatus = "ok" | "warning" | "error";
export type LegacyStatus = "open" | "paid" | "written_off";

export interface LegacyBatch {
  id: number;
  file_name: string;
  file_size: number;
  file_hash: string;
  as_of_date: string;
  control_account_code: string;
  status: LegacyBatchStatus;
  row_count: number;
  valid_count: number;
  error_count: number;
  warning_count: number;
  total_amount: string;
  opening_journal_id?: number;
  skip_reason?: string;
  notes?: string;
  committed_at?: string;
  created_at: string;
}

export interface LegacyRowIssue {
  column: string;
  message: string;
}

// Kolom Raw* adalah isi sel APA ADANYA. Ditampilkan berdampingan dengan hasil
// bacanya karena ketika klien protes "angkanya bukan segitu", yang harus bisa
// dibuka adalah apa yang mereka kirim — bukan tafsir kita atasnya.
export interface LegacyBatchRow {
  id: number;
  batch_id: number;
  line_no: number;
  raw_customer_name: string;
  raw_source_label: string;
  raw_external_ref: string;
  raw_outstanding: string;
  raw_due_date: string;
  raw_phone: string;
  raw_email: string;
  raw_notes: string;
  amount: string;
  due_date?: string;
  parse_status: LegacyParseStatus;
  issues?: LegacyRowIssue[];
  legacy_receivable_id?: number;
}

export interface LegacyReceivable {
  id: number;
  batch_id: number;
  customer_name: string;
  customer_id?: number;
  source_label: string;
  external_ref?: string;
  phone?: string;
  email?: string;
  control_account_code: string;
  as_of_date: string;
  due_date?: string;
  original_amount: string;
  paid_amount: string;
  status: LegacyStatus;
  notes?: string;
  created_at: string;
}

export interface LegacyReceivableView extends LegacyReceivable {
  outstanding: string;
  payment_count: number;
}

export interface LegacySummary {
  count: number;
  total_original: string;
  total_paid: string;
  total_outstanding: string;
  open_count: number;
  paid_count: number;
  written_off_count: number;
}

export interface LegacyListResult {
  rows: LegacyReceivableView[];
  summary: LegacySummary;
  source_labels: string[];
}

export interface LegacyPayment {
  id: number;
  legacy_receivable_id: number;
  // Negatif hanya untuk baris pembatalan (cermin dari baris asal). Riwayatnya
  // tetap utuh — pembatalan tidak menghapus baris (append-only).
  amount: string;
  payment_date: string;
  cash_account_code: string;
  control_account_code: string;
  journal_entry_id: number;
  document_id?: number;
  document_number?: string;
  voids_payment_id?: number;
  notes?: string;
  created_at: string;
}

export type LegacyAuditEvent = "imported" | "paid" | "payment_void" | "customer_linked";

export interface LegacyAudit {
  id: number;
  legacy_receivable_id: number;
  event: LegacyAuditEvent;
  amount: string;
  outstanding_after: string;
  detail?: string;
  created_at: string;
}

export interface LegacyDetail {
  receivable: LegacyReceivable;
  outstanding: string;
  payments: LegacyPayment[];
  audits: LegacyAudit[];
  batch?: LegacyBatch;
}

// Rekonsiliasi sengaja berbentuk "kupas lapis", bukan "bandingkan dua angka
// besar": saldo akun kontrol dipecah menurut asal jurnalnya supaya yang
// dibandingkan dengan rincian piutang lama hanyalah PORSI SALDO AWAL-nya.
// Kalau yang dibandingkan saldo hidup, rekonsiliasi akan rusak setiap kali ada
// customer membayar — dan kontrol yang rusak tiap hari berhenti dibaca orang.
export interface LegacyReconciliation {
  control_account_code: string;
  control_account_name: string;
  as_of_date: string;
  ledger_balance: string;
  operational_movement: string;
  legacy_payment_effect: string;
  opening_portion: string;
  legacy_existing: string;
  incoming: string;
  subledger_total: string;
  difference: string;
  matched: boolean;
  ledger_available: boolean;
  note?: string;
}

export interface LegacyPreview {
  batch: LegacyBatch;
  rows: LegacyBatchRow[];
  reconciliation: LegacyReconciliation;
}

export interface LegacyCommitResult {
  batch: LegacyBatch;
  imported_count: number;
  skipped_count: number;
  total_imported: string;
  opening_journal_id?: number;
  reconciliation: LegacyReconciliation;
}

export interface LegacyPaymentResult {
  journal_entry_id: number;
  document_id?: number;
  document_number?: string;
  total_amount: string;
  payments: LegacyPayment[];
  duplicate?: boolean;
}

// ── Daftar & detail ──────────────────────────────────────────────────────────

export interface LegacyListFilter {
  status?: LegacyStatus | "";
  q?: string;
  source_label?: string;
  only_open?: boolean;
}

export async function fetchLegacyReceivables(
  token: string,
  filter: LegacyListFilter = {},
): Promise<LegacyListResult> {
  const p = new URLSearchParams();
  if (filter.status) p.set("status", filter.status);
  if (filter.q) p.set("q", filter.q);
  if (filter.source_label) p.set("source_label", filter.source_label);
  if (filter.only_open) p.set("only_open", "true");
  const qs = p.toString();
  return apiFetch<LegacyListResult>(`/legacy-ar${qs ? `?${qs}` : ""}`, { token });
}

export async function fetchLegacyReceivable(token: string, id: number): Promise<LegacyDetail> {
  return apiFetch<LegacyDetail>(`/legacy-ar/${id}`, { token });
}

export async function fetchLegacyReconciliation(
  token: string,
  asOf?: string,
  controlAccountCode?: string,
): Promise<LegacyReconciliation> {
  const p = new URLSearchParams();
  if (asOf) p.set("as_of", asOf);
  if (controlAccountCode) p.set("control_account_code", controlAccountCode);
  const qs = p.toString();
  return apiFetch<LegacyReconciliation>(`/legacy-ar/reconciliation${qs ? `?${qs}` : ""}`, { token });
}

// ── Impor ────────────────────────────────────────────────────────────────────

// templateUrl: tautan unduh template resmi — SELALU same-origin lewat proxy Next.
//
// Sengaja tidak memakai apiBase(): ini bukan URL yang di-fetch kode, melainkan
// href yang ditulis ke HTML dan diikuti BROWSER. apiBase() menjawab "http://
// backend:8080/..." saat dirender di server — host internal yang tidak bisa
// diresolusi browser, dan sekaligus memicu hydration mismatch karena klien
// menjawab beda. Yang menentukan bentuk tautan ini adalah siapa yang MENGKLIK,
// bukan siapa yang merender.
export function legacyTemplateUrl(): string {
  return "/api/v1/legacy-ar/template";
}

export interface UploadLegacyInput {
  file: File;
  asOfDate: string;
  controlAccountCode?: string;
  notes?: string;
}

// uploadLegacyBatch memakai fetch mentah, bukan apiFetch: badannya multipart,
// dan menyetel Content-Type sendiri akan menghapus boundary yang dihasilkan
// browser sehingga server tidak bisa membaca berkasnya sama sekali.
export async function uploadLegacyBatch(
  token: string,
  input: UploadLegacyInput,
): Promise<LegacyPreview> {
  const fd = new FormData();
  fd.append("file", input.file);
  fd.append("as_of_date", input.asOfDate);
  if (input.controlAccountCode) fd.append("control_account_code", input.controlAccountCode);
  if (input.notes) fd.append("notes", input.notes);

  const res = await fetch(`${apiBase()}/legacy-ar/batches`, {
    method: "POST",
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    body: fd,
  });
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    let payload: Record<string, unknown> | undefined;
    try {
      const err = (await res.json()) as { error?: string } & Record<string, unknown>;
      message = err.error ?? message;
      payload = err;
    } catch {
      // biarkan pesan default
    }
    throw new ApiError(res.status, message, payload);
  }
  return (await res.json()) as LegacyPreview;
}

export async function fetchLegacyBatch(token: string, batchID: number): Promise<LegacyPreview> {
  return apiFetch<LegacyPreview>(`/legacy-ar/batches/${batchID}`, { token });
}

export interface CommitLegacyInput {
  skip_reason?: string;
  opening_counter_account_code?: string;
  opening_description?: string;
}

export async function commitLegacyBatch(
  token: string,
  batchID: number,
  input: CommitLegacyInput = {},
): Promise<LegacyCommitResult> {
  return apiFetch<LegacyCommitResult>(`/legacy-ar/batches/${batchID}/commit`, {
    method: "POST",
    token,
    body: input,
  });
}

export async function discardLegacyBatch(token: string, batchID: number): Promise<void> {
  await apiFetch(`/legacy-ar/batches/${batchID}/discard`, { method: "POST", token, body: {} });
}

// ── Pelunasan ────────────────────────────────────────────────────────────────

export interface LegacyAllocationInput {
  receivable_id: number;
  amount: string;
}

export interface LegacyPaymentInput {
  allocations: LegacyAllocationInput[];
  cash_account_code: string;
  payment_date?: string;
  notes?: string;
  // idempotency_key dibuat sekali saat modal dibuka dan DIPAKAI ULANG pada
  // percobaan kedua. Itulah yang membuat klik ganda atau retry jaringan tidak
  // menghasilkan dua penerimaan kas.
  idempotency_key?: string;
}

export async function receiveLegacyPayment(
  token: string,
  input: LegacyPaymentInput,
): Promise<LegacyPaymentResult> {
  return apiFetch<LegacyPaymentResult>("/legacy-ar/payments", {
    method: "POST",
    token,
    body: input,
  });
}

export async function voidLegacyPayment(
  token: string,
  paymentID: number,
  reason: string,
): Promise<LegacyPaymentResult> {
  return apiFetch<LegacyPaymentResult>(`/legacy-ar/payments/${paymentID}/void`, {
    method: "POST",
    token,
    body: { reason },
  });
}
