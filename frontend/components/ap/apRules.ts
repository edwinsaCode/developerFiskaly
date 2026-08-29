// Aturan LAYAR untuk hutang usaha (W-11) — dipisah dari komponen React supaya
// bisa diuji sebagai fungsi murni tanpa merender apa pun.
//
// Batas yang dijaga file ini:
//
//   Yang ada di sini  : bentuk form, field mana wajib, kombinasi mana mustahil,
//                       label status, dan penjumlahan baris sebagai ALAT BANTU
//                       ISI (Σ baris ditampilkan supaya operator tahu ia sudah
//                       memasukkan semuanya).
//   Yang TIDAK di sini: pemilihan akun, nilai kewajiban vendor, perlakuan PPN,
//                       dan dampak RAB. Semua itu datang dari backend lewat
//                       /ap/invoices/preview dan ditampilkan apa adanya.
//
// Validasi di bawah adalah SARINGAN AWAL, bukan sumber kebenaran: setiap aturan
// di sini juga ditegakkan backend. Menyaring lebih dulu hanya menghemat satu
// perjalanan bolak-balik; yang menolak tetap server.

import type { APInvoiceStatus } from "@/lib/types/api";
import type { APInvoiceBody, APInvoiceLineBody, VendorBody } from "@/lib/api/ap";

// ── Taksonomi baris biaya ───────────────────────────────────────────────────
// Cermin domain.CostTier × domain.CostCategory di backend:
//   direct/shared → kategori kapitalisasi (land|hard|soft|financing)
//   overhead      → kategori beban (marketing|other)
// Kombinasi di luar matriks ditolak backend; layar tidak menawarkannya.

export const AP_PROJECT_CATEGORIES = [
  { value: "land", label: "Tanah" },
  { value: "hard", label: "Hard Cost (Konstruksi)" },
  { value: "soft", label: "Soft Cost (Perizinan, Desain)" },
  { value: "financing", label: "Biaya Pendanaan" },
] as const;

export const AP_OVERHEAD_CATEGORIES = [
  { value: "marketing", label: "Pemasaran" },
  { value: "other", label: "Umum & Administrasi" },
] as const;

export const AP_PROJECT_TIERS = [
  { value: "direct", label: "Langsung ke satu unit" },
  { value: "shared", label: "Biaya bersama proyek" },
] as const;

// Scope tagihan. `overhead` = tagihan perusahaan tanpa proyek (project_id 0 di
// backend); `proyek` = tagihan yang bermuara ke satu proyek.
export type APScope = "proyek" | "overhead";

// ── Status pengakuan ────────────────────────────────────────────────────────
// Ini status PENGAKUAN, bukan status pembayaran. Pembayaran hutang belum ada
// jalurnya, jadi tidak satu pun label di bawah menjanjikan "lunas"/"sisa".

export const AP_STATUS_LABEL: Record<APInvoiceStatus, string> = {
  draft: "Draft",
  posted: "Diakui",
  reversed: "Dibalik",
};

export function apStatusVariant(
  status: APInvoiceStatus,
): "default" | "success" | "danger" | "warning" | "accent" | "neutral" {
  switch (status) {
    case "posted":
      return "success";
    case "reversed":
      return "danger";
    default:
      return "warning";
  }
}

// apStatusHint menjelaskan apa yang sudah terjadi di buku pada status ini.
// Kalimat `posted` bercabang menurut scope: tagihan overhead menjadi beban
// periode berjalan, ia tidak menambah realisasi RAB dan tidak masuk HPP — jadi
// menyebut "realisasi RAB" di sana akan bertentangan dengan perilaku aslinya.
export function apStatusHint(status: APInvoiceStatus, scope: APScope): string {
  switch (status) {
    case "draft":
      return scope === "overhead"
        ? "Belum ada apa pun di buku: tidak ada saldo hutang dan belum ada beban yang terhitung."
        : "Belum ada apa pun di buku: tidak ada saldo hutang dan tidak ada realisasi RAB.";
    case "posted":
      return scope === "overhead"
        ? "Kewajiban sudah lahir di buku besar dan biayanya menjadi beban periode berjalan — tidak menambah realisasi RAB dan tidak masuk HPP."
        : "Kewajiban sudah lahir di buku besar dan biayanya terhitung sebagai realisasi RAB.";
    case "reversed":
      return "Dibalik lewat jurnal pembalik. Tagihan aslinya tidak dihapus — jejaknya tetap ada.";
  }
}

