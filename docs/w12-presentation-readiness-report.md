# W-12 — Presentation Readiness / Final Polish

Tanggal: 2026-08-15
Lingkup: polish siap-demo di atas W-12 yang sudah selesai. Tidak ada implementasi
ulang W-12, tidak ada refactor besar, tidak ada sentuhan ke accounting engine, AP
Payment, migration 000073, R-12 locking, journal, budget, actual_cost, charge,
legacyar, maupun tax **rules** (satu perubahan di modul pajak murni otorisasi
route — lihat bagian 6).

**STATUS AKHIR: PASS — READY FOR CLIENT PRESENTATION**

---

## 1. Session cookie register — SELESAI

Register memasang cookie lewat helper yang sama dengan login.

`frontend/lib/session-cookie.ts` menjadi satu-satunya tempat umur cookie
ditentukan: `cookieLifetime(token)` membaca klaim `exp` dari JWT itu sendiri dan
memakai sisa umurnya sebagai `maxAge`. Dua jalan masuk memakainya:

- `app/api/auth/login/route.ts:31` → `res.cookies.set(SESSION_COOKIE, token, sessionCookieOptions(token))`
- `app/api/auth/register/route.ts` → baris yang identik

Konsekuensinya: **tidak ada cookie auth yang hidup lebih lama daripada JWT-nya.**
Lifetime JWT backend tidak disentuh sama sekali — yang dipendekkan adalah
cookie, bukan token diperpanjang. Cookie lama dipatok 7 hari sementara JWT 24
jam; selisih itulah penyebab gejala "tiba-tiba semua halaman error" (cookie masih
ada sehingga tidak ada yang menganggap sesi berakhir, tapi setiap panggilan API
dijawab 401).

Tidak ada migration baru yang dibutuhkan untuk perbaikan ini.

Verifikasi runtime: register tenant baru (tenant **9901009**, user **36**,
`w12-cookie@dev.test`) → langsung masuk aplikasi tanpa login ulang, cookie
`esa_session` terpasang dengan `Max-Age` mengikuti `exp` token.

## 2. `/logo.png` — SELESAI, 200

```
$ curl -s -o /dev/null -D - http://127.0.0.1:3005/logo.png | head -3
HTTP/1.1 200 OK
Content-Type: image/png
Content-Length: 25140
```

Kontrol negatif — halaman terlindungi tetap terlindungi tanpa cookie:

```
$ curl -s -o /dev/null -D - http://127.0.0.1:3005/dashboard
HTTP/1.1 307 Temporary Redirect
location: http://localhost:3005/login
```

Jalannya lewat regex `STATIC_FILE` di `frontend/middleware.ts`, yang membebaskan
aset `public/` — bukan dengan membuka semua static asset secara sembarangan, dan
bukan dengan mengauthentikasi aset. Di browser logo tampil pada login, register,
dan sidebar; network log mencatat `/logo.png` → 200/304 di setiap halaman.

## 3. Branding — SERAGAM: NATA ALAM RAYA

Merek yang dilihat pemakai berasal dari satu berkas, `frontend/lib/brand.ts`
(`BRAND_NAME`, `BRAND_PREFIX`, `BRAND_ACCENT`, `BRAND_TAGLINE`, `pageTitle()`).
Tidak ada merek baru yang dikarang; nama diambil dari koreksi klien.

Grep akhir atas seluruh teks yang dilihat pemakai (`app/`, `components/`,
`lib/`, ekstensi `.ts`/`.tsx`) menyisakan **tiga** kemunculan `esaProperti`, dan
ketiganya adalah komentar kode, bukan teks layar:

- `app/api/auth/session/route.ts:11` — komentar menjelaskan bentuk klaim token
- `lib/brand.ts:5` dan `lib/brand.ts:8` — komentar yang justru menjelaskan
  pembagian ini

`esaproperti` tetap dipakai sebagai identifier teknis: module Go, nama package,
route, database, config, nama repo. **Tidak ada rename teknis** yang dilakukan —
tidak ada perubahan route, folder, package, database, API path, import,
identifier internal, migrasi, maupun nama repo.

Judul tab dikunci oleh test: `pageTitle("Jurnal")` → `"Jurnal — NATA ALAM RAYA"`,
dengan assert eksplisit bahwa `esaProperti` tidak muncul di judul.

Catatan jujur (tidak memblokir): berkas gambar `public/logo.png` bertuliskan
"NATA ALAM" tanpa kata "RAYA". Tidak ada aset logo resmi ber-"RAYA" di
repositori dan saya tidak membuat-buat aset baru. Semua teks merek sudah
"NATA ALAM RAYA"; yang tertinggal hanya artwork. Bila klien punya file logo
resmi, cukup menimpa `frontend/public/logo.png` — tidak ada kode yang berubah.

