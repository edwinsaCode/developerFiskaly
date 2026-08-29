import { test } from "node:test";
import assert from "node:assert/strict";
import { cookieLifetime, SESSION_COOKIE } from "../../lib/session-cookie.ts";

// Token palsu: middleware & helper ini sengaja TIDAK memverifikasi tanda tangan
// (backend yang melakukannya), jadi payload saja sudah cukup untuk diuji.
function tokenWithExp(exp: number | undefined): string {
  const payload = exp === undefined ? { uid: 1 } : { uid: 1, exp };
  const b64 = Buffer.from(JSON.stringify(payload)).toString("base64url");
  return `header.${b64}.signature`;
}

const DAY = 60 * 60 * 24;
const now = () => Math.floor(Date.now() / 1000);

test("umur cookie mengikuti klaim exp token, bukan angka tetap", () => {
  const secs = cookieLifetime(tokenWithExp(now() + 3600));
  // Toleransi 5 detik: waktu berjalan di antara pembuatan token dan pembacaan.
  assert.ok(Math.abs(secs - 3600) <= 5, `mau ~3600, dapat ${secs}`);
});

test("cookie tidak pernah hidup lebih lama dari tokennya", () => {
  // Inti bug W-12: cookie 7 hari di atas token 24 jam. Berapa pun exp-nya,
  // umur cookie tidak boleh melampauinya.
  for (const ttl of [60, 3600, DAY]) {
    const secs = cookieLifetime(tokenWithExp(now() + ttl));
    assert.ok(secs <= ttl + 5, `ttl ${ttl}: cookie ${secs} detik melampaui token`);
  }
});

test("token tanpa exp jatuh ke 24 jam, bukan 7 hari", () => {
  assert.equal(cookieLifetime(tokenWithExp(undefined)), DAY);
});

test("token rusak tidak membuat cookie abadi", () => {
  for (const bad of ["", "bukan-jwt", "a.b", "a.!!!.c"]) {
    assert.equal(cookieLifetime(bad), DAY, `token ${JSON.stringify(bad)}`);
  }
});

test("token yang sudah kedaluwarsa tidak menghasilkan umur negatif", () => {
  // maxAge negatif akan menghapus cookie seketika dan memantulkan pemakai
  // kembali ke /login tepat setelah login berhasil.
  const secs = cookieLifetime(tokenWithExp(now() - 3600));
  assert.ok(secs > 0, `mau positif, dapat ${secs}`);
});

test("nama cookie satu, dipakai bersama", () => {
  assert.equal(SESSION_COOKIE, "esa_session");
});

// ── Pesan error dari backend ──────────────────────────────────────────────
import { backendErrorMessage } from "../../lib/session-cookie.ts";

test("amplop JSON backend dikupas jadi kalimat untuk manusia", () => {
  assert.equal(
    backendErrorMessage('{"error":"email atau password salah"}', "Autentikasi gagal"),
    "email atau password salah",
  );
});

test("badan kosong jatuh ke pesan cadangan", () => {
  for (const body of ["", "   "]) {
    assert.equal(backendErrorMessage(body, "Autentikasi gagal"), "Autentikasi gagal");
  }
});

test("teks polos dipakai apa adanya", () => {
  assert.equal(backendErrorMessage("layanan sedang sibuk", "gagal"), "layanan sedang sibuk");
});

test("JSON tanpa field error tidak pernah bocor mentah ke layar", () => {
  for (const body of ['{"detail":"x"}', '{"error":""}', "{rusak"]) {
    const msg = backendErrorMessage(body, "Autentikasi gagal");
    assert.ok(!msg.startsWith("{"), `JSON mentah bocor: ${msg}`);
  }
});
