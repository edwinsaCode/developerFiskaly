# W-12 — Identity, RBAC Hardening & Authentication UX

Laporan penyelesaian. Tanggal: 2026-08-15.
Catatan: repositori ini bukan git repo, jadi pembedaan "baru" vs "diubah"
diambil dari isi berkas dan riwayat kerja, bukan dari diff. Berkas yang tidak
bisa dipastikan statusnya ditandai eksplisit.

---

## 1. Berkas baru

**Backend**

| Berkas | Isi |
|---|---|
| `backend/internal/platform/auth/scope.go` | Daftar-izin marketing di depan router (`auth.Scope`), `RequireSalesWrite`. |
| `backend/internal/platform/auth/scope_test.go` | 8 test batas role, termasuk "endpoint baru tertutup otomatis". |
| `backend/migrations/000074_users_name.up.sql` / `.down.sql` | Kolom `users.name`, komentar role diperbarui. |

**Frontend**

| Berkas | Isi |
|---|---|
| `frontend/lib/session-cookie.ts` | Satu aturan cookie sesi + pengupas amplop error backend. |
| `frontend/lib/roles.ts` | Kapabilitas role di sisi klien; `marketingMayOpenPage`, `homePathFor`. |
| `frontend/components/proyek/tabs.ts` | Definisi tab dipisah dari modul `"use client"` (lihat §14). |
| `frontend/components/auth/sessionCookie.test.ts` | 10 test umur cookie + pesan error. |
| `frontend/components/auth/roles.test.ts` | Test batas halaman per role. |
| `frontend/app/api/auth/session/route.ts` | Proxy `GET /auth/me` untuk identitas pemakai (status "baru" tidak dapat dipastikan). |

## 2. Berkas diubah

**Backend**

- `internal/domain/role.go` — `RoleMarketing` + kapabilitas eksplisit (`CanWrite`, `CanSell`, `SeesAccounting`, `CanManageUsers`, `Valid`).
- `cmd/api/main.go` — `auth.Scope("/api/v1")` dipasang sekali setelah `auth.Middleware`; rute jual memakai `RequireSalesWrite`.
- `internal/sale/handler.go`, `internal/crm/crm.go`, `internal/customer/handler.go` — guard tulis penjualan.
- `internal/tenant/{model,repository,service,handler,errors}.go` — `name` pada user, create+edit user, validasi, error tipis.
- `internal/tenant/service_test.go` — test identitas user.

**Frontend**

- `middleware.ts` — kedaluwarsa token, `?expired=1`, batas halaman marketing, pengecualian berkas statis.
- `lib/api/client.ts` — penanganan 401 terpusat + penjaga badai.
- `app/api/auth/login/route.ts`, `app/api/auth/register/route.ts` — cookie & pesan error lewat `lib/session-cookie.ts`.
- `lib/auth.ts`, `app/(app)/layout.tsx`, `components/layout/{Header,Sidebar,CommandPalette}.tsx` — identitas pemakai satu kali di layout, diturunkan sebagai prop.
- `app/login/page.tsx`, `components/auth/LoginForm.tsx` — tata letak split + show/hide kata sandi.
- `components/settings/UsersSection.tsx` — form nama/email/kata sandi/role untuk create dan edit.
- `tailwind.config.ts`, `components/ui/{Toast,Modal}.tsx` — skala lapisan bernama.
- `components/{accounting/JournalDetail,accounting/PeriodManager,billing/AllInvoiceList,billing/InvoiceList,booking/BookingBoard,penjualan/UnitSalePanel,proyek/WorkspaceNav}.tsx`, `app/(app)/proyek/[id]/page.tsx`, `app/(app)/penjualan/[unitId]/page.tsx`, `app/(app)/proyek/[id]/unit/[unitId]/page.tsx`, `lib/types/api.ts`, `lib/api/party.ts` — penyesuaian z-index bernama & penyembunyian aksi yang memang 403 untuk marketing.

## 3. Migrasi baru

`000074_users_name` — satu migrasi.

