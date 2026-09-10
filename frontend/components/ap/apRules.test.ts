// Uji aturan layar hutang usaha (W-11, Tahap 3).
//
// Jalankan: npm run test  (Node test runner + type stripping, tanpa dependency
// baru dan tanpa satu pun data produksi — seluruh fixture dibuat di file ini).
//
// Yang diuji di sini adalah aturan LAYAR: field mana wajib, kombinasi mana
// mustahil, bentuk body yang dikirim, label status, penyaringan daftar, dan
// pembacaan pesan error. Aturan AKUNTANSI-nya diuji di backend
// (internal/ap/journal_test.go + ap_integration_test.go) — kalau file ini ikut
// menegaskan nilai kewajiban atau pemilihan akun, ia menjadi definisi kedua.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  AP_OVERHEAD_CATEGORIES,
  AP_PROJECT_CATEGORIES,
  AP_STATUS_LABEL,
  apStatusHint,
  apStatusVariant,
  buildInvoiceBody,
  buildVendorBody,
  emptyInvoiceForm,
  emptyInvoiceLine,
  emptyVendorForm,
  errStatus,
  errText,
  filterInvoices,
  filterVendors,
  hasInvoiceErrors,
  sumRupiah,
  validateInvoice,
  validateVendor,
  type InvoiceFormState,
  type VendorFormState,
} from "./apRules.ts";

// ── Fixture ─────────────────────────────────────────────────────────────────

function vendorRow(over: Partial<{
  id: number;
  name: string;
  npwp: string;
  email: string;
  phone: string;
  is_active: boolean;
  is_pkp: boolean;
}> = {}) {
  return {
    id: 1,
    name: "CV Karya Beton",
    npwp: "01.234.567.8-901.000",
    email: "tagihan@karyabeton.test",
    phone: "0812000111",
    is_active: true,
    is_pkp: true,
    ...over,
  };
}

function invoiceRow(over: Partial<{
  invoice_number: string;
  vendor_name: string;
  vendor_id: number;
  status: "draft" | "posted" | "reversed";
  invoice_date: string;
  due_date: string;
  description: string;
}> = {}) {
  return {
    invoice_number: "INV/2026/001",
    vendor_name: "CV Karya Beton",
    vendor_id: 1,
    status: "posted" as const,
    invoice_date: "2026-08-10",
    due_date: "2026-09-09",
    description: "Pengecoran blok A",
    ...over,
  };
}

function validInvoiceForm(over: Partial<InvoiceFormState> = {}): InvoiceFormState {
  return {
    ...emptyInvoiceForm("2026-08-14"),
    vendor_id: "1",
    project_id: "7",
    invoice_number: "INV/2026/001",
    invoice_date: "2026-08-10",
    due_date: "2026-09-09",
    description: "Pengecoran blok A",
    lines: [
      {
        category: "hard",
        cost_tier: "shared",
        hard_subcategory: "produksi_komersial",
        unit_id: "",
        budget_item_id: "",
        amount: "100000000",
        description: "Beton ready-mix",
      },
    ],
    ...over,
  };
}

// ── 0. RULE KLIEN FREEZE (2026-09-04): HPP hanya Tanah + Konstruksi/Hard ────
//    Cost. Soft Cost, Biaya Lain-lain, dan Operasional (dahulu "Pendanaan")
//    BUKAN HPP walaupun ada di RAB — kategori tagihan proyek (scope=proyek,
//    yang berujung ke tier direct/shared, kapitalisasi Persediaan) tidak
//    boleh pernah menawarkan kategori beban itu.

test("tagihan proyek: hanya kategori kapitalisasi HPP yang ditawarkan (land, hard)", () => {
  const values = AP_PROJECT_CATEGORIES.map((c) => c.value);
  assert.deepEqual(values, ["land", "hard"]);
  for (const nonHpp of ["soft", "operational", "marketing", "other"]) {
    assert.ok(
      !values.includes(nonHpp as (typeof values)[number]),
      `tagihan proyek tidak boleh menawarkan kategori beban "${nonHpp}"`,
    );
  }
});