## 4. Label role — SERAGAM

Satu sumber label: `frontend/lib/roles.ts` → `ROLE_LABELS`.

| identifier internal | label yang dilihat pemakai |
|---|---|
| `owner` | **Pemilik** |
| `accountant` | **Accounting** |
| `marketing` | **Marketing** |
| `viewer` | **Viewer** |

Identifier internal **tidak diubah** sama sekali, dan perilaku otorisasi tidak
bergeser: `canWrite`, `canSell`, `seesAccounting`, `canManageUsers`,
`marketingMayOpenPage`, `homePathFor` semuanya memberi jawaban yang sama seperti
sebelum task ini. Dikunci oleh `components/auth/roles.test.ts`: peta label persis
seperti tabel di atas, tidak ada dua role berbagi label, dan setiap role punya
label yang tidak kosong.

## 5. Aksi UI yang pasti 403 — DISEMBUNYIKAN

Backend tetap satu-satunya batas keamanan. Yang diperbaiki adalah janji layar:
tombol yang pasti berakhir 403 tidak lagi ditawarkan, karena pemakai baru tahu
setelah mengisi seluruh formulir. **Tidak ada aturan otorisasi backend yang
diubah** untuk poin ini, dan tidak ada tombol yang dihapus secara global.

Alatnya baru satu berkas: `frontend/lib/hooks/usePermissions.ts` —
`useMayWrite()`, `useMaySell()`, `useMayManageUsers()`, masing-masing membungkus
`useUserSafe()` + predikat yang sudah ada di `lib/roles.ts`. Hook dipakai supaya
pasangan itu tidak disalin belasan kali; salinan seperti itulah yang dulu membuat
satu peran punya dua perlakuan berbeda di dua layar.

16 kontrol di 10 komponen:

| Komponen | Kontrol yang disembunyikan untuk peran baca |
|---|---|
| `settings/SalesSection.tsx` | `+ Tim`, `+ Sales`, CTA `+ Sales Pertama` |
| `settings/SchemesSection.tsx` | `+ Skema`, `+ Skema Pertama`, `+ Bank`, `+ Bank Pertama` |
| `settings/CustomersSection.tsx` | `+ Pelanggan` + CTA empty state |
| `settings/ProductTypesSection.tsx` | `+ Produk`, aksi baris `Ubah`/`Nonaktifkan` |
| `settings/RealizationChargeTypesSection.tsx` | `+ Jenis Biaya` (header + empty state), aksi baris |
| `settings/DocumentNumberingSection.tsx` | `+ Jenis Dokumen` (header + empty state), aksi baris |
| `settings/UsersSection.tsx` | `+ Pengguna`, `Ubah` (sudah sejak sesi sebelumnya, `canManageUsers`) |
| `commission/CommissionBoard.tsx` | `Hitung Komisi`, CTA `+ Buat Aturan`, batch action bar, toggle aturan aktif, seluruh form "Aturan Baru" |
| `proyek/workspace/ProjectControlCard.tsx` | `+ Update Fisik` |
| `pajak/TaxObligationTable.tsx` | `+ Akrual Baru`, `Bayar` per baris |
| `sales/SalesWorkspace.tsx` | `+ Lead Baru` (header + empty state), aksi lead (`Dihubungi`, `Serius`, `→ Customer`, `batal`) — pakai `useMaySell` |

Yang dipegang: Marketing tidak melihat aksi accounting/payment/kas/jurnal/COA/AP
(halamannya sendiri memang di luar jangkauan — lihat bagian 9-D); Viewer tidak
melihat aksi tulis; Accounting dan Pemilik **tidak berubah sama sekali**, diperiksa
langsung di runtime.

Daftar baca tetap terbuka untuk Viewer — mis. daftar aturan komisi tetap terbaca,
hanya formnya yang hilang; "lihat customer →" tetap ada karena read-only.

## 6. Current user display — SELESAI

Header menampilkan **Nama + Peran**; email muncul di menu identitas. Sumbernya
`GET /api/auth/session` → backend `GET /api/v1/auth/me`, satu-satunya sumber
identitas yang benar.

Identitas diambil **satu kali** di layout `(app)` dan dibagikan lewat
`lib/context/UserContext.tsx` (`UserProvider` di `components/layout/AppShell.tsx`,
dikonsumsi via `useUser()`/`useUserSafe()`). Tidak ada komponen yang memanggil
API identitas sendiri-sendiri — hook izin di bagian 5 juga membaca context yang
sama, bukan memicu request baru.

