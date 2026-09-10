import { apiBase, apiFetch } from "./client";
import type {
  CostPreviewResponse,
  ExpenseListItem,
  ExpenseScope,
  ExpenseType,
  FixedAsset,
  FixedAssetAcquisitionPreview,
} from "@/lib/types/api";

// Klien Transaksi Pengeluaran (W-10).
//
// Satu endpoint untuk dua scope. Bentuk body-nya sengaja dibuat sama persis
// dengan DTO backend supaya tidak ada lapisan penerjemah kedua yang bisa
// menyimpang — aturan akuntansi tetap tinggal di server.
//
// purchase_type == "fixed_asset" adalah pintu masuk YANG SAMA (bukan endpoint
// baru): field scope/expense_type_id/category/cost_tier diabaikan backend,
// diganti field khusus aset tetap di bawah.

export interface ExpenseBody {
  purchase_type?: "expense" | "fixed_asset";
  scope: ExpenseScope;

  date: string;              // YYYY-MM-DD
  amount: string;            // rupiah bulat, string (jangan pernah number)
  payment_method: "bank";    // §9: hutang usaha belum punya jalur pelunasan
  bank_account_code: string;
  vendor: string;
  description: string;

  // scope operasional
  expense_type_id?: number;

  // scope proyek (project_id juga boleh diisi untuk operasional = tag cost center)
  project_id?: number;
  unit_id?: number;
  phase_id?: number;
  budget_item_id?: number;
  category?: string;
  cost_tier?: string;
  // UAT 2026-09-07: produksi_subsidi|produksi_komersial|sarana_prasarana|
  // perizinan — hanya bermakna saat category="hard", wajib saat cost_tier
  // "shared" (tidak ditautkan unit).
  hard_subcategory?: string;

  // purchase_type == fixed_asset
  fixed_asset_category_id?: number;
  asset_name?: string;
  residual_value?: string;
  useful_life_months?: number;
  depreciation_method?: string;
}

export type ExpensePreviewResult = CostPreviewResponse | FixedAssetAcquisitionPreview;
export type ExpenseCreateResult = ExpenseListItem | FixedAsset;

// isFixedAssetPreview / isFixedAssetResult: pembeda bentuk respons union di
// atas — backend tidak mengirim field diskriminan eksplisit, jadi dibedakan
// dari bentuk field yang hanya ada di satu sisi.
export function isFixedAssetPreview(
  v: ExpensePreviewResult,
): v is FixedAssetAcquisitionPreview {
  return "debit_account_code" in v;
}

export function isFixedAssetResult(v: ExpenseCreateResult): v is FixedAsset {
  return "asset_code" in v;
}

export interface ExpenseListFilter {
  scope?: ExpenseScope | "";
  project_id?: number;
  from?: string;
  to?: string;
}

export async function fetchExpenses(
  token: string,
  filter: ExpenseListFilter = {},
): Promise<ExpenseListItem[]> {
  const q = new URLSearchParams();
  if (filter.scope) q.set("scope", filter.scope);
  if (filter.project_id) q.set("project_id", String(filter.project_id));
  if (filter.from) q.set("from", filter.from);
  if (filter.to) q.set("to", filter.to);
  const qs = q.toString();
  return apiFetch<ExpenseListItem[]>(`/expenses${qs ? `?${qs}` : ""}`, { token });
}

export async function fetchExpense(token: string, id: number): Promise<ExpenseListItem> {
  return apiFetch<ExpenseListItem>(`/expenses/${id}`, { token });
}

// fetchExpenseListPrintHTML mengambil HTML cetak Riwayat Biaya siap-A4 untuk
// satu proyek — representasi baca dari cost_entries, tidak ada apa pun yang
// ditulis di sini.
export async function fetchExpenseListPrintHTML(
  token: string,
  filter: ExpenseListFilter = {},
): Promise<string> {
  const q = new URLSearchParams();
  if (filter.scope) q.set("scope", filter.scope);
  if (filter.project_id) q.set("project_id", String(filter.project_id));
  if (filter.from) q.set("from", filter.from);
  if (filter.to) q.set("to", filter.to);
  const qs = q.toString();
  const res = await fetch(`${apiBase()}/expenses/print${qs ? `?${qs}` : ""}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`Gagal memuat riwayat biaya (${res.status})`);
  }
  return res.text();
}

// fetchExpensePrintHTML mengambil HTML cetak Bukti Kas Keluar (BKK) siap-A4
// untuk satu transaksi pengeluaran — representasi baca dari cost_entries,
// dokumen sudah ada (document_number), tidak ada apa pun yang ditulis di sini.
export async function fetchExpensePrintHTML(token: string, id: number): Promise<string> {
  const res = await fetch(`${apiBase()}/expenses/${id}/print`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`Gagal memuat bukti kas keluar (${res.status})`);
  }
  return res.text();
}

// previewExpense: dry-run. Mengembalikan jurnal PERSIS yang akan terbit,
// tanpa menulis apa pun.
export async function previewExpense(
  token: string,
  body: ExpenseBody,
): Promise<ExpensePreviewResult> {
  return apiFetch<ExpensePreviewResult>("/expenses/preview", { method: "POST", token, body });
}

// createExpense mencatat + memposting dalam satu transaksi. Responsnya sudah
// membawa nomor BKK dan referensi jurnal (jalur expense) atau baris Register
// yang baru terbentuk (jalur fixed_asset).
export async function createExpense(
  token: string,
  body: ExpenseBody,
): Promise<ExpenseCreateResult> {
  return apiFetch<ExpenseCreateResult>("/expenses", { method: "POST", token, body });
}

// ── Master jenis pengeluaran ────────────────────────────────────────────────

export async function fetchExpenseTypes(token: string): Promise<ExpenseType[]> {
  return apiFetch<ExpenseType[]>("/expense-types", { token });
}

export async function createExpenseType(
  token: string,
  body: { code: string; name: string; expense_account_code: string },
): Promise<ExpenseType> {
  return apiFetch<ExpenseType>("/expense-types", { method: "POST", token, body });
}

export async function updateExpenseType(
  token: string,
  id: number,
  body: { name?: string; expense_account_code?: string; is_active?: boolean },
): Promise<ExpenseType> {
  return apiFetch<ExpenseType>(`/expense-types/${id}`, { method: "PATCH", token, body });
}

export interface ExpenseTypeChange {
  id: number;
  code: string;
  field: string;
  old_value: string;
  new_value: string;
  created_at: string;
}

export async function fetchExpenseTypeChanges(token: string): Promise<ExpenseTypeChange[]> {
  return apiFetch<ExpenseTypeChange[]>("/expense-types/changes", { token });
}
