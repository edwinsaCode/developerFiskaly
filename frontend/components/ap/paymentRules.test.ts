// Uji aturan layar pembayaran hutang vendor (W-11, Tahap 5).
//
// Jalankan: npm run test  (Node test runner + type stripping, tanpa dependency
// baru dan tanpa satu pun data produksi — seluruh fixture dibuat di file ini).
//
// Yang diuji: kelayakan bayar, bentuk body yang dikirim, penyaringan alokasi,
// saringan awal validasi, pembacaan payload kelebihan bayar, dan penyaringan
// daftar. Yang SENGAJA tidak diuji di sini: nilai sisa tagihan, pembagian
// alokasi otomatis, dan komposisi jurnal — ketiganya milik backend
// (internal/ap/payment_test.go + payment_integration_test.go). Menegaskannya di
// sini akan melahirkan definisi kedua yang bisa menyimpang tanpa satu pun error.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  AP_PAYMENT_STATUS_LABEL,
  allocationTotal,
  apPaymentStatusHint,
  apPaymentStatusVariant,
  buildPaymentBody,
  eligibleForPayment,
  eligibleInvoicesOf,
  emptyPaymentForm,
  filledAllocations,
  filterPayments,
  hasPaymentErrors,
  newIdempotencyKey,
  overpaymentFrom,
  validatePayment,
  type PaymentFormState,
} from "./paymentRules.ts";
import type { APInvoiceView, APPaymentStatus } from "@/lib/types/api";

// ── Fixture ─────────────────────────────────────────────────────────────────

// Tagihan cukup dibuat sebatas field yang dibaca aturan layar. Sisanya diisi
// nilai netral: menyalin seluruh bentuk APInvoiceView ke tiap fixture hanya
// menambah tempat yang harus ikut diubah saat backend menambah kolom.
function inv(over: {
  id: number;
  vendor_id?: number;
  due_date?: string;
  status?: string;
  payment_status?: APPaymentStatus;
  outstanding?: string;
}): APInvoiceView {
  return {
    id: over.id,
    vendor_id: over.vendor_id ?? 1,
    invoice_number: `INV-${over.id}`,
    invoice_date: "2026-08-01",
    due_date: over.due_date ?? "2026-09-01",
    dpp_amount: "10000000",
    ppn_amount: "0",
    retention_amount: "0",
    advance_applied: "0",
    payable_amount: "10000000",
    status: (over.status ?? "posted") as APInvoiceView["status"],
    journal_entry_id: 1,
    description: "",
    created_by: 1,
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    vendor_name: `Vendor ${over.vendor_id ?? 1}`,
    vendor_is_pkp: false,
    paid_amount: "0",
    outstanding: over.outstanding ?? "10000000",
    payment_status: over.payment_status ?? "unpaid",
  };
}

function form(over: Partial<PaymentFormState> = {}): PaymentFormState {
  return { ...emptyPaymentForm("2026-08-15", "1"), ...over };
}

// ── Kelayakan bayar ─────────────────────────────────────────────────────────

test("hanya tagihan terposting yang masih bersisa yang bisa dibayar", () => {
  assert.equal(eligibleForPayment(inv({ id: 1, payment_status: "unpaid" })), true);
  assert.equal(eligibleForPayment(inv({ id: 2, payment_status: "partial" })), true);
  assert.equal(eligibleForPayment(inv({ id: 3, payment_status: "paid" })), false);
  // Draft: kewajibannya belum lahir, jadi belum ada yang bisa dilunasi.
  assert.equal(
    eligibleForPayment(inv({ id: 4, status: "draft", payment_status: "unrecognized" })),
    false,
  );
  assert.equal(
    eligibleForPayment(inv({ id: 5, status: "reversed", payment_status: "reversed" })),
    false,
  );
});

test("kelayakan dibaca dari status server, bukan dari angka sisa", () => {
  // Kasus mustahil-secara-akuntansi ini justru yang menjaga batasnya: kalau
  // suatu saat backend melaporkan `paid` dengan sisa bukan nol, layar TIDAK
  // boleh menawarkannya untuk dibayar hanya karena angkanya kelihatan tersisa.
  const aneh = inv({ id: 9, payment_status: "paid", outstanding: "5000000" });
  assert.equal(eligibleForPayment(aneh), false);
});

test("antrean bayar disaring per vendor dan diurut jatuh tempo tertua dulu", () => {
  const list = [
    inv({ id: 1, vendor_id: 1, due_date: "2026-09-10" }),
    inv({ id: 2, vendor_id: 2, due_date: "2026-08-01" }),
    inv({ id: 3, vendor_id: 1, due_date: "2026-08-20" }),
    inv({ id: 4, vendor_id: 1, payment_status: "paid" }),
  ];
  assert.deepEqual(
    eligibleInvoicesOf(list, "1").map((i) => i.id),
    [3, 1],
  );
  // Vendor kosong = seluruh vendor (dipakai halaman daftar untuk menghitung antrean).
  assert.deepEqual(
    eligibleInvoicesOf(list, "").map((i) => i.id),
    [2, 3, 1],
  );
});