Logout membersihkan identitas: cookie `esa_session` dihapus dan context ikut
kosong, sehingga nama/peran pemakai sebelumnya tidak tertinggal di layar.

## 7. Halaman login — CEK VISUAL SELESAI

Tidak ada redesain. Yang diperiksa dan benar:

- Branding **NATA ALAM RAYA** + tagline, logo tampil (200).
- Show/hide password tersedia; tombol matanya punya `aria-label` dan bisa dicapai
  keyboard.
- Pesan error terbaca manusia. `backendErrorMessage()` di `lib/session-cookie.ts`
  mengupas amplop JSON backend, jadi yang sampai ke layar adalah
  **"email atau password salah"** — bukan `{"error":"..."}`.
- Tidak ada JSON mentah di mana pun pada layar login.

## 8. Regression gate — SEMUA HIJAU

Dijalankan ulang setelah seluruh perubahan task ini:

| Gate | Hasil |
|---|---|
| `npx tsc --noEmit --incremental false` | exit 0, nol error |
| `npm test` | **73 pass / 0 fail** |
| `npm run build` (production, `NEXT_DIST_DIR=.next-build`) | sukses |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0, nol temuan |
| `go test ./...` | exit 0, seluruh paket ok |
| `go test -tags integration -p 1 ./... -count=1` | exit 0, seluruh paket ok |

Test W-11 tetap PASS. Test W-12 RBAC tetap PASS. Tidak ada test yang di-skip,
dilonggarkan, atau dihapus untuk membuat gate hijau.

## 9. Final runtime client-demo check (A–G) — LULUS

Seluruhnya di tenant DEV, `http://localhost:3005` (loopback saja; tidak ada IP
LAN yang dipakai).

**A. Login** — Pemilik masuk, mendarat di `/dashboard`. Marketing masuk, mendarat
di `/penjualan` (`homePathFor`).

**B. Current user** — nama dan peran benar untuk keempat akun; satu request
identitas per sesi, bukan per komponen.

**C. Password salah** — backend 401; layar menampilkan **"email atau password
salah"**; tidak ada JSON mentah; pemakai **tetap di halaman login** (tidak ada
redirect, form tidak kosong mendadak).

**D. Empat peran** — diverifikasi dua lapis, UI dan backend langsung:

| Peran | UI | Backend (curl langsung ke endpoint) |
|---|---|---|
| Pemilik | akses penuh, termasuk kelola pengguna | 2xx |
| Accounting | penuh kecuali kelola pengguna | `POST /users` → **403** |
| Marketing | sidebar menyusut jadi 4 tautan penjualan; `/accounting/jurnal` langsung dilempar ke `/penjualan` | endpoint akuntansi → **403** `"role marketing tidak punya akses ke data ini"` (ditolak allow-list router lebih dulu) |
| Viewer | seluruh aksi tulis hilang, seluruh data tetap terbaca | endpoint tulis → **403** `"role tidak cukup untuk operasi ini"` |

Menyembunyikan tombol tidak menggantikan penjaga: setiap baris di kolom kanan
diuji dengan memanggil endpoint-nya langsung, bukan lewat UI.

**E. Session expiry** — cookie kedaluwarsa/tak terbaca → `307 → /login?expired=1`
dengan `set-cookie: esa_session=; Path=/; Max-Age=0`; backend menjawab 401 untuk
token itu; halaman login menampilkan **"Sesi Anda telah berakhir. Silakan login
kembali."** Satu peristiwa, bukan dua.

**F. Modal + notifikasi** — toast "email sudah terdaftar" muncul di atas modal
yang terbuka: `layerZ: "100"`, `portalToBody: true`, `modalOpen: true`, hit-test
di tiga titik mengembalikan `TOAST`. Tidak ada notifikasi yang tertimbun.

**G. `/logo.png`** — 200 dan tampil (lihat bagian 2).

## 10. Presentation cleanup — BERSIH