- `ALTER TABLE users ADD COLUMN name VARCHAR(200) NOT NULL DEFAULT '' AFTER email`
- komentar kolom `role` diperbarui menjadi `owner|accountant|marketing|viewer`.

Kompatibel mundur: 24 user lama tetap valid dengan `name = ''`. **Tidak ada satu
nama pun yang dikarang** — layar menampilkan email sebagai fallback sampai
pemiliknya mengisi sendiri. `down` sengaja TIDAK menurunkan role user marketing
yang sudah ada (mengubah hak akses orang secara diam-diam adalah keputusan
pemilik, bukan keputusan migrasi).

## 4. Perubahan autentikasi

1. **Email tetap kredensial login.** `name` murni identitas tampilan. Dibuktikan
   runtime: setelah email user diubah, login dengan email lama → 401, email baru
   + kata sandi lama → 200.
2. **Umur cookie mengikuti token.** Sebelumnya cookie 7 hari di atas JWT 24 jam —
   itulah "tiba-tiba semua halaman error". Sekarang `maxAge` dibaca dari klaim
   `exp` token (`lib/session-cookie.ts`).
3. **Dua jalan masuk disatukan.** `/api/auth/login` dan `/api/auth/register` kini
   memakai helper yang sama; sebelumnya register masih memasang cookie 7 hari.
4. **Pesan error tidak lagi bocor sebagai JSON.** Proxy dulu membungkus badan JSON
   backend ke dalam amplop kedua, sehingga layar menampilkan
   `{"error":"email atau password salah"}`. `backendErrorMessage` mengupasnya.
5. **Identitas pemakai satu sumber.** `GET /auth/me` diambil sekali di layout
   `(app)` dan diturunkan sebagai prop — tidak ada request berulang per komponen.

## 5. Matriks izin final

Penegakan ada di backend, dua lapis: `auth.Scope` (batas BACA, daftar-izin
marketing) di depan router, lalu `RequireWrite` / `RequireSalesWrite` /
`RequireRole("owner")` pada rute.

| Area | Pemilik | Accounting | Marketing | Viewer |
|---|---|---|---|---|
| Proyek / fase / unit / tipe produk (baca) | ✅ | ✅ | ✅ | ✅ |
| Prospek (leads): baca + tulis | ✅ | ✅ | ✅ | baca |
| Pelanggan: baca + tulis | ✅ | ✅ | ✅ | baca |
| Booking: baca, buat, batal | ✅ | ✅ | ✅ | baca |
| Kontrak penjualan + jadwal (buat/regenerate) | ✅ | ✅ | ✅ | baca |
| Statement kontrak (satu pembeli) | ✅ | ✅ | ✅ | ✅ |
| Master skema bayar / sumber dana / sales | ✅ | ✅ | baca | baca |
| `GET /ledger/accounts/cash-bank` (kode+nama, **tanpa saldo**) | ✅ | ✅ | ✅¹ | ✅ |
| COA penuh, jurnal, buku besar, neraca saldo | ✅ | ✅ | ❌ 403 | baca |
| Kas/bank & saldo, penerimaan, termin, cicilan | ✅ | ✅ | ❌ 403 | baca |
| Hutang usaha (vendor, invoice, pembayaran, reversal) | ✅ | ✅ | ❌ 403 | baca |
| Biaya, RAB, HPP, `sale-record` | ✅ | ✅ | ❌ 403 | baca |
| Laporan keuangan | ✅ | ✅ | ❌ 403 | baca |
| Kelola pengguna (`POST`/`PATCH /users`) | ✅ | ❌ 403 | ❌ 403 | ❌ 403 |
| Semua tulis akuntansi | ✅ | ✅ | ❌ | ❌ 403 |

¹ Satu pengecualian yang disengaja dan didokumentasikan di `scope.go`: form
booking harus memilih akun kas/bank penerima booking fee. `ledger.Account` tidak
punya field saldo sama sekali, jadi marketing tahu ada rekening BCA — tidak
pernah tahu isinya.