// ── Penjumlahan rupiah bulat ────────────────────────────────────────────────

// sumRupiah menjumlahkan nominal rupiah bulat memakai BigInt.
//
// BigInt, bukan Number: nominal properti rutin menembus miliar dan penjumlahan
// floating-point akan melenceng tanpa satu pun pesan error. Nilai yang bukan
// bilangan bulat diabaikan (baris yang belum diisi), karena fungsi ini hanya
// alat bantu isi — angka yang mengikat tetap yang dikembalikan backend.
// Ditulis `BigInt(0)`, bukan literal `0n`: target tsconfig proyek ini ES2017,
// dan literal BigInt baru sah sejak ES2020. Nilainya identik.
export function sumRupiah(values: string[]): string {
  let total = BigInt(0);
  for (const v of values) {
    const s = (v ?? "").trim();
    if (!/^\d+$/.test(s)) continue;
    total += BigInt(s);
  }
  return total.toString();
}

// ── Vendor ──────────────────────────────────────────────────────────────────

export interface VendorFormState {
  name: string;
  npwp: string;
  is_pkp: boolean;
  address: string;
  phone: string;
  email: string;
  bank_name: string;
  bank_account: string;
  note: string;
  is_active: boolean;
}

export function emptyVendorForm(): VendorFormState {
  return {
    name: "",
    npwp: "",
    is_pkp: false,
    address: "",
    phone: "",
    email: "",
    bank_name: "",
    bank_account: "",
    note: "",
    is_active: true,
  };
}

export function validateVendor(f: VendorFormState): Record<string, string> {
  const e: Record<string, string> = {};
  if (!f.name.trim()) e.name = "Nama vendor wajib diisi";
  // NPWP bukan syarat PKP menurut backend, tetapi vendor PKP tanpa NPWP hampir
  // selalu salah input — dan salah di sini berakhir sebagai PPN Masukan yang
  // tidak bisa dikreditkan.
  if (f.is_pkp && !f.npwp.trim()) e.npwp = "Vendor PKP wajib mencantumkan NPWP";
  if (f.email.trim() && !f.email.includes("@")) e.email = "Email tidak valid";
  return e;
}

export function buildVendorBody(f: VendorFormState, forUpdate = false): VendorBody {
  const body: VendorBody = {
    name: f.name.trim(),
    npwp: f.npwp.trim(),
    is_pkp: f.is_pkp,
    address: f.address.trim(),
    phone: f.phone.trim(),
    email: f.email.trim(),
    bank_name: f.bank_name.trim(),
    bank_account: f.bank_account.trim(),
    note: f.note.trim(),
  };
  // `is_active` hanya dikirim saat ubah. Saat buat, backend selalu menyalakan
  // vendor baru; mengirimnya di sana hanya menambah field yang tidak dibaca.
  if (forUpdate) body.is_active = f.is_active;
  return body;
}

// ── Tagihan ─────────────────────────────────────────────────────────────────

export interface InvoiceLineState {
  category: string;
  cost_tier: string;
  unit_id: string;
  budget_item_id: string;
  amount: string;
  description: string;
}

export interface InvoiceFormState {
  scope: APScope;
  vendor_id: string;
  project_id: string;
  invoice_number: string;
  invoice_date: string;
  due_date: string;
  ppn_amount: string;
  faktur_pajak_number: string;
  // dpp_amount OPSIONAL: angka yang tertulis di lembar tagihan vendor. Bila
  // diisi, backend menolak tagihan yang Σ barisnya berbeda (INV-AP-8) —
  // selisihnya tidak dibulatkan dan tidak dipilih salah satunya.
  dpp_amount: string;
  description: string;
  lines: InvoiceLineState[];
}

export function emptyInvoiceLine(scope: APScope): InvoiceLineState {
  return {
    category: "",
    cost_tier: scope === "overhead" ? "overhead" : "shared",
    unit_id: "",
    budget_item_id: "",
    amount: "",
    description: "",
  };
}

export function emptyInvoiceForm(today: string): InvoiceFormState {
  return {
    scope: "proyek",
    vendor_id: "",
    project_id: "",
    invoice_number: "",
    invoice_date: today,
    due_date: today,
    ppn_amount: "",
    faktur_pajak_number: "",
    dpp_amount: "",
    description: "",
    lines: [emptyInvoiceLine("proyek")],
  };
}