test("tagihan overhead: Soft Cost dan Operasional wajib ada di sini, bukan di kategori proyek", () => {
  const values = AP_OVERHEAD_CATEGORIES.map((c) => c.value);
  assert.ok(values.includes("soft"), "soft harus ada di kategori overhead (beban, bukan HPP)");
  assert.ok(values.includes("operational"), "operational harus ada di kategori overhead (beban, bukan HPP)");
  // Kategori kapitalisasi tidak boleh nyasar ke overhead.
  for (const hpp of ["land", "hard"]) {
    assert.ok(
      !values.includes(hpp as (typeof values)[number]),
      `kategori overhead tidak boleh menawarkan kategori HPP "${hpp}"`,
    );
  }
});

// ── 1. Daftar vendor ────────────────────────────────────────────────────────

test("daftar vendor: pencarian mencakup nama, NPWP, dan email", () => {
  const list = [
    vendorRow({ id: 1, name: "CV Karya Beton" }),
    vendorRow({ id: 2, name: "PT Baja Utama", npwp: "99.888.777.6-555.000", email: "ar@baja.test" }),
  ];

  assert.deepEqual(filterVendors(list, "karya", false).map((v) => v.id), [1]);
  assert.deepEqual(filterVendors(list, "99.888", false).map((v) => v.id), [2]);
  assert.deepEqual(filterVendors(list, "ar@baja", false).map((v) => v.id), [2]);
  assert.equal(filterVendors(list, "", false).length, 2);
});

test("daftar vendor: yang nonaktif disembunyikan sampai diminta", () => {
  const list = [
    vendorRow({ id: 1 }),
    vendorRow({ id: 2, name: "PT Lama", is_active: false }),
  ];
  assert.deepEqual(filterVendors(list, "", false).map((v) => v.id), [1]);
  assert.deepEqual(filterVendors(list, "", true).map((v) => v.id), [1, 2]);
});

// ── 2. Validasi buat vendor ─────────────────────────────────────────────────

test("vendor: nama wajib diisi", () => {
  const e = validateVendor(emptyVendorForm());
  assert.equal(e.name, "Nama vendor wajib diisi");
});

test("vendor PKP tanpa NPWP ditolak sebelum dikirim", () => {
  const f: VendorFormState = { ...emptyVendorForm(), name: "PT Baja", is_pkp: true };
  const e = validateVendor(f);
  assert.equal(e.npwp, "Vendor PKP wajib mencantumkan NPWP");
  // Non-PKP tidak terkena aturan itu.
  assert.equal(validateVendor({ ...f, is_pkp: false }).npwp, undefined);
});

test("vendor: email tanpa @ ditolak; vendor sah lolos tanpa error", () => {
  const f: VendorFormState = { ...emptyVendorForm(), name: "PT Baja", email: "bukan-email" };
  assert.equal(validateVendor(f).email, "Email tidak valid");
  assert.deepEqual(validateVendor({ ...f, email: "ar@baja.test" }), {});
});

test("vendor: is_active hanya ikut terkirim saat mengubah", () => {
  const f: VendorFormState = { ...emptyVendorForm(), name: "  PT Baja  " };
  const create = buildVendorBody(f, false);
  assert.equal(create.name, "PT Baja"); // spasi dirapikan
  assert.equal("is_active" in create, false);
  assert.equal(buildVendorBody(f, true).is_active, true);
});

// ── 3. Daftar tagihan ───────────────────────────────────────────────────────

test("daftar tagihan: saringan vendor, status, dan rentang tanggal tagihan", () => {
  const list = [
    invoiceRow({ invoice_number: "A-1", vendor_id: 1, status: "draft", invoice_date: "2026-07-01" }),
    invoiceRow({ invoice_number: "A-2", vendor_id: 2, status: "posted", invoice_date: "2026-08-10" }),
    invoiceRow({ invoice_number: "A-3", vendor_id: 1, status: "posted", invoice_date: "2026-08-20" }),
  ];

  assert.deepEqual(
    filterInvoices(list, { vendorId: "1" }).map((i) => i.invoice_number),
    ["A-1", "A-3"],
  );
  assert.deepEqual(
    filterInvoices(list, { status: "posted" }).map((i) => i.invoice_number),
    ["A-2", "A-3"],
  );
  assert.deepEqual(
    filterInvoices(list, { from: "2026-08-01", to: "2026-08-15" }).map((i) => i.invoice_number),
    ["A-2"],
  );
  assert.deepEqual(
    filterInvoices(list, { q: "a-3" }).map((i) => i.invoice_number),
    ["A-3"],
  );
  assert.equal(filterInvoices(list, {}).length, 3);
});

