// Uji aturan role di sisi layar (W-12).
//
// Jalankan: npm run test
//
// Yang diuji di sini HANYA keputusan tampilan: role apa melihat apa, dan
// halaman mana yang boleh dibuka marketing. Otorisasi sesungguhnya ada di
// backend (internal/platform/auth/scope.go + scope_test.go); kalau file ini
// ikut mengklaim "endpoint X aman", ia menjadi definisi kedua yang bisa
// berbohong. Yang dijaga di sini adalah satu hal: daftar halaman ini tidak
// boleh diam-diam melebar.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  ALL_ROLES,
  ROLE_LABELS,
  canManageUsers,
  canSell,
  canWrite,
  homePathFor,
  marketingMayOpenPage,
  seesAccounting,
} from "../../lib/roles.ts";

test("setiap role punya label Indonesia", () => {
  assert.deepEqual(ALL_ROLES, ["owner", "accountant", "marketing", "viewer"]);
  for (const r of ALL_ROLES) {
    assert.ok(ROLE_LABELS[r] && ROLE_LABELS[r].length > 0, `label kosong: ${r}`);
  }
  assert.equal(ROLE_LABELS.owner, "Pemilik");
  assert.equal(ROLE_LABELS.marketing, "Marketing");
});

test("marketing menjual, tidak menulis akuntansi, tidak kelola pengguna", () => {
  assert.equal(canSell("marketing"), true);
  assert.equal(canWrite("marketing"), false);
  assert.equal(seesAccounting("marketing"), false);
  assert.equal(canManageUsers("marketing"), false);
});

test("role lama tidak bergeser perilakunya", () => {
  assert.equal(canWrite("owner"), true);
  assert.equal(canWrite("accountant"), true);
  assert.equal(canWrite("viewer"), false);
  assert.equal(seesAccounting("owner"), true);
  assert.equal(seesAccounting("accountant"), true);
  assert.equal(seesAccounting("viewer"), true);
  assert.equal(canManageUsers("owner"), true);
  assert.equal(canManageUsers("accountant"), false);
});

test("beranda marketing bukan dashboard", () => {
  assert.equal(homePathFor("marketing"), "/penjualan");
  assert.equal(homePathFor("owner"), "/dashboard");
  assert.equal(homePathFor("accountant"), "/dashboard");
  assert.equal(homePathFor("viewer"), "/dashboard");
});

test("halaman penjualan & katalog terbuka untuk marketing", () => {
  for (const p of [
    "/proyek",
    "/proyek/7",
    "/proyek/7/unit/12",
    "/penjualan",
    "/penjualan/sales",
    "/penjualan/booking",
    "/penjualan/12", // detail unit + kontrak
    "/penjualan/12/", // trailing slash tidak mengubah keputusan
  ]) {
    assert.equal(marketingMayOpenPage(p), true, `seharusnya boleh: ${p}`);
  }
});

test("halaman uang tertutup untuk marketing", () => {
  for (const p of [
    "/dashboard",
    "/laporan",
    "/pajak",
    "/pengaturan",
    "/biaya",
    "/rab",
    "/proyek/7/rab",
    "/proyek/7/biaya",
    "/penjualan/komisi",
    "/penjualan/kpr",
    "/penjualan/pembatalan",
    "/penjualan/12/invoice",
    "/penjualan/12/tagihan",
    "/accounting/jurnal",
    "/accounting/coa",
    "/accounting/hutang",
    "/accounting/pembayaran-vendor",
    "/accounting/gl",
  ]) {
    assert.equal(marketingMayOpenPage(p), false, `seharusnya ditolak: ${p}`);
  }
});

test("halaman baru tertutup otomatis untuk marketing", () => {
  // Gagal-tertutup: rute yang belum ada saat baris ini ditulis tidak boleh
  // lolos hanya karena ia berada di bawah prefiks yang dikenal.
  assert.equal(marketingMayOpenPage("/fitur-baru"), false);
  assert.equal(marketingMayOpenPage("/proyek/7/laporan-keuangan"), false);
  assert.equal(marketingMayOpenPage("/penjualan/12/pembayaran"), false);
});

// ── Label role: satu sebutan untuk tiap peran ────────────────────────────────
import { BRAND_NAME, BRAND_PREFIX, BRAND_ACCENT, pageTitle } from "../../lib/brand.ts";

test("setiap role punya label, dan labelnya yang disepakati", () => {
  assert.deepEqual(ROLE_LABELS, {
    owner: "Pemilik",
    accountant: "Accounting",
    marketing: "Marketing",
    viewer: "Viewer",
  });
  for (const r of ALL_ROLES) {
    assert.ok(ROLE_LABELS[r], `role ${r} tidak punya label`);
  }
});

test("tidak ada dua role yang berbagi label", () => {
  const labels = ALL_ROLES.map((r) => ROLE_LABELS[r]);
  assert.equal(new Set(labels).size, labels.length);
});

// ── Merek: satu nama produk ──────────────────────────────────────────────────

test("merek yang dilihat pemakai adalah NATA ALAM RAYA", () => {
  assert.equal(BRAND_NAME, "NATA ALAM RAYA");
  // Dipecah untuk pewarnaan aksen — gabungannya harus tetap merek yang utuh.
  assert.equal(`${BRAND_PREFIX} ${BRAND_ACCENT}`, BRAND_NAME);
});

test("judul tab membawa nama merek, bukan nama teknis", () => {
  const t = pageTitle("Jurnal");
  assert.equal(t, "Jurnal — NATA ALAM RAYA");
  assert.ok(!/esaProperti/i.test(t));
});
