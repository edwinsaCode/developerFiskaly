// Uji aturan tampil-error field rupiah (F-5).
//
// Jalankan: npm run test  (Node 22 test runner + type stripping, tanpa
// dependency baru).

import { test } from "node:test";
import assert from "node:assert/strict";

import { shouldShowRupiahError, validateRupiah } from "./rupiah.ts";

// 1. Nominal kosong → error terlihat pada form yang memvalidasi saat submit
//    (ExpenseForm: `showErrorWhenEmpty`).
test("nominal kosong: error submit terlihat", () => {
  const err = validateRupiah("");
  assert.equal(err, "Jumlah harus diisi");
  assert.equal(shouldShowRupiahError(err ?? undefined, "", true), true);
});

// 2. Nominal valid → tidak ada error sama sekali.
test("nominal valid: tidak ada error", () => {
  assert.equal(validateRupiah("1500000"), null);
  assert.equal(shouldShowRupiahError(undefined, "1500000", true), false);
  assert.equal(shouldShowRupiahError(undefined, "1500000"), false);
});

// 3. Nominal "0" → aturan lama tidak berubah: tetap tidak sah, dan pesannya
//    sudah terlihat baik dengan maupun tanpa flag (nilainya bukan string kosong).
test('nominal "0": aturan lama tidak berubah', () => {
  const err = validateRupiah("0");
  assert.equal(err, "Jumlah harus diisi");
  assert.equal(shouldShowRupiahError(err ?? undefined, "0"), true);
  assert.equal(shouldShowRupiahError(err ?? undefined, "0", true), true);
});

// 4. Form lain (tanpa flag) harus berperilaku PERSIS seperti sebelum perbaikan.
//    Perilaku lama: `showError = error && value !== ""`.
test("tanpa flag: identik dengan perilaku sebelum perbaikan", () => {
  const errors = [undefined, "Jumlah harus diisi", "Melebihi sisa tagihan"];
  const values = ["", "0", "1500000", "250"];
  for (const error of errors) {
    for (const value of values) {
      const lama = Boolean(error) && value !== "";
      assert.equal(
        shouldShowRupiahError(error, value),
        lama,
        `default berubah untuk (error=${String(error)}, value="${value}")`,
      );
      assert.equal(
        shouldShowRupiahError(error, value, false),
        lama,
        `flag=false berubah untuk (error=${String(error)}, value="${value}")`,
      );
    }
  }
});

// Flag hanya boleh menambah satu kasus: error + nilai kosong. Selebihnya sama.
test("flag hanya mengubah kasus error + nilai kosong", () => {
  assert.equal(shouldShowRupiahError(undefined, "", true), false); // tanpa error tetap diam
  assert.equal(shouldShowRupiahError("Jumlah harus diisi", "", false), false);
  assert.equal(shouldShowRupiahError("Jumlah harus diisi", "", true), true);
});

// Aturan validasi lainnya ikut terkunci supaya tidak ada yang bergeser diam-diam.
test("aturan validateRupiah lainnya tidak bergeser", () => {
  assert.equal(validateRupiah("abc"), "Jumlah tidak valid");
  assert.equal(validateRupiah("1.5"), "Jumlah harus rupiah bulat (tanpa sen)");
  assert.equal(validateRupiah("-5"), "Jumlah harus lebih dari Rp 0");
});