**Sifat daftar-izin:** apa pun yang tidak tertulis di `marketingAllow` dibalas
403, **termasuk endpoint yang belum ada saat baris itu ditulis**. Gagal-tertutup,
bukan gagal-terbuka. Ada test khusus untuk sifat ini
(`TestScope_EndpointBaruTertutupOtomatis`), dan `*` mencocokkan tepat satu
segmen — `/units/*` tidak pernah bisa meloloskan `/units/5/cost-entries`
(`TestScope_WildcardBukanPrefix`).

## 6. Perubahan untuk role Marketing

- Role `marketing` masuk `domain.Role` dengan kapabilitas eksplisit; role tak
  dikenal kini 403 (sebelumnya role kosong diam-diam dapat akses baca penuh).
- Batas baca ditegakkan sekali di router, bukan ~200 guard per-rute.
- Middleware Next mengantar marketing ke halaman yang memang bisa ia pakai
  alih-alih layar error — ini kenyamanan, bukan keamanan; otorisasi tetap di
  backend.
- Menu, tab, dan tombol yang pasti 403 disembunyikan agar tidak menjebak.

## 7. Perubahan session expiry

- Middleware Edge membaca `exp`; token mati → 307 `/login?expired=1` **dan**
  cookie dihapus (`Max-Age=0`), sehingga tidak ada pemakai yang berputar membawa
  token mati.
- `lib/api/client.ts`: 401 → bersihkan state → logout → `/login?expired=1` →
  pesan "Sesi Anda telah berakhir. Silakan login kembali."
- Anti-badai: flag modul `loggingOut` — 401 serentak hanya memicu satu logout.
- Tanpa putaran: `/login` sendiri publik; klien tidak me-redirect saat sudah di
  halaman login.
- Endpoint publik (`/api/auth`, `/api/v1`, `/health`) tidak memicu logout, dan
  `/api/v1/*` dibiarkan transparan supaya backend membalas 401 JSON, bukan HTML
  redirect.
- Berkas statis `/public` tidak lagi dilindungi sesi (dulu `/logo.png` dijawab
  307 ke `/login`, sehingga logo pecah tepat di halaman login).

## 8. Perubahan modal/toast

Akar masalah dicari lebih dulu, bukan menaikkan z-index acak:

1. Tidak ada skala lapisan — tiap komponen memakai angka sendiri. Diperbaiki
   dengan skala bernama di `tailwind.config.ts`: `header: 30`, `overlay: 50`,
   `toast: 100`, lalu semua komponen memakai nama itu.
2. Toast dirender di dalam pohon modal, sehingga z-index-nya hanya berlaku dalam
   stacking context modal. Diperbaiki dengan mem-portal toast ke `document.body`.

## 9. Perubahan login UI

Desktop split: kiri visual properti + overlay halus + tagline; kanan logo
esaProperti, heading "Masuk ke akun Anda", email, kata sandi, tombol Masuk.
Satu kolom di tablet/mobile. Tombol show/hide punya label aksesibel
("Tampilkan kata sandi"), dapat dicapai keyboard, dan hanya mengubah `type`
input — nilai kata sandi tidak disentuh. Logika autentikasi tidak diubah demi
redesign.

## 10. Hasil test

| Gate | Hasil |
|---|---|
| `npx tsc --noEmit --incremental false` | exit 0 |
| `npm test` (frontend) | **69 pass / 0 fail** (sebelum W-12: 59) |
| `NEXT_DIST_DIR=.next-build npx next build` | sukses |
| `go build ./...` | OK |
| `go vet ./...` | OK |
| `go test ./...` | seluruh paket `ok` |
| `go test -tags integration -p 1 ./... -count=1` | **seluruh 26 paket `ok`** |

Test W-11 tetap hijau — `internal/ap` lulus di suite integrasi (31,5 s), tidak
ada satu pun yang di-skip atau dilonggarkan.