- **Console**: 10 layar demo ditelusuri dalam dua batch (`/dashboard`, `/laporan`,
  `/accounting/jurnal`, `/accounting/receivable`, `/accounting/pembayaran-vendor`,
  `/pengaturan`, `/penjualan/kpr`, `/penjualan/pembatalan`,
  `/accounting/collection`, `/accounting/pengeluaran`) dengan filter
  `["error","warning"]`. Hasil: **nol error, nol warning, nol hydration warning**.
  Satu-satunya keluaran adalah baris INFO bawaan React ("Download the React
  DevTools").
- **Network**: 69 permintaan tercatat pada penelusuran ulang — **nol 4xx, nol
  5xx**. Semua 200/304, termasuk `/logo.png` dan panggilan API
  (`/api/v1/sales-persons`, `/api/v1/sales-teams`, `/api/v1/ledger/journals`).
- **JSON mentah di UI**: tidak ada; pesan error backend dikupas amplopnya.
- **Gambar rusak**: tidak ada.
- **Branding / label peran**: konsisten (bagian 3 dan 4).
- **Tombol yang pasti 403**: sudah tidak ditawarkan (bagian 5).
- **Loading nyangkut**: tidak ditemukan; daftar yang gagal dimuat menampilkan
  pesan sebabnya, bukan empty state palsu.
- **Modal / toast layering**: benar (bagian 9-F).

---

## Temuan keamanan nyata yang ditemukan runtime — DITEMUKAN & DIPERBAIKI

Saat menjalankan matriks peran 9-D, **Viewer — peran baca — bisa menembus modul
pajak sampai ke business logic**:

- `POST /tax/tax-rates` → 400 (bukan 403) — bisa memasang tarif pajak
- `POST /units/{id}/tax/accrue` → 400 — bisa memposting jurnal akrual pajak
- `POST /tax/obligations/{id}/pay` → 422 — bisa memposting jurnal pelunasan

Status 400/422 itu artinya permintaan **diterima dan diproses**; yang menolak
adalah validasi payload, bukan peran. Modul pajak adalah satu-satunya modul yang
route tulisnya tidak memakai `auth.RequireWrite()`. Marketing memang tetap
tertolak, tapi oleh allow-list router — bukan oleh penjaga peran di modul ini.

Perbaikan (`backend/internal/tax/handler.go`) — tiga route tulis mendapat penjaga
yang sama dengan modul lain:

```go
r.With(auth.RequireWrite()).Post("/tax/tax-rates", h.setTaxRate)
r.With(auth.RequireWrite()).Post("/units/{unitID}/tax/accrue", h.accrueTax)
r.With(auth.RequireWrite()).Post("/tax/obligations/{id}/pay", h.payTax)
```

Jalur baca sengaja dibiarkan terbuka: auditor dan pemilik yang hanya membaca
tetap perlu melihat tarif dan kewajiban pajak.

**Tidak ada aturan pajak yang berubah** — hanya siapa yang boleh memanggil.
Dikunci oleh `backend/internal/tax/handler_guard_test.go`: route tulis wajib 403
untuk `viewer`, `marketing`, peran tak dikenal, dan tanpa peran; route baca wajib
tidak diblokir untuk `viewer`.

Penyisiran menyeluruh atas seluruh route tulis di semua modul mengonfirmasi tidak
ada celah sejenis yang tersisa — yang tanpa penjaga hanyalah endpoint `/preview`
(tidak mengubah apa pun), stub notaris yang membalas 410, dan route auth publik.

Karena blocker ini **ditemukan, diperbaiki, dan diberi test regresi di dalam task
ini**, dan seluruh gate hijau setelahnya, status akhir tetap PASS.

---

## Data dev yang dibuat selama verifikasi

- Tenant **9901009**, user **36** (`w12-cookie@dev.test`) — dari verifikasi
  cookie register (bagian 1).
- Tenant 9901008 beserta akun empat peran — sudah ada dari sesi sebelumnya.

Tidak ada transaksi keuangan baru yang diposting. Tidak ada `INSERT`/`UPDATE`/
`DELETE` manual ke database dev; seluruh data lahir dari alur aplikasi yang
memang sedang diuji.

## Catatan terbuka (tidak memblokir presentasi)

1. Artwork `public/logo.png` bertuliskan "NATA ALAM" tanpa "RAYA". Semua teks
   sudah benar; tinggal menimpa berkas gambarnya bila klien punya aset resmi.
2. `leads.name` bertipe `varchar(200)` dan MySQL memotong diam-diam; belum ada
   validasi panjang di backend. Tidak terlihat pada demo normal.

## Scope lock — dipatuhi

Tidak dikerjakan, sesuai instruksi: Advance Payment, Retention, Release,
redesain accounting, redesain AP, migration baru, workflow bisnis baru, refactor
auth besar. Perubahan kode task ini seluruhnya: satu berkas backend (otorisasi
route pajak) + satu test-nya, satu hook frontend baru, dan penjagaan tampilan di
10 komponen.
