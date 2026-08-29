// Aturan LAYAR untuk pembayaran hutang vendor (W-11 Tahap 5) — dipisah dari
// komponen React supaya bisa diuji sebagai fungsi murni tanpa merender apa pun.
//
// Batas yang dijaga file ini, dan ini batas yang paling penting di seluruh
// increment:
//
//   Yang ada di sini  : bentuk form, field mana wajib, kombinasi mana mustahil,
//                       label status, penyaringan daftar, pembacaan pesan error.
//   Yang TIDAK di sini: sisa tagihan, status pembayaran, alokasi, nomor BKK,
//                       baris jurnal, dan saldo. Semuanya datang dari backend
//                       dan ditampilkan apa adanya.
//
// Sisa tagihan lahir dari sub-ledger alokasi di server (D-12). Kalau layar ikut
// menghitungnya — sekali pun untuk "optimistic update" yang kelihatan tidak
// berbahaya — akan ada dua definisi "sisa", dan yang salah tidak pernah
// memunculkan error. Karena itu tidak satu pun fungsi di bawah mengurangkan
// pembayaran dari tagihan.
//
// Penjumlahan Σ alokasi memang ada di sini, tetapi perannya SARINGAN AWAL: ia
// menahan form yang pasti ditolak backend supaya operator tidak menunggu satu
// perjalanan bolak-balik untuk mengetahuinya. Yang MENOLAK tetap server
// (ErrAllocationSumMismatch).

import type { APInvoiceView, APPaymentStatus, APPaymentView } from "@/lib/types/api";
import type { APAllocationBody, APPaymentBody } from "@/lib/api/ap";
// Relatif + berekstensi, bukan alias "@/…": ini satu-satunya impor NILAI di
// file ini, dan harness uji (`node --experimental-strip-types`) menyelesaikan
// specifier apa adanya tanpa mengenal path alias tsconfig. Menyalin ulang
// penjumlahan rupiah ke sini hanya untuk menyenangkan resolver akan melahirkan
// dua versi aritmetika uang — persis yang paling tidak boleh terjadi.
import { sumRupiah } from "./apRules.ts";

// ── Status pembayaran tagihan ───────────────────────────────────────────────

export const AP_PAYMENT_STATUS_LABEL: Record<APPaymentStatus, string> = {
  unrecognized: "Belum diakui",
  unpaid: "Belum dibayar",
  partial: "Dibayar sebagian",
  paid: "Lunas",
  reversed: "Dibalik",
};

export function apPaymentStatusVariant(
  s: APPaymentStatus,
): "default" | "success" | "danger" | "warning" | "accent" | "neutral" {
  switch (s) {
    case "paid":
      return "success";
    case "partial":
      return "accent";
    case "reversed":
      return "danger";
    case "unpaid":
      return "warning";
    default:
      return "neutral";
  }
}

// apPaymentStatusHint menjelaskan apa arti status itu di buku — bukan mengulang
// labelnya dengan kata lain.
export function apPaymentStatusHint(s: APPaymentStatus): string {
  switch (s) {
    case "unrecognized":
      return "Tagihan masih draft: kewajibannya belum lahir di buku, jadi belum ada yang bisa dibayar.";
    case "unpaid":
      return "Kewajiban sudah diakui dan belum menerima pembayaran apa pun.";
    case "partial":
      return "Sebagian sudah dibayar. Sisanya dihitung backend dari alokasi pembayaran, bukan dari kolom.";
    case "paid":
      return "Seluruh kewajiban tagihan ini sudah terbayar.";
    case "reversed":
      return "Tagihan sudah dibalik — kewajibannya tidak lagi berdiri.";
  }
}

// ── Tagihan yang bisa dibayar ───────────────────────────────────────────────

// eligibleForPayment menjawab boleh-tidaknya sebuah tagihan muncul di antrean
// bayar — TANPA aritmetika sama sekali.
//
// Keputusannya bersandar pada dua nilai yang keduanya dari server: status
// pengakuan dan status pembayaran turunan. Layar sengaja TIDAK memeriksa
// `outstanding > 0` sendiri: itu akan menjadi definisi kedua tentang "masih ada
// sisa", dan definisi kedua adalah cara termudah membuat daftar ini tidak
// sepakat dengan halaman tagihan.
export function eligibleForPayment(inv: APInvoiceView): boolean {
  return (
    inv.status === "posted" &&
    (inv.payment_status === "unpaid" || inv.payment_status === "partial")
  );
}

export function eligibleInvoicesOf(
  invoices: APInvoiceView[],
  vendorId: string,
): APInvoiceView[] {
  const vid = vendorId.trim();
  return invoices
    .filter((inv) => eligibleForPayment(inv))
    .filter((inv) => !vid || String(inv.vendor_id) === vid)
    // Jatuh tempo paling tua dulu — urutan yang sama dengan alokasi otomatis di
    // backend, supaya "otomatis" dan "pilih sendiri" tidak terlihat berbeda.
    .sort((a, b) => a.due_date.localeCompare(b.due_date));
}