Test RBAC marketing (`internal/platform/auth`): `TestScope_MarketingDiizinkan`,
`TestScope_MarketingDitolak`, `TestScope_WildcardBukanPrefix`,
`TestScope_MethodDicocokkan`, `TestScope_EndpointBaruTertutupOtomatis`,
`TestScope_RoleTidakDikenal403`, `TestScope_RoleLamaTidakBerubah`,
`TestScope_TanpaClaims401` — semua PASS.

## 11. Verifikasi runtime

Keduanya loopback saja: `LISTEN 127.0.0.1:3005` dan `LISTEN 127.0.0.1:8085`.

**A — Halaman login.** Split desktop tampil; show/hide berfungsi dan tidak
mengubah nilai; login benar → masuk aplikasi.

**B — Identitas pemakai.** Header menampilkan nama + email/role; user tanpa nama
jatuh ke email.

**C — Buat & ubah pengguna.** Dibuat "Staf Uji W12" / `w12-staf@dev.test` /
marketing lewat UI, lalu diubah menjadi "Staf Uji W12 Revisi" /
`w12-staf2@dev.test` / viewer dengan kata sandi dikosongkan. Dibuktikan dengan
curl: login email **baru** + kata sandi **lama** → 200 (PATCH parsial menjaga
kata sandi), email **lama** → 401.

**D — RBAC empat role.** Diuji di UI dan di endpoint.

| Uji (curl) | Hasil |
|---|---|
| marketing → `GET /projects`, `/customers`, `POST /bookings` | 200 (diizinkan) |
| marketing → `/ledger/accounts`, `/journal-entries`, `/ap/*`, `/payments`, reversal, `/cost-entries` | 403 |
| accountant / viewer → `POST /users`, `PATCH /users/31` | 403 |
| accountant / viewer → `GET /users` | 200 |
| accountant → `POST /cost-entries` | 400 (otorisasi lolos, validasi menolak) |
| viewer → `POST /cost-entries` | 403 (tulis diblokir) |

**E — Session expiry.** Edge: cookie kedaluwarsa → 307 `/login?expired=1` +
`set-cookie: esa_session=; Max-Age=0`; tanpa cookie → 307 `/login` tanpa flag;
`/login` sendiri → 200 (tidak ada putaran); `/api/v1/auth/me` → 401 JSON.
Runtime: backend di-restart dengan `JWT_SECRET` berbeda sehingga cookie yang
dipegang menjadi tidak sah — klik di dalam sesi menghasilkan 401 → logout
otomatis → `/login?expired=1` dengan pesan "Sesi Anda telah berakhir. Silakan
login kembali." Backend dikembalikan ke secret asli setelahnya.

**F — Notifikasi di atas modal.** Error validasi dipicu dari modal:

```json
{
  "layerZ": "100",
  "toastCoveredByAnything": false,
  "hitsAtToast": ["DIV pointer-events-auto…", "SPAN flex-1", "DIV pointer-events-auto…"],
  "modalStillOpen": true,
  "toastText": "email sudah terdaftar"
}
```

Hit-test di tiga sudut toast semuanya mengembalikan elemen toast, modal tetap
terbuka.

**Tambahan — pesan error login.** Kata sandi salah di browser: request
`/api/auth/login` → 401, badan `{"error":"email atau password salah"}` (satu
amplop), toast berbunyi `email atau password salah` tanpa kurung kurawal, dan
halaman tetap di `/login`.

## 12. Data test yang dibuat

Semuanya di tenant uji **9901008 "W12 Verify"** — **0 jurnal**, jadi tidak ada
satu pun transaksi akuntansi nyata yang dibuat.

| Objek | Nilai |
|---|---|
| Tenant | 9901008 "W12 Verify" |
| User 28 | `w12-owner@dev.test` — "Ratna Owner" (owner) |
| User 29 | `w12-accountant@dev.test` — "Dev accountant" |
| User 30 | `w12-marketing@dev.test` — "Dev marketing" |
| User 31 | `w12-viewer@dev.test` — "Sari Viewer" |
| User 35 | `w12-staf2@dev.test` — "Staf Uji W12 Revisi" (viewer) |
| Kata sandi | `w12devpass` (kecuali user 35, lihat §11-C) |
| Proyek 4116 | "Perumahan W12 Dev" |
| Unit 5410 / 5411 | W12-A1 / W12-A2 |
| Pelanggan 1195 | `CUST-W12-01` "Budi Verifikasi W12" |
| Lead 2 | nama panjang untuk uji batas |