export interface InvoiceErrors {
  fields: Record<string, string>;
  // lines[i] → field → pesan
  lines: Record<number, Record<string, string>>;
}

export function hasInvoiceErrors(e: InvoiceErrors): boolean {
  return Object.keys(e.fields).length > 0 || Object.keys(e.lines).length > 0;
}

// validateInvoice menyaring kombinasi yang pasti ditolak backend.
//
// `vendorIsPKP` datang dari master vendor (data backend), bukan tebakan layar:
// PPN Masukan dari vendor non-PKP adalah klaim tanpa dasar dan ditolak server.
export function validateInvoice(
  f: InvoiceFormState,
  vendorIsPKP: boolean | undefined,
): InvoiceErrors {
  const fields: Record<string, string> = {};
  const lines: Record<number, Record<string, string>> = {};

  if (!f.vendor_id) fields.vendor_id = "Vendor wajib dipilih";
  if (!f.invoice_number.trim()) fields.invoice_number = "Nomor tagihan vendor wajib diisi";
  if (!f.invoice_date) fields.invoice_date = "Tanggal tagihan wajib diisi";
  if (!f.due_date) fields.due_date = "Tanggal jatuh tempo wajib diisi";
  if (f.invoice_date && f.due_date && f.due_date < f.invoice_date) {
    fields.due_date = "Jatuh tempo tidak boleh mendahului tanggal tagihan";
  }
  if (f.scope === "proyek" && !f.project_id) fields.project_id = "Proyek wajib dipilih";

  const ppn = (f.ppn_amount ?? "").trim();
  const hasPPN = /^\d+$/.test(ppn) && BigInt(ppn) > BigInt(0);
  if (hasPPN && vendorIsPKP === false) {
    fields.ppn_amount = "Vendor non-PKP tidak boleh menerbitkan PPN Masukan";
  }
  if (hasPPN && !f.faktur_pajak_number.trim()) {
    // PPN Masukan tanpa faktur tidak bisa dikreditkan — mencatatnya sebagai
    // aset berarti mengakui hak yang tidak dimiliki.
    fields.faktur_pajak_number = "Nomor faktur pajak wajib diisi bila ada PPN";
  }
  if (ppn !== "" && !/^\d+$/.test(ppn)) {
    fields.ppn_amount = "PPN harus rupiah bulat";
  }

  if (f.lines.length === 0) {
    fields.lines = "Tagihan harus punya minimal satu baris biaya";
  }
  f.lines.forEach((ln, i) => {
    const e: Record<string, string> = {};
    if (!ln.category) e.category = "Kategori wajib dipilih";
    if (!ln.cost_tier) e.cost_tier = "Jenis biaya wajib dipilih";
    if (!/^\d+$/.test(ln.amount.trim()) || BigInt(ln.amount.trim() || "0") <= BigInt(0)) {
      e.amount = "Nominal baris harus rupiah bulat lebih dari nol";
    }
    if (ln.cost_tier === "direct" && !ln.unit_id) {
      e.unit_id = "Biaya langsung harus menunjuk satu unit";
    }
    if (ln.cost_tier !== "direct" && ln.unit_id) {
      e.unit_id = "Hanya biaya langsung yang boleh menunjuk unit";
    }
    if (Object.keys(e).length > 0) lines[i] = e;
  });

  // DPP tertulis dicek di sini juga supaya operator melihat selisihnya sebelum
  // mengirim — tetapi yang MENOLAK tetap backend (INV-AP-8).
  const dpp = (f.dpp_amount ?? "").trim();
  if (dpp !== "") {
    if (!/^\d+$/.test(dpp)) {
      fields.dpp_amount = "DPP harus rupiah bulat";
    } else if (
      Object.keys(lines).length === 0 &&
      dpp !== sumRupiah(f.lines.map((l) => l.amount))
    ) {
      fields.dpp_amount = "DPP tertulis tidak sama dengan jumlah baris biaya";
    }
  }

  return { fields, lines };
}