// ── Form ────────────────────────────────────────────────────────────────────

// Mode alokasi. `auto` mengirim daftar alokasi KOSONG dan membiarkan backend
// menyusunnya tertua-dulu; `explicit` mengirim pasangan tagihan↔nominal.
// Keduanya melewati validasi dan penguncian yang sama di server — mode otomatis
// bukan jalur pintas.
export type AllocationMode = "auto" | "explicit";

export interface PaymentFormState {
  vendor_id: string;
  payment_date: string;
  cash_account_code: string;
  amount: string;
  mode: AllocationMode;
  // invoiceId (string) → nominal rupiah bulat (string). Hanya dibaca saat mode
  // `explicit`; isinya dipertahankan saat operator bolak-balik antar mode.
  allocations: Record<string, string>;
  description: string;
}

export function emptyPaymentForm(today: string, vendorId = ""): PaymentFormState {
  return {
    vendor_id: vendorId,
    payment_date: today,
    cash_account_code: "",
    amount: "",
    mode: "auto",
    allocations: {},
    description: "",
  };
}

// filledAllocations mengambil hanya baris yang benar-benar diisi nominal.
//
// Baris nol dibuang, tidak dikirim: nol dalam daftar eksplisit ditolak backend
// (ErrAllocationZero), dan baris yang tidak melakukan apa pun di dokumen
// pembayaran hampir selalu berarti operator salah mengisi.
export function filledAllocations(f: PaymentFormState): APAllocationBody[] {
  const out: APAllocationBody[] = [];
  for (const [id, raw] of Object.entries(f.allocations)) {
    const v = (raw ?? "").trim();
    if (!/^\d+$/.test(v) || BigInt(v) <= BigInt(0)) continue;
    out.push({ invoice_id: Number(id), amount: v });
  }
  return out.sort((a, b) => a.invoice_id - b.invoice_id);
}

// allocationTotal menjumlahkan alokasi terisi — ALAT BANTU ISI, bukan angka
// yang mengikat. Ia dipakai untuk memberi tahu operator bahwa jumlahnya belum
// pas sebelum ia menekan tombol.
export function allocationTotal(f: PaymentFormState): string {
  return sumRupiah(filledAllocations(f).map((a) => a.amount));
}

export interface PaymentErrors {
  fields: Record<string, string>;
  // invoiceId → pesan
  allocations: Record<string, string>;
}

export function hasPaymentErrors(e: PaymentErrors): boolean {
  return Object.keys(e.fields).length > 0 || Object.keys(e.allocations).length > 0;
}

// validatePayment menyaring kombinasi yang PASTI ditolak backend.
//
// Yang sengaja TIDAK diperiksa di sini: apakah sebuah alokasi melebihi sisa
// tagihannya. Backend menjawab kelebihan bayar dengan angkanya — sisa, diminta,
// dan selisih — dan jawaban itu jauh lebih berguna daripada tebakan layar atas
// data yang mungkin sudah berubah sejak halaman dimuat.
export function validatePayment(
  f: PaymentFormState,
  eligible: APInvoiceView[],
): PaymentErrors {
  const fields: Record<string, string> = {};
  const allocations: Record<string, string> = {};

  if (!f.vendor_id) fields.vendor_id = "Vendor wajib dipilih";
  if (!f.payment_date) fields.payment_date = "Tanggal pembayaran wajib diisi";
  if (!f.cash_account_code) fields.cash_account_code = "Sumber dana kas/bank wajib dipilih";

  const amt = (f.amount ?? "").trim();
  if (!/^\d+$/.test(amt) || BigInt(amt || "0") <= BigInt(0)) {
    fields.amount = "Jumlah bayar harus rupiah bulat lebih dari nol";
  }

  if (f.vendor_id && eligible.length === 0) {
    fields.vendor_id = "Vendor ini tidak punya tagihan terposting yang masih bersisa";
  }

  if (f.mode === "explicit") {
    const eligibleIds = new Set(eligible.map((inv) => String(inv.id)));
    for (const [id, raw] of Object.entries(f.allocations)) {
      const v = (raw ?? "").trim();
      if (v === "") continue;
      if (!/^\d+$/.test(v)) {
        allocations[id] = "Nominal alokasi harus rupiah bulat";
        continue;
      }
      // Tagihan yang tidak lagi ada di daftar layak bayar — mis. baru saja
      // lunas lewat layar lain — tidak boleh ikut terkirim diam-diam.
      if (!eligibleIds.has(id)) {
        allocations[id] = "Tagihan ini tidak lagi bisa dibayar; muat ulang halaman";
      }
    }

    const rows = filledAllocations(f);
    if (rows.length === 0) {
      fields.allocations = "Pilih minimal satu tagihan dan isi nominalnya";
    } else if (!fields.amount && allocationTotal(f) !== amt) {
      fields.allocations =
        "Jumlah alokasi harus sama persis dengan jumlah bayar. Sesuaikan salah satunya.";
    }
  }

  return { fields, allocations };
}