test("daftar tagihan: saringan yang tidak cocok mengembalikan daftar kosong, bukan seluruhnya", () => {
  const list = [invoiceRow()];
  assert.equal(filterInvoices(list, { status: "reversed" }).length, 0);
});

// ── 4. Status ───────────────────────────────────────────────────────────────

test("status: label & warna mengikuti daur hidup PENGAKUAN", () => {
  assert.equal(AP_STATUS_LABEL.draft, "Draft");
  assert.equal(AP_STATUS_LABEL.posted, "Diakui");
  assert.equal(AP_STATUS_LABEL.reversed, "Dibalik");

  assert.equal(apStatusVariant("draft"), "warning");
  assert.equal(apStatusVariant("posted"), "success");
  assert.equal(apStatusVariant("reversed"), "danger");
});

test("status: penjelasan overhead tidak pernah menjanjikan realisasi RAB", () => {
  // Tagihan overhead menjadi beban periode berjalan — ia tidak menambah
  // realisasi RAB dan tidak masuk HPP. Menyebut "realisasi RAB" di jalur
  // overhead hanya sah dalam bentuk penyangkalan; klaim afirmatif seperti
  // "terhitung/terbaca sebagai realisasi RAB" bertentangan dengan jurnalnya.
  const klaimAfirmatif = ["terhitung sebagai realisasi rab", "terbaca sebagai realisasi rab"];
  for (const status of ["draft", "posted", "reversed"] as const) {
    const hint = apStatusHint(status, "overhead").toLowerCase();
    for (const klaim of klaimAfirmatif) {
      assert.ok(
        !hint.includes(klaim),
        `hint overhead status "${status}" menjanjikan realisasi RAB`,
      );
    }
  }

  assert.ok(apStatusHint("posted", "overhead").includes("beban periode berjalan"));
  assert.ok(apStatusHint("posted", "overhead").includes("tidak menambah realisasi RAB"));
  assert.ok(apStatusHint("posted", "proyek").includes("terhitung sebagai realisasi RAB"));

  // Pembalikan tidak bergantung scope — kalimatnya sama persis.
  assert.equal(
    apStatusHint("reversed", "overhead"),
    apStatusHint("reversed", "proyek"),
  );
});

test("status: tidak ada label yang menjanjikan pembayaran", () => {
  // Pelunasan hutang vendor belum ada jalurnya. Label seperti "Lunas"/"Sebagian"
  // akan menjanjikan angka yang tidak punya jurnal di belakangnya.
  const terlarang = ["lunas", "sebagian", "belum dibayar", "sisa"];
  for (const label of Object.values(AP_STATUS_LABEL)) {
    for (const kata of terlarang) {
      assert.ok(
        !label.toLowerCase().includes(kata),
        `label status "${label}" menyiratkan status pembayaran`,
      );
    }
  }
});

// ── 5. Penjumlahan rupiah ───────────────────────────────────────────────────

test("jumlah baris memakai bilangan bulat presisi penuh, bukan floating-point", () => {
  // 9.007.199.254.740.993 > 2^53: Number akan membulatkannya diam-diam.
  assert.equal(sumRupiah(["9007199254740992", "1"]), "9007199254740993");
  assert.equal(sumRupiah(["100000000", "250000000"]), "350000000");
  // Baris yang belum diisi diabaikan — fungsi ini hanya alat bantu isi.
  assert.equal(sumRupiah(["", "  ", "500"]), "500");
  assert.equal(sumRupiah([]), "0");
});

// ── 6. Validasi form tagihan ────────────────────────────────────────────────

