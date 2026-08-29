import test from "node:test";
import assert from "node:assert/strict";
import {
  normalize,
  matchesQuery,
  pageCount,
  clampPage,
  pageSlice,
  rangeLabel,
} from "./table-view.ts";

test("normalize: rapikan huruf besar dan spasi", () => {
  assert.equal(normalize("  Budi   Santoso "), "budi santoso");
  assert.equal(normalize(null), "");
  assert.equal(normalize(undefined), "");
  assert.equal(normalize(42), "42");
});

test("matchesQuery: kueri kosong meloloskan semua baris", () => {
  assert.equal(matchesQuery(["C-01"], ""), true);
  assert.equal(matchesQuery(["C-01"], "   "), true);
});

test("matchesQuery: setiap kata boleh datang dari kolom berbeda", () => {
  const row = ["C-01", "Budi Santoso", "ppjb"];
  assert.equal(matchesQuery(row, "budi c-01"), true);
  assert.equal(matchesQuery(row, "c-01 budi"), true);
  assert.equal(matchesQuery(row, "budi siti"), false);
});

test("matchesQuery: cocok sebagian kata dan abai huruf besar", () => {
  assert.equal(matchesQuery(["Agus Kuncoro"], "kunc"), true);
  assert.equal(matchesQuery(["Agus Kuncoro"], "AGUS"), true);
});

test("matchesQuery: kolom kosong tidak bikin cocok palsu", () => {
  assert.equal(matchesQuery([null, undefined, ""], "budi"), false);
});

test("pageCount: nol baris tetap satu halaman", () => {
  assert.equal(pageCount(0, 20), 1);
  assert.equal(pageCount(20, 20), 1);
  assert.equal(pageCount(21, 20), 2);
  assert.equal(pageCount(137, 20), 7);
});

test("clampPage: halaman di luar rentang ditarik ke halaman terakhir", () => {
  // Kasus nyata: pemakai di halaman 5, lalu mengetik kata yang menyisakan
  // 3 baris. Tanpa penjepitan, ia melihat tabel kosong.
  assert.equal(clampPage(5, 3, 20), 1);
  assert.equal(clampPage(9, 137, 20), 7);
  assert.equal(clampPage(0, 137, 20), 1);
  assert.equal(clampPage(-3, 137, 20), 1);
  assert.equal(clampPage(NaN, 137, 20), 1);
});

test("pageSlice: potongan sesuai halaman, halaman mulai dari 1", () => {
  const rows = Array.from({ length: 25 }, (_, i) => i + 1);
  assert.deepEqual(pageSlice(rows, 1, 10), [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
  assert.deepEqual(pageSlice(rows, 3, 10), [21, 22, 23, 24, 25]);
  // Halaman kelewat jauh tetap memberi baris, bukan array kosong.
  assert.deepEqual(pageSlice(rows, 99, 10), [21, 22, 23, 24, 25]);
});

test("pageSlice: daftar kosong menghasilkan potongan kosong tanpa error", () => {
  assert.deepEqual(pageSlice([], 1, 20), []);
});

test("rangeLabel: angka yang ditampilkan sesuai yang benar-benar tampil", () => {
  assert.equal(rangeLabel(1, 20, 137), "Menampilkan 1–20 dari 137");
  assert.equal(rangeLabel(7, 20, 137), "Menampilkan 121–137 dari 137");
  assert.equal(rangeLabel(1, 20, 12), "Menampilkan 1–12 dari 12");
  assert.equal(rangeLabel(5, 20, 12), "Menampilkan 1–12 dari 12");
  assert.equal(rangeLabel(1, 20, 0), "Tidak ada baris");
});