Data ini **belum dihapus**. Tenant terisolasi dan tanpa jurnal, sedangkan
menghapus tidak bisa dibatalkan — jadi keputusan hapus diserahkan ke pemilik.
Perintah pembersihannya, bila diminta:

```sql
DELETE FROM leads WHERE tenant_id = 9901008;
DELETE FROM customers WHERE tenant_id = 9901008;
DELETE FROM units WHERE tenant_id = 9901008;
DELETE FROM projects WHERE tenant_id = 9901008;
DELETE FROM users WHERE tenant_id = 9901008;
DELETE FROM tenants WHERE id = 9901008;
```

## 13. Blocker

Tidak ada. Seluruh gate §7 hijau.

## 14. Temuan non-blocking

1. **Merek tidak konsisten** — shell aplikasi menampilkan "NATA ALAM /
   Akuntansi Developer Properti", halaman login menampilkan "esaProperti".
2. **Label role berbeda antar tempat** — header: "Accounting"/"Viewer";
   footer sidebar: "Akuntan"/"Pengamat".
3. **Tombol yang pasti 403** — non-owner masih melihat "+ Pengguna" dan "Ubah"
   di Pengaturan; gagal saat submit, bukan sebelumnya. Tidak berbahaya
   (backend menolak), tapi menjebak.
4. **`refresh()` menelan error diam-diam** di `UsersSection` — bila `GET /users`
   suatu saat 403, layar berbunyi "Belum ada pengguna lain", bukan pesan izin.
5. **Nama lead dipotong diam-diam** — `leads.name` adalah `VARCHAR(200)` tanpa
   validasi panjang di backend; masukan 600 karakter tersimpan menjadi 200
   karakter tanpa peringatan. (Koreksi atas catatan sementara sebelumnya: yang
   tersimpan 200, bukan 600.)
6. **Tab "Kinerja Sales"** memakai `/reports/sales-performance` yang 403 untuk
   marketing; perilaku layarnya untuk role itu belum diperiksa.
7. **Modul `"use client"` sebagai sumber nilai** — komponen server yang mengimpor
   nilai non-tipe dari modul klien menerima proxy, bukan nilainya. Ditemukan dan
   diperbaiki (`components/proyek/tabs.ts`); pola ini layak diwaspadai di tempat
   lain.
8. **Konfigurasi Tailwind basi di `next dev`** — perubahan `tailwind.config.ts`
   tidak dibaca oleh dev server yang sudah berjalan; CSS yang disajikan sempat
   tidak punya `.z-toast` sama sekali. `tsc` tidak bisa menangkap ini; hanya
   verifikasi runtime yang bisa.

## 15. Yang sengaja TIDAK diubah

Sesuai batas W-12 ("jangan menyentuh accounting engine"):

- Mesin akuntansi: jurnal, posting, pembalik, HPP, alokasi, budget,
  `actual_cost`, charge, `legacyar`, pajak, closing — nol perubahan.
- Logika AP Payment W-11, migrasi `000073`, locking R-12, invariant pembayaran.
- `RequireWrite` lama tetap di tempatnya; `auth.Scope` adalah lapisan tambahan
  di atasnya, bukan pengganti. Owner, accountant, dan viewer melewatinya tanpa
  perubahan perilaku sama sekali.
- Hash kata sandi, penerbitan JWT, dan TTL token (24 jam) tidak diubah — yang
  diperbaiki hanya umur cookie yang tidak sinkron dengannya.
- Tidak ada refactor besar di luar cakupan; tidak ada `AutoMigrate`; tidak ada
  perubahan di `internal/domain` selain `role.go`.
- Temuan non-blocking §14 dibiarkan apa adanya — memperbaikinya berarti melebar
  keluar cakupan W-12.