test("tagihan: form kosong menolak dengan sebab yang jelas", () => {
  const e = validateInvoice(emptyInvoiceForm("2026-08-14"), undefined);
  assert.ok(hasInvoiceErrors(e));
  assert.equal(e.fields.vendor_id, "Vendor wajib dipilih");
  assert.equal(e.fields.invoice_number, "Nomor tagihan vendor wajib diisi");
  assert.equal(e.fields.project_id, "Proyek wajib dipilih");
  assert.ok(e.lines[0]?.category);
  assert.ok(e.lines[0]?.amount);
});

test("tagihan: form lengkap lolos saringan layar", () => {
  assert.equal(hasInvoiceErrors(validateInvoice(validInvoiceForm(), true)), false);
});

test("tagihan: jatuh tempo tidak boleh mendahului tanggal tagihan", () => {
  const e = validateInvoice(validInvoiceForm({ due_date: "2026-08-01" }), true);
  assert.equal(e.fields.due_date, "Jatuh tempo tidak boleh mendahului tanggal tagihan");
});

test("tagihan: PPN dari vendor non-PKP ditolak", () => {
  const f = validInvoiceForm({ ppn_amount: "11000000", faktur_pajak_number: "010.000-26.000001" });
  assert.equal(hasInvoiceErrors(validateInvoice(f, true)), false);
  const e = validateInvoice(f, false);
  assert.equal(e.fields.ppn_amount, "Vendor non-PKP tidak boleh menerbitkan PPN Masukan");
});

test("tagihan: PPN tanpa nomor faktur pajak ditolak", () => {
  const e = validateInvoice(validInvoiceForm({ ppn_amount: "11000000" }), true);
  assert.equal(
    e.fields.faktur_pajak_number,
    "Nomor faktur pajak wajib diisi bila ada PPN",
  );
});

test("tagihan: biaya langsung wajib menunjuk unit, biaya bersama justru tidak boleh", () => {
  const direct = validInvoiceForm({
    lines: [{ ...emptyInvoiceLine("proyek"), category: "hard", cost_tier: "direct", amount: "5000000" }],
  });
  assert.equal(validateInvoice(direct, true).lines[0].unit_id, "Biaya langsung harus menunjuk satu unit");

  const shared = validInvoiceForm({
    lines: [{ ...emptyInvoiceLine("proyek"), category: "hard", cost_tier: "shared", unit_id: "12", amount: "5000000" }],
  });
  assert.equal(
    validateInvoice(shared, true).lines[0].unit_id,
    "Hanya biaya langsung yang boleh menunjuk unit",
  );
});

test("tagihan: Hard Cost tanpa unit wajib memilih subkategori Produksi Subsidi/Komersial/Sarana/Perizinan", () => {
  const noSub = validInvoiceForm({
    lines: [{ ...emptyInvoiceLine("proyek"), category: "hard", cost_tier: "shared", amount: "5000000" }],
  });
  assert.equal(
    validateInvoice(noSub, true).lines[0].hard_subcategory,
    "Pilih subkategori — wajib diisi karena biaya ini tidak ditautkan ke unit",
  );

  const withSub = validInvoiceForm({
    lines: [
      {
        ...emptyInvoiceLine("proyek"),
        category: "hard",
        cost_tier: "shared",
        hard_subcategory: "produksi_subsidi",
        amount: "5000000",
      },
    ],
  });
  assert.equal(hasInvoiceErrors(validateInvoice(withSub, true)), false);

  // Biaya langsung ke satu unit sudah tahu Subsidi/Komersial-nya lewat unit
  // itu sendiri — subkategori tidak wajib.
  const direct = validInvoiceForm({
    lines: [
      { ...emptyInvoiceLine("proyek"), category: "hard", cost_tier: "direct", unit_id: "12", amount: "5000000" },
    ],
  });
  assert.equal(hasInvoiceErrors(validateInvoice(direct, true)), false);
});