// ── Alokasi ─────────────────────────────────────────────────────────────────

test("baris alokasi kosong dan nol dibuang, sisanya urut invoice_id", () => {
  const f = form({
    mode: "explicit",
    allocations: { "7": "1000", "3": "", "5": "0", "2": "2000" },
  });
  assert.deepEqual(filledAllocations(f), [
    { invoice_id: 2, amount: "2000" },
    { invoice_id: 7, amount: "1000" },
  ]);
});

test("total alokasi dijumlahkan sebagai bilangan bulat besar, bukan float", () => {
  const f = form({
    mode: "explicit",
    // Dua nilai yang jumlahnya melewati 2^53; lewat Number, hasilnya akan meleset.
    allocations: { "1": "9007199254740993", "2": "1" },
  });
  assert.equal(allocationTotal(f), "9007199254740994");
});

// ── Validasi ────────────────────────────────────────────────────────────────

test("field wajib disaring sebelum permintaan dikirim", () => {
  const e = validatePayment(
    { ...emptyPaymentForm("2026-08-15"), payment_date: "" },
    [],
  );
  assert.ok(e.fields.vendor_id);
  assert.ok(e.fields.payment_date);
  assert.ok(e.fields.cash_account_code);
  assert.ok(e.fields.amount);
  assert.equal(hasPaymentErrors(e), true);
});

test("nominal harus rupiah bulat lebih dari nol", () => {
  const base = form({ cash_account_code: "1-1200" });
  const eligible = [inv({ id: 1 })];
  for (const bad of ["0", "-5000", "1000.50", "seribu", ""]) {
    const e = validatePayment({ ...base, amount: bad }, eligible);
    assert.ok(e.fields.amount, `nominal "${bad}" seharusnya ditolak`);
  }
  const ok = validatePayment({ ...base, amount: "1000000" }, eligible);
  assert.equal(ok.fields.amount, undefined);
});

test("vendor tanpa tagihan bersisa ditahan dengan alasannya", () => {
  const e = validatePayment(form({ cash_account_code: "1-1200", amount: "500000" }), []);
  assert.match(e.fields.vendor_id ?? "", /tidak punya tagihan/i);
});

test("mode eksplisit: Σ alokasi wajib sama persis dengan jumlah bayar", () => {
  const eligible = [inv({ id: 1 }), inv({ id: 2 })];
  const base = form({ cash_account_code: "1-1200", mode: "explicit" });

  const kurang = validatePayment(
    { ...base, amount: "3000000", allocations: { "1": "1000000", "2": "1000000" } },
    eligible,
  );
  assert.match(kurang.fields.allocations ?? "", /sama persis/i);

  const pas = validatePayment(
    { ...base, amount: "2000000", allocations: { "1": "1000000", "2": "1000000" } },
    eligible,
  );
  assert.equal(pas.fields.allocations, undefined);
  assert.equal(hasPaymentErrors(pas), false);
});

test("mode eksplisit menolak tagihan yang tidak lagi layak bayar", () => {
  const e = validatePayment(
    form({
      cash_account_code: "1-1200",
      amount: "1000000",
      mode: "explicit",
      allocations: { "99": "1000000" },
    }),
    [inv({ id: 1 })],
  );
  assert.match(e.allocations["99"] ?? "", /tidak lagi bisa dibayar/i);
});

test("mode eksplisit tanpa satu pun baris terisi ditahan", () => {
  const e = validatePayment(
    form({
      cash_account_code: "1-1200",
      amount: "1000000",
      mode: "explicit",
      allocations: { "1": "" },
    }),
    [inv({ id: 1 })],
  );
  assert.match(e.fields.allocations ?? "", /minimal satu tagihan/i);
});

test("mode otomatis tidak menuntut alokasi apa pun", () => {
  const e = validatePayment(
    form({ cash_account_code: "1-1200", amount: "1000000" }),
    [inv({ id: 1 })],
  );
  assert.equal(hasPaymentErrors(e), false);
});

test("saringan layar TIDAK menebak kelebihan bayar — itu jawaban backend", () => {
  // Alokasi 99 juta ke tagihan bersisa 10 juta: pasti ditolak server, tetapi
  // layar meneruskannya supaya operator menerima angka sisa/diminta/selisih yang
  // sebenarnya, bukan tebakan atas data yang mungkin sudah berubah.
  const e = validatePayment(
    form({
      cash_account_code: "1-1200",
      amount: "99000000",
      mode: "explicit",
      allocations: { "1": "99000000" },
    }),
    [inv({ id: 1, outstanding: "10000000" })],
  );
  assert.equal(hasPaymentErrors(e), false);
});

// ── Body ────────────────────────────────────────────────────────────────────

test("mode otomatis mengirim daftar alokasi kosong", () => {
  const body = buildPaymentBody(
    form({ cash_account_code: "1-1200", amount: "5000000", description: " termin 2 " }),
  );
  assert.deepEqual(body, {
    vendor_id: 1,
    payment_kind: "invoice",
    payment_date: "2026-08-15",
    amount: "5000000",
    cash_account_code: "1-1200",
    allocations: [],
    description: "termin 2",
  });
});

