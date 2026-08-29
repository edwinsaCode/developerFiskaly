import { apiFetch, apiBase } from "./client";
import type {
  APInvoice,
  APInvoicePaymentRow,
  APInvoiceStatus,
  APInvoiceView,
  APPaymentPreview,
  APPaymentResult,
  APPaymentView,
  APPreviewResult,
  Vendor,
} from "@/lib/types/api";

// Klien Hutang Usaha (W-11).
//
// Bentuk body di bawah SAMA PERSIS dengan DTO backend (`internal/ap/handler.go`).
// Tidak ada lapisan penerjemah kedua: seluruh aturan akuntansi — DPP vs PPN,
// akun debit tiap baris, nilai kewajiban vendor — tinggal di server, dan layar
// hanya menampilkan apa yang dikembalikannya.

// ── Vendor ──────────────────────────────────────────────────────────────────

export interface VendorBody {
  name?: string;
  npwp?: string;
  is_pkp?: boolean;
  address?: string;
  phone?: string;
  email?: string;
  bank_name?: string;
  bank_account?: string;
  note?: string;
  // is_active hanya dipakai saat ubah — vendor dinonaktifkan, tidak dihapus.
  is_active?: boolean;
}

export interface VendorFilter {
  q?: string;
  active?: boolean;
}

export async function fetchVendors(
  token: string,
  filter: VendorFilter = {},
): Promise<Vendor[]> {
  const q = new URLSearchParams();
  if (filter.q) q.set("q", filter.q);
  if (filter.active !== undefined) q.set("active", filter.active ? "true" : "false");
  const qs = q.toString();
  return apiFetch<Vendor[]>(`/ap/vendors${qs ? `?${qs}` : ""}`, { token });
}

export async function fetchVendor(token: string, id: number): Promise<Vendor> {
  return apiFetch<Vendor>(`/ap/vendors/${id}`, { token });
}

export async function createVendor(token: string, body: VendorBody): Promise<Vendor> {
  return apiFetch<Vendor>("/ap/vendors", { method: "POST", token, body });
}

// updateVendor: field yang tidak dikirim TIDAK berubah (backend memakai pointer).
// Karena itu jangan pernah mengirim objek lengkap hasil rakitan ulang layar —
// `is_pkp` yang tidak sengaja ikut terkirim bisa membatalkan PPN yang sah.
export async function updateVendor(
  token: string,
  id: number,
  body: VendorBody,
): Promise<Vendor> {
  return apiFetch<Vendor>(`/ap/vendors/${id}`, { method: "PATCH", token, body });
}

// ── Tagihan vendor ──────────────────────────────────────────────────────────

export interface APInvoiceLineBody {
  unit_id?: number;
  phase_id?: number;
  category: string;
  cost_tier: string;
  amount: string; // rupiah bulat, string (jangan pernah number)
  budget_item_id?: number;
  description: string;
}

export interface APInvoiceBody {
  vendor_id: number;
  project_id: number; // 0 = tagihan overhead (tanpa proyek)

  invoice_number: string;
  invoice_date: string; // YYYY-MM-DD
  due_date: string; // YYYY-MM-DD

  dpp_amount: string;
  ppn_amount: string;
  faktur_pajak_number: string;

  description: string;
  lines: APInvoiceLineBody[];
}

export interface APInvoiceFilter {
  vendor_id?: number;
  project_id?: number;
  status?: APInvoiceStatus | "";
  due_before?: string; // YYYY-MM-DD
}

export async function fetchAPInvoices(
  token: string,
  filter: APInvoiceFilter = {},
): Promise<APInvoiceView[]> {
  const q = new URLSearchParams();
  if (filter.vendor_id) q.set("vendor_id", String(filter.vendor_id));
  if (filter.project_id) q.set("project_id", String(filter.project_id));
  if (filter.status) q.set("status", filter.status);
  if (filter.due_before) q.set("due_before", filter.due_before);
  const qs = q.toString();
  return apiFetch<APInvoiceView[]>(`/ap/invoices${qs ? `?${qs}` : ""}`, { token });
}

export async function fetchAPInvoice(token: string, id: number): Promise<APInvoiceView> {
  return apiFetch<APInvoiceView>(`/ap/invoices/${id}`, { token });
}

// previewAPInvoice: dry-run. Backend menjalankan SELURUH validasi dan komposisi
// yang dijalankan create, lalu berhenti tepat sebelum menulis — jadi angka dan
// baris jurnal di layar pratinjau bukan tiruan, melainkan hasilnya sendiri.
export async function previewAPInvoice(
  token: string,
  body: APInvoiceBody,
): Promise<APPreviewResult> {
  return apiFetch<APPreviewResult>("/ap/invoices/preview", { method: "POST", token, body });
}

// createAPInvoice menyimpan sebagai DRAFT: belum ada saldo hutang, belum ada
// realisasi RAB. Pengakuan baru terjadi lewat postAPInvoice.
export async function createAPInvoice(
  token: string,
  body: APInvoiceBody,
): Promise<APInvoice> {
  return apiFetch<APInvoice>("/ap/invoices", { method: "POST", token, body });
}