// buildPaymentBody menyusun body persis seperti DTO backend.
//
// Nominal dikirim sebagai STRING apa adanya — tidak pernah lewat Number, karena
// nilai properti rutin menembus 2^53 dan pembulatan floating-point akan
// mengubah angka tanpa satu pun pesan error.
export function buildPaymentBody(f: PaymentFormState): APPaymentBody {
  return {
    vendor_id: Number(f.vendor_id),
    payment_kind: "invoice",
    payment_date: f.payment_date,
    amount: (f.amount ?? "").trim(),
    cash_account_code: f.cash_account_code,
    allocations: f.mode === "explicit" ? filledAllocations(f) : [],
    description: (f.description ?? "").trim(),
  };
}

// ── Kunci idempotensi ───────────────────────────────────────────────────────

// newIdempotencyKey dibuat SEKALI per sesi form, bukan per klik.
//
// Itu inti gunanya: kalau kasir menekan Simpan dua kali karena jaringan lambat,
// permintaan kedua membawa kunci yang SAMA, dan backend mengembalikan
// pembayaran yang sama alih-alih menerbitkan BKK kedua.
//
// Tidak dipanggil saat render. `crypto.randomUUID()` menghasilkan nilai berbeda
// di server dan di browser, dan nilai acak yang ikut ter-render adalah cara
// paling langsung melahirkan hydration mismatch.
export function newIdempotencyKey(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  // Cadangan untuk lingkungan tanpa Web Crypto. Tidak sekuat UUID v4, tetapi
  // kunci idempotensi hanya perlu unik terhadap tenant ini, bukan kriptografis.
  return `ap-pay-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

// ── Kelebihan bayar ─────────────────────────────────────────────────────────

// Backend menjawab kelebihan bayar dengan ANGKANYA (D-18), bukan hanya kalimat.
// Bentuk ini membawa ketiganya sampai ke layar supaya koreksinya bisa ditawarkan
// tanpa menyuruh operator membuka tagihan satu per satu.
export interface OverpaymentInfo {
  message: string;
  outstanding: string;
  requested: string;
  excess: string;
  invoice_id?: number;
  invoice_number?: string;
}

// overpaymentFrom membaca payload error backend bila ia bertipe `overpayment`.
// Mengembalikan undefined untuk error jenis lain — pemanggil menampilkan
// pesannya apa adanya.
export function overpaymentFrom(payload: unknown): OverpaymentInfo | undefined {
  if (!payload || typeof payload !== "object") return undefined;
  const p = payload as Record<string, unknown>;
  if (p.error !== "overpayment") return undefined;
  const str = (k: string) => (typeof p[k] === "string" ? (p[k] as string) : "");
  return {
    message: str("message") || "Pembayaran melebihi sisa tagihan",
    outstanding: str("outstanding"),
    requested: str("requested"),
    excess: str("excess"),
    invoice_id: typeof p.invoice_id === "number" ? p.invoice_id : undefined,
    invoice_number: str("invoice_number") || undefined,
  };
}

// PayResult membawa pesan error backend UTUH sampai ke layar, plus angka
// kelebihan bayar bila ada.
//
// Server action yang melempar exception kehilangan pesannya di build produksi
// (Next.js menggantinya dengan teks generik). Untuk pembayaran itu tidak bisa
// diterima: "pembayaran melebihi sisa tagihan sebesar Rp X" adalah satu-satunya
// petunjuk berapa yang harus dikoreksi.
export type PayResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string; status?: number; overpayment?: OverpaymentInfo };

// ── Penyaringan daftar pembayaran ───────────────────────────────────────────

export interface PaymentListFilter {
  q?: string;
  vendorId?: string;
  state?: "" | "posted" | "reversed";
  from?: string;
  to?: string;
}

// filterPayments menyaring daftar yang SUDAH diambil dari backend. Ia tidak
// menghitung apa pun — hanya memilih baris mana yang tampil.
//
// Keadaan "dibalik" dibaca dari `is_reversed` milik server, bukan diturunkan
// dari ada-tidaknya `reversed_at`: yang menentukan boleh-tidaknya tombol
// Balikkan muncul harus punya satu sumber saja.
export function filterPayments<
  T extends Pick<
    APPaymentView,
    "document_number" | "vendor_name" | "vendor_id" | "description" | "payment_date" | "is_reversed"
  >,
>(list: T[], f: PaymentListFilter): T[] {
  const needle = (f.q ?? "").trim().toLowerCase();
  return list.filter((p) => {
    if (f.vendorId && String(p.vendor_id) !== f.vendorId) return false;
    if (f.state === "posted" && p.is_reversed) return false;
    if (f.state === "reversed" && !p.is_reversed) return false;
    if (f.from && p.payment_date.slice(0, 10) < f.from) return false;
    if (f.to && p.payment_date.slice(0, 10) > f.to) return false;
    if (!needle) return true;
    return (
      (p.document_number ?? "").toLowerCase().includes(needle) ||
      (p.vendor_name ?? "").toLowerCase().includes(needle) ||
      (p.description ?? "").toLowerCase().includes(needle)
    );
  });
}