test("mode eksplisit mengirim pasangan tagihan↔nominal apa adanya sebagai string", () => {
  const body = buildPaymentBody(
    form({
      cash_account_code: "1-1200",
      amount: "9007199254740993",
      mode: "explicit",
      allocations: { "4": "9007199254740993" },
    }),
  );
  assert.deepEqual(body.allocations, [
    { invoice_id: 4, amount: "9007199254740993" },
  ]);
  // Nominal tidak pernah melewati Number: nilainya harus tetap utuh digit demi digit.
  assert.equal(body.amount, "9007199254740993");
});

// ── Kunci idempotensi ───────────────────────────────────────────────────────

test("kunci idempotensi selalu berbeda antar sesi form", () => {
  const a = newIdempotencyKey();
  const b = newIdempotencyKey();
  assert.notEqual(a, b);
  assert.ok(a.length >= 16);
});

// ── Kelebihan bayar ─────────────────────────────────────────────────────────

test("payload kelebihan bayar dibaca beserta ketiga angkanya", () => {
  const over = overpaymentFrom({
    error: "overpayment",
    message: "pembayaran melebihi sisa tagihan",
    outstanding: "10000000",
    requested: "12000000",
    excess: "2000000",
    invoice_id: 7,
    invoice_number: "INV-7",
  });
  assert.equal(over?.excess, "2000000");
  assert.equal(over?.invoice_number, "INV-7");
  // `message` yang dipakai layar, bukan `error` — `error` hanya berbunyi "overpayment".
  assert.match(over?.message ?? "", /melebihi sisa tagihan/);
});

test("error jenis lain tidak dibaca sebagai kelebihan bayar", () => {
  assert.equal(overpaymentFrom({ error: "invoice_not_payable" }), undefined);
  assert.equal(overpaymentFrom(undefined), undefined);
  assert.equal(overpaymentFrom("overpayment"), undefined);
});

// ── Label status ────────────────────────────────────────────────────────────

test("setiap status pembayaran punya label, warna, dan penjelasan", () => {
  const all: APPaymentStatus[] = [
    "unrecognized",
    "unpaid",
    "partial",
    "paid",
    "reversed",
  ];
  for (const s of all) {
    assert.ok(AP_PAYMENT_STATUS_LABEL[s]);
    assert.ok(apPaymentStatusVariant(s));
    assert.ok(apPaymentStatusHint(s).length > 20, `penjelasan "${s}" terlalu pendek`);
  }
  assert.equal(apPaymentStatusVariant("paid"), "success");
  assert.equal(apPaymentStatusVariant("reversed"), "danger");
});

// ── Penyaringan daftar ──────────────────────────────────────────────────────

function pay(over: {
  id: number;
  document_number: string;
  vendor_id?: number;
  vendor_name?: string;
  description?: string;
  payment_date?: string;
  is_reversed?: boolean;
}) {
  return {
    document_number: over.document_number,
    vendor_id: over.vendor_id ?? 1,
    vendor_name: over.vendor_name ?? "PT Beton Jaya",
    description: over.description ?? "",
    payment_date: over.payment_date ?? "2026-08-10",
    is_reversed: over.is_reversed ?? false,
  };
}

test("daftar pembayaran disaring tanpa menghitung apa pun", () => {
  const list = [
    pay({ id: 1, document_number: "BKK/2026/000001", payment_date: "2026-08-01" }),
    pay({
      id: 2,
      document_number: "BKK/2026/000002",
      vendor_id: 2,
      vendor_name: "CV Kusen",
      payment_date: "2026-08-12",
      is_reversed: true,
    }),
    pay({
      id: 3,
      document_number: "BKK/2026/000003",
      description: "termin struktur",
      payment_date: "2026-08-20",
    }),
  ];

  assert.equal(filterPayments(list, {}).length, 3);
  assert.equal(filterPayments(list, { vendorId: "2" }).length, 1);
  assert.equal(filterPayments(list, { state: "reversed" }).length, 1);
  assert.equal(filterPayments(list, { state: "posted" }).length, 2);
  assert.equal(filterPayments(list, { from: "2026-08-10" }).length, 2);
  assert.equal(filterPayments(list, { to: "2026-08-12" }).length, 2);
  assert.equal(filterPayments(list, { q: "000002" }).length, 1);
  assert.equal(filterPayments(list, { q: "kusen" }).length, 1);
  assert.equal(filterPayments(list, { q: "struktur" }).length, 1);
});

test("keadaan dibalik dibaca dari is_reversed milik server", () => {
  // Baris dengan reversed_at terisi tetapi is_reversed false TIDAK boleh
  // diperlakukan sebagai dibalik: satu keadaan, satu sumber.
  const list = [
    { ...pay({ id: 1, document_number: "BKK/2026/000001" }), reversed_at: "2026-08-11" },
  ];
  assert.equal(filterPayments(list, { state: "posted" }).length, 1);
  assert.equal(filterPayments(list, { state: "reversed" }).length, 0);
});