export async function postAPInvoice(token: string, id: number): Promise<APInvoice> {
  return apiFetch<APInvoice>(`/ap/invoices/${id}/post`, { method: "POST", token });
}

// reverseAPInvoice membalik lewat jurnal pembalik — tagihannya tidak dihapus
// dan tidak diedit (ledger append-only).
export async function reverseAPInvoice(
  token: string,
  id: number,
  reverseDate?: string,
): Promise<APInvoice> {
  return apiFetch<APInvoice>(`/ap/invoices/${id}/reverse`, {
    method: "POST",
    token,
    body: { reverse_date: reverseDate ?? "" },
  });
}

// ── Pembayaran vendor ───────────────────────────────────────────────────────
//
// Enam endpoint, tidak lebih. Tidak ada endpoint uang muka, retensi, atau
// pelepasan retensi di sini — bukan karena lupa, melainkan karena backend
// menolaknya pada tahap ini, dan klien yang menyediakan pemanggilnya akan
// mengundang layar memanggil sesuatu yang pasti gagal.

export interface APAllocationBody {
  invoice_id: number;
  amount: string; // rupiah bulat, string
}

export interface APPaymentBody {
  vendor_id: number;
  // payment_kind: pada tahap ini selalu "invoice". Dikirim eksplisit supaya
  // penolakan backend terbaca sebagai penolakan aturan, bukan field yang lupa.
  payment_kind: "invoice";
  payment_date: string; // YYYY-MM-DD; kosong = hari ini menurut server
  amount: string;
  cash_account_code: string;
  // allocations KOSONG = mode otomatis (tagihan tertua dilunasi lebih dulu).
  // Terisi = mode eksplisit; Σ alokasi wajib sama dengan amount.
  allocations: APAllocationBody[];
  description: string;
}

export interface APPaymentFilter {
  vendor_id?: number;
  include_reversed?: boolean;
}

export async function fetchAPPayments(
  token: string,
  filter: APPaymentFilter = {},
): Promise<APPaymentView[]> {
  const q = new URLSearchParams();
  if (filter.vendor_id) q.set("vendor_id", String(filter.vendor_id));
  // Pembayaran yang dibalik tetap diminta: menyembunyikannya membuat daftar
  // menjawab "tidak pernah ada" untuk uang yang pernah keluar lalu dikoreksi.
  if (filter.include_reversed !== false) q.set("include_reversed", "true");
  const qs = q.toString();
  return apiFetch<APPaymentView[]>(`/ap/payments${qs ? `?${qs}` : ""}`, { token });
}

export async function fetchAPPayment(token: string, id: number): Promise<APPaymentView> {
  return apiFetch<APPaymentView>(`/ap/payments/${id}`, { token });
}

// fetchAPPaymentPrintHTML mengambil HTML cetak Bukti Kas Keluar (BKK) siap-A4
// untuk satu pembayaran vendor — representasi baca dari ap_payments, dokumen
// sudah ada (Payment.DocumentNumber), tidak ada apa pun yang ditulis di sini.
export async function fetchAPPaymentPrintHTML(token: string, id: number): Promise<string> {
  const res = await fetch(`${apiBase()}/ap/payments/${id}/print`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`Gagal memuat bukti kas keluar (${res.status})`);
  }
  return res.text();
}

// previewAPPayment: dry-run penuh. Backend mengunci, mengalokasikan, dan
// menyusun jurnalnya — lalu berhenti tepat sebelum menulis. Angka yang muncul
// di layar pratinjau adalah angka backend, bukan hitungan layar.
export async function previewAPPayment(
  token: string,
  body: APPaymentBody,
): Promise<APPaymentPreview> {
  return apiFetch<APPaymentPreview>("/ap/payments/preview", {
    method: "POST",
    token,
    body,
  });
}

// createAPPayment mencatat pembayaran DAN menerbitkan BKK dalam satu transaksi.
//
// `idempotencyKey` dikirim lewat header, bukan body: header ikut terbawa apa
// adanya saat permintaan diulang, sedangkan body dirakit ulang dan lebih mudah
// kehilangan kuncinya. Tanpa kunci ini, klik dobel pada jaringan lambat menjadi
// dua BKK untuk satu tagihan yang sama.
export async function createAPPayment(
  token: string,
  body: APPaymentBody,
  idempotencyKey: string,
): Promise<APPaymentResult> {
  return apiFetch<APPaymentResult>("/ap/payments", {
    method: "POST",
    token,
    body,
    headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
  });
}

// reverseAPPayment membalik lewat jurnal pembalik. Pembayaran aslinya tetap ada
// dan tetap terbaca — yang berubah hanya `reversed_at`-nya.
export async function reverseAPPayment(
  token: string,
  id: number,
  reverseDate?: string,
): Promise<APPaymentView> {
  return apiFetch<APPaymentView>(`/ap/payments/${id}/reverse`, {
    method: "POST",
    token,
    body: { reverse_date: reverseDate ?? "" },
  });
}

export async function fetchAPInvoicePayments(
  token: string,
  invoiceID: number,
): Promise<APInvoicePaymentRow[]> {
  return apiFetch<APInvoicePaymentRow[]>(`/ap/invoices/${invoiceID}/payments`, { token });
}