test("tagihan: subkategori hanya berlaku untuk Hard Cost", () => {
  const f = validInvoiceForm({
    lines: [
      {
        ...emptyInvoiceLine("proyek"),
        category: "land",
        cost_tier: "shared",
        hard_subcategory: "produksi_subsidi",
        amount: "5000000",
      },
    ],
  });
  assert.equal(
    validateInvoice(f, true).lines[0].hard_subcategory,
    "Subkategori hanya berlaku untuk kategori Hard Cost",
  );
});

test("tagihan: DPP tertulis yang tidak sama dengan Σ baris ditandai sebelum dikirim", () => {
  const f = validInvoiceForm({ dpp_amount: "99000000" }); // Σ baris = 100.000.000
  assert.equal(
    validateInvoice(f, true).fields.dpp_amount,
    "DPP tertulis tidak sama dengan jumlah baris biaya",
  );
  // Angka yang cocok lolos.
  assert.equal(
    hasInvoiceErrors(validateInvoice(validInvoiceForm({ dpp_amount: "100000000" }), true)),
    false,
  );
});

test("tagihan overhead: proyek tidak wajib", () => {
  const f = validInvoiceForm({
    scope: "overhead",
    project_id: "",
    lines: [{ ...emptyInvoiceLine("overhead"), category: "other", amount: "5000000" }],
  });
  assert.equal(hasInvoiceErrors(validateInvoice(f, true)), false);
});

// ── 7. Body yang dikirim ────────────────────────────────────────────────────

test("body tagihan: nominal dikirim sebagai STRING apa adanya", () => {
  const body = buildInvoiceBody(
    validInvoiceForm({
      lines: [
        {
          category: "hard",
          cost_tier: "direct",
          hard_subcategory: "",
          unit_id: "12",
          budget_item_id: "34",
          amount: "9007199254740993",
          description: "Beton",
        },
      ],
    }),
  );

  assert.equal(typeof body.lines[0].amount, "string");
  // Nilai di atas 2^53 harus utuh — inilah alasan nominal tidak pernah lewat Number.
  assert.equal(body.lines[0].amount, "9007199254740993");
  assert.equal(body.lines[0].unit_id, 12);
  assert.equal(body.lines[0].budget_item_id, 34);
  assert.equal(body.vendor_id, 1);
  assert.equal(body.project_id, 7);
});

test("body tagihan overhead: project_id = 0 (tagihan tanpa proyek di backend)", () => {
  const body = buildInvoiceBody(
    validInvoiceForm({
      scope: "overhead",
      project_id: "",
      lines: [{ ...emptyInvoiceLine("overhead"), category: "other", amount: "5000000" }],
    }),
  );
  assert.equal(body.project_id, 0);
  assert.equal(body.lines[0].cost_tier, "overhead");
  assert.equal("unit_id" in body.lines[0], false);
});

test("body tagihan: tidak membawa field pembayaran, retensi, atau uang muka", () => {
  // Ketiganya belum punya jalur penyelesaian yang sah. Mengirimnya berarti
  // membentuk saldo yang tidak bisa dituntaskan.
  const body = buildInvoiceBody(validInvoiceForm()) as unknown as Record<string, unknown>;
  for (const field of ["retention_amount", "advance_applied", "paid_amount", "payment_method"]) {
    assert.equal(field in body, false, `body tagihan membawa field terlarang: ${field}`);
  }
});

// ── 8. Penanganan error backend ─────────────────────────────────────────────

test("error backend: pesan & status HTTP dibaca utuh dari ApiError", () => {
  const apiError = Object.assign(new Error("Σ baris tidak sama dengan DPP tagihan"), {
    name: "ApiError",
    status: 422,
  });
  assert.equal(errText(apiError), "Σ baris tidak sama dengan DPP tagihan");
  assert.equal(errStatus(apiError), 422);
});

test("error backend: bentuk yang tidak dikenali tetap menghasilkan satu kalimat", () => {
  assert.equal(errText(new Error("")), "Kesalahan server");
  assert.equal(errText(undefined), "Kesalahan server");
  assert.equal(errText({}), "Kesalahan server");
  assert.equal(errText("periode akuntansi sudah ditutup"), "periode akuntansi sudah ditutup");
  assert.equal(errStatus(new Error("x")), undefined);
});