// buildInvoiceBody menyusun body persis seperti DTO backend.
//
// Nominal dikirim sebagai STRING apa adanya — tidak pernah lewat Number, karena
// nilai di atas 2^53 dan pembulatan floating-point akan mengubah angka tanpa
// satu pun pesan error.
export function buildInvoiceBody(f: InvoiceFormState): APInvoiceBody {
  const lines: APInvoiceLineBody[] = f.lines.map((ln) => ({
    category: ln.category,
    cost_tier: ln.cost_tier,
    amount: ln.amount.trim(),
    description: ln.description.trim(),
    ...(ln.unit_id ? { unit_id: Number(ln.unit_id) } : {}),
    ...(ln.budget_item_id ? { budget_item_id: Number(ln.budget_item_id) } : {}),
  }));

  return {
    vendor_id: Number(f.vendor_id),
    project_id: f.scope === "proyek" && f.project_id ? Number(f.project_id) : 0,
    invoice_number: f.invoice_number.trim(),
    invoice_date: f.invoice_date,
    due_date: f.due_date,
    dpp_amount: f.dpp_amount.trim(),
    ppn_amount: f.ppn_amount.trim(),
    faktur_pajak_number: f.faktur_pajak_number.trim(),
    description: f.description.trim(),
    lines,
  };
}

// ── Hasil pemanggilan backend ───────────────────────────────────────────────

// ApResult membawa pesan error backend UTUH sampai ke layar.
//
// Server action yang melempar exception kehilangan pesannya di build produksi
// (Next.js menggantinya dengan teks generik). Untuk hutang usaha itu tidak bisa
// diterima: pesan seperti "Σ baris tidak sama dengan DPP" adalah satu-satunya
// petunjuk apa yang harus diperbaiki operator.
export type ApResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string; status?: number };

// errText menormalkan apa pun yang dilempar klien API menjadi satu kalimat.
export function errText(e: unknown): string {
  if (e && typeof e === "object") {
    const msg = (e as { message?: unknown }).message;
    if (typeof msg === "string" && msg.trim() !== "") return msg;
  }
  if (typeof e === "string" && e.trim() !== "") return e;
  return "Kesalahan server";
}

// errStatus mengambil status HTTP bila errornya berasal dari ApiError.
export function errStatus(e: unknown): number | undefined {
  if (e && typeof e === "object") {
    const s = (e as { status?: unknown }).status;
    if (typeof s === "number") return s;
  }
  return undefined;
}

// ── Penyaringan daftar (dilakukan di klien, tanpa perjalanan ke server) ─────

export interface VendorLike {
  name: string;
  npwp?: string;
  email?: string;
  phone?: string;
  is_active: boolean;
}

// filterVendors: pencarian bebas atas nama/NPWP/email + saringan status aktif.
export function filterVendors<T extends VendorLike>(
  list: T[],
  q: string,
  showInactive: boolean,
): T[] {
  const needle = q.trim().toLowerCase();
  return list.filter((v) => {
    if (!showInactive && !v.is_active) return false;
    if (!needle) return true;
    return (
      v.name.toLowerCase().includes(needle) ||
      (v.npwp ?? "").toLowerCase().includes(needle) ||
      (v.email ?? "").toLowerCase().includes(needle) ||
      (v.phone ?? "").toLowerCase().includes(needle)
    );
  });
}

export interface InvoiceLike {
  invoice_number: string;
  vendor_name: string;
  vendor_id: number;
  status: APInvoiceStatus;
  invoice_date: string;
  due_date: string;
  description: string;
}

export interface InvoiceListFilter {
  q?: string;
  vendorId?: string;
  status?: string;
  from?: string;
  to?: string;
}

// filterInvoices menyaring daftar yang SUDAH diambil dari backend. Ia tidak
// menghitung apa pun — hanya memilih baris mana yang tampil.
export function filterInvoices<T extends InvoiceLike>(
  list: T[],
  f: InvoiceListFilter,
): T[] {
  const needle = (f.q ?? "").trim().toLowerCase();
  return list.filter((inv) => {
    if (f.vendorId && String(inv.vendor_id) !== f.vendorId) return false;
    if (f.status && inv.status !== f.status) return false;
    // Saringan tanggal memakai TANGGAL TAGIHAN, bukan jatuh tempo: yang dicari
    // orang di daftar ini adalah "tagihan yang masuk bulan ini".
    if (f.from && inv.invoice_date.slice(0, 10) < f.from) return false;
    if (f.to && inv.invoice_date.slice(0, 10) > f.to) return false;
    if (!needle) return true;
    return (
      inv.invoice_number.toLowerCase().includes(needle) ||
      inv.vendor_name.toLowerCase().includes(needle) ||
      (inv.description ?? "").toLowerCase().includes(needle)
    );
  });
}
