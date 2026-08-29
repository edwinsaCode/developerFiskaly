# Billing Batch 2 — Verifikasi Final (UAT End-to-End + Root Cause)

**Tanggal:** 2026-08-05
**Gate:** klien menyatakan Billing Batch 2 baru selesai bila **dua** verifikasi lulus:
1. UAT end-to-end skenario bisnis nyata + bukti seluruh laporan membaca angka yang sama (SSOT).
2. Akar penyebab pasti anomali `tenants.require_realization_settled` yang sempat berubah menjadi 0.

**Hasil: kedua verifikasi LULUS.** Rincian, bukti, dan 5 temuan terbuka ada di bawah.

---

## 1. Metode

Bukan test suite unit — ini UAT terhadap **sistem yang berjalan**: backend di `127.0.0.1:8099/api/v1`,
MySQL nyata, tenant terisolasi `9900246 (UAT Batch 2)`, seluruh aksi lewat **HTTP API yang sama dengan
yang dipakai user**. Tidak ada penulisan langsung ke tabel untuk membuat kondisi.

| Skrip | Isi | Cek |
|---|---|---|
| `reset.py` | bersihkan tabel transaksi tenant, buat ulang proyek + unit A-1 (500jt) & B-1 (400jt) | — |
| `s1_cash.py` | siklus TUNAI penuh unit A-1: kontrak → jadwal → DP → termin → pelunasan | 84 |
| `s2_ops.py` | operasi titipan realisasi: tagih, bayar, alokasi manual, payout, void/pembalik, transfer, refund | 126 |
| `s3_bast.py` | biaya proyek → kapitalisasi → gate BAST → BAST A-1 (pendapatan, HPP, PPh Final) | 91 |
| `s4_kpr.py` | siklus KPR unit B-1: akad → gate → BAST → pencairan bank → kekurangan → pelunasan | 59 |
| `s5_ar.py` | piutang hidup unit C-1: DP lunas, termin dibayar sebagian & nunggak, tagihan realisasi nunggak | 35 |
| `s6_ssot.py` | **uji-silang seluruh laporan** terhadap ledger mentah dan terhadap satu sama lain | 262 |
| | **Total** | **657** |

Pipeline dijalankan **bersih dari nol** (`reset.py` → s1 → … → s6): **657 cek, 0 gagal**, deterministik
(diulang, hasil identik). Setiap event memverifikasi tiga hal sekaligus: **baris jurnal persis**
(akun, sisi, nominal), **Σ debit == Σ kredit**, dan **outstanding kanonik** sesudahnya.

---

## 2. Bukti per event — jurnal & outstanding

31 jurnal terposting dihasilkan. Ringkasan per tahap (semua sudah diverifikasi baris-per-baris):

### 2.1 TUNAI — unit A-1 Rp 500.000.000

| Event | Jurnal | Outstanding sesudah |
|---|---|---|
| Kontrak ditandatangani | **tidak ada jurnal** (diuji: 0 entri baru) | 500.000.000 |
| DP 100jt (5 Jun) | `1-1100 Dr 100.000.000 \| 2-2000 Cr 100.000.000` | 400.000.000 |
| Termin 2 150jt (10 Jun) | `1-1100 Dr 150.000.000 \| 2-2000 Cr 150.000.000` | 250.000.000 |
| Transfer titipan → harga 2jt | `2-2400 Dr 2.000.000 \| 2-2000 Cr 2.000.000` | 248.000.000 |
| Pelunasan 248jt (24 Jul) | `1-1100 Dr 248.000.000 \| 2-2000 Cr 248.000.000` | **0** |
| **BAST (25 Jul)** | ① `2-2000 Dr 500.000.000 \| 4-1000 Cr 500.000.000`<br>② `5-1000 Dr 350.000.000 \| 1-3000 Cr 100.000.000 \| 1-3100 Cr 200.000.000 \| 1-3200 Cr 50.000.000`<br>③ `5-2000 Dr 12.500.000 \| 2-4000 Cr 12.500.000` | 0 |

Invariant #7 terbukti: seluruh kas pra-BAST mendarat di **2-2000 Uang Muka (kewajiban)**, bukan pendapatan.
Pendapatan baru muncul di BAST, dan pada saat itu juga Uang Muka di-nol-kan.

### 2.2 Titipan realisasi (K-1..K-5) — unit A-1

| Event | Jurnal |
|---|---|
| Tagihan grup dibuat | **tidak ada jurnal** (K-1: titipan, bukan pendapatan) |
| Terima titipan 15jt / 6jt / 5jt / 5jt | `1-1100 Dr … \| 2-2400 Cr …` |
| Payout Notaris/PDAM/BPHTB/IMB/Sertifikat | `2-2400 Dr … \| 1-1100 Cr …` |
| Void pembayaran (salah kontak) | pembalik penuh `2-2400 Dr 5.000.000 \| 1-1100 Cr 5.000.000` — entri asal **tidak diubah** (Invariant #5) |
| Transfer antar-grup 3jt | `2-2400 Dr 3.000.000 \| 2-2400 Cr 3.000.000` |
| Refund sisa 1jt | `2-2400 Dr 1.000.000 \| 1-1100 Cr 1.000.000` |

**Saldo akhir 2-2400 = 0** (Dr 39.000.000 = Cr 39.000.000). Titipan tidak pernah menyentuh akun pendapatan
— rule klien K-1 terbukti dipatuhi di seluruh jalur, termasuk void, transfer, dan refund.

### 2.3 KPR — unit B-1 Rp 400.000.000

| Event | Jurnal | Outstanding |
|---|---|---|
| Kontrak KPR | tidak ada jurnal | 400.000.000 |
| DP 40jt | `1-1200 Dr 40.000.000 \| 2-2000 Cr 40.000.000` | 360.000.000 |
| **BAST sebelum akad** | **DITOLAK 422**, 0 jurnal (gate skema KPR = akad) | — |
| Akad kredit | tidak ada jurnal | 360.000.000 |
| BAST pasca-akad | ① `2-2000 Dr 40.000.000 \| 1-2200 Dr 360.000.000 \| 4-1000 Cr 400.000.000`<br>② HPP 350jt<br>③ PPh Final 10jt | 360.000.000 |
| Pencairan bank 340jt | `1-1200 Dr 340.000.000 \| 1-2200 Cr 340.000.000` | 20.000.000 |
| Invoice KEKURANGAN 20jt | tidak ada jurnal; invoice kedua ditolak 422 | 20.000.000 |
| Pelunasan kekurangan 20jt | `1-1200 Dr 20.000.000 \| 1-2200 Cr 20.000.000` | **0** |

Saldo akhir `1-2200 Piutang Bank` = **0**. Sisa harga pasca-BAST menjadi piutang, bukan uang muka — benar.

### 2.4 Piutang hidup — unit C-1 Rp 300.000.000

DP 60jt lunas; termin 100jt dibayar sebagian 40jt dan **lewat jatuh tempo**; final 140jt belum jatuh tempo;
tagihan realisasi BPHTB 12jt lewat tempo dan belum dibayar.
Outstanding rumah **200.000.000**, outstanding realisasi **12.000.000** — dipakai sebagai bahan uji-silang aging.

---

## 3. Bukti SSOT — satu angka dibaca sama oleh semua laporan

Ini inti permintaan klien. Setiap angka di bawah diambil **secara independen** dari tiap laporan lewat
endpoint masing-masing, lalu dibandingkan dengan ledger mentah (SQL langsung ke `journal_lines`).

| Angka | Nilai | Dibaca identik oleh |
|---|---|---|
| Kas & bank | **330.000.000** | ledger mentah · trial balance · neraca · dashboard · arus kas |
| Pendapatan | **900.000.000** | ledger · laba-rugi · P&L per proyek · dashboard · Σ profit per unit |
| HPP | **700.000.000** | ledger · laba-rugi · P&L per proyek · dashboard |
| Uang Muka Penjualan | **100.000.000** | ledger · trial balance · neraca |
| Outstanding rumah | **200.000.000** | financial-summary kontrak · piutang dashboard · statement · AR aging |
| Outstanding realisasi | **12.000.000** | ringkasan grup · portfolio outstanding · aging biaya |
| Laba bersih | **177.500.000** | ledger (900 − 700 − 22,5) · laba-rugi · P&L proyek · dashboard |

Selain itu diverifikasi:
- **Trial balance seimbang**: Σ debit = Σ kredit = **3.365.500.000**; setiap baris akun cocok dengan SQL mentah.
- **Persamaan akuntansi** neraca terpenuhi (Aset = Kewajiban + Ekuitas).
- **Jurnal draft tidak bocor** ke laporan mana pun (semua reader memfilter `posted_at IS NOT NULL`).
- **AR aging** cocok baris-per-baris dengan `payment_schedules`, termasuk umur hari dan bucket
  (`current`, `1_30`, `31_60`, `61_90`, `90_plus`) — dan aging biaya realisasi memakai **engine yang sama**.
- **Kwitansi**: seri KWT 1..9 dan KWR 1..5 kontinu tanpa lompatan; nominal tiap kwitansi == nominal termin;
  semuanya bisa diambil ulang lewat `/termins/{id}/receipt`.
- **Biaya aktual** cocok dengan ekspresi netting kanonik (debit non-reversal − kredit reversal) = 670.000.000,
  dan `progress_pct` dashboard == aktual/RAB.

---

## 4. Verifikasi #2 — akar penyebab `require_realization_settled` = 0

**Kesimpulan: berasal dari lingkungan pengujian (skrip smoke kita sendiri), bukan cacat produk, dan tidak
dapat terjadi di produksi tanpa aksi user yang ter-otentikasi.** Tiga bukti independen:

### Bukti A — enumerasi seluruh penulis kolom
Di seluruh codebase hanya ada **satu** statement yang menulis kolom ini di jalur produk:
`internal/charge/query.go:254` (`SetRequireRealizationSettled`), yang hanya dijangkau lewat
`PUT /charges/policy` — endpoint ter-otentikasi, ber-tenant, dan melakukan **read-back** setelah menulis.
Struct GORM `tenant.Tenant` **tidak memuat kolom ini**, sehingga tidak ada `Save`/`Updates` full-row yang
bisa menimpanya secara tak sengaja. Penulis lain hanya dua:
- cleanup integration test (`charge_integration_test.go:68,88`) — **di-hard-scope ke tenant 9.900.244**;
- migrasi 000059 down+up (`DROP`/`ADD COLUMN … DEFAULT 0`) — hanya lewat `make migrate-down` yang dev-only;
  produksi memakai `make migrate-prod` (up-only), dan `migrate up` pada versi terkini adalah no-op.

### Bukti B — forensik log akses (penyebab persisnya)
Access log sesi-sesi sebelumnya merekam respons `PUT /charges/policy` berukuran **37B** lalu **38B**.
Pemetaan byte itu saya buktikan langsung: `{"require_realization_settled":true}` = 37B,
`…:false}` = 38B. Jejaknya:

| Sesi | Waktu | Respons | Arti |
|---|---|---|---|
| 7c3e2122 | 12:07:29 | 37B → 38B | set `true`, lalu **dikembalikan ke `false`** |
| 1ae23c48 | 14:33:06 / 14:33:57 | 37B → 38B | idem |

Artinya: skrip smoke menyalakan kebijakan untuk mengetes gate, lalu **mengembalikannya ke default `false`
di akhir run** — persis sebagaimana seharusnya skrip test berperilaku. Sesi berikutnya membaca 0 dan
menyimpulkan itu "anomali". Tidak ada penulis misterius.

### Bukti C — eksperimen terkontrol
Seluruh 14 tenant di-set `1`, lalu:
- `go test -count=1 ./...` → rc=0, 20 paket ok, **tidak ada tenant yang berubah**;
- `TEST_DB_DSN=… go test -count=1 -tags integration ./internal/charge/...` → rc=0, dan **hanya**
  tenant 9900244 (CG Test) yang kembali ke 0. 9900245 dan 9900246 tetap 1.

Blast radius test terbukti secara empiris terbatas pada tenant fixture-nya sendiri. Kebijakan tenant UAT
juga bertahan `1` selama seluruh pipeline 657 cek.

**Catatan penting (bukan bug, tapi risiko governance):** perubahan kebijakan ini **tidak menulis audit trail**.
Untuk sebuah flag yang mengatur gate finansial (boleh/tidak BAST sebelum titipan lunas), sebaiknya setiap
perubahan tercatat siapa-kapan-dari-nilai-berapa. Lihat rekomendasi R-A.

---

## 5. Temuan terbuka (dilaporkan, tidak ditambal diam-diam)

> **Status per 2026-08-05:** tabel ini adalah catatan temuan **saat UAT**. T-1, T-3, T-4, T-5, dan R-A
> sudah diputuskan klien dan **ditutup** — lihat §8 untuk keputusan, implementasi, dan buktinya.
> Hanya **T-2 (true-up P0-4)** yang masih terjadwal.

| # | Temuan | Bukti | Dampak | Keputusan yang diminta |
|---|---|---|---|---|
| **T-1** | Transfer titipan → harga rumah menerbitkan **kwitansi KWT baru** untuk uang yang sudah pernah dikwitansikan sebagai KWR | s1 event transfer 2jt: KWR terbit saat terima titipan, KWT terbit lagi saat transfer | Pembeli memegang 2 kwitansi untuk 1 aliran uang → berisiko dispute | Klien: apakah transfer internal boleh tanpa kwitansi baru (cukup memo/mutasi), atau kwitansi transfer diberi seri/tipe sendiri? |
| **T-2** | HPP diakui **budgeted** 350jt × 2 unit = 700jt, sedangkan biaya terkapitalisasi aktual 670jt → persediaan berakhir **minus 30jt** (`1-3100` −20jt, `1-3200` −10jt) | trial balance akhir | Konsekuensi yang **memang diharapkan** dari kebijakan B2 budgeted-HPP (PSAK 44) selama true-up P0-4 belum ada | Konfirmasi bahwa P0-4 (true-up saat penyelesaian proyek) tetap wajib sebelum go-live akuntansi, atau persediaan minus di-guard sementara |
| **T-3** | Pelunasan **kekurangan KPR** (utang pembeli, bukan bank) dikreditkan ke `1-2200 Piutang Bank`, bukan direklas ke `1-2000 Piutang Usaha` | s4 event pelunasan 20jt | Saldo akhir tetap benar (0), tapi **umur & komposisi piutang salah**: piutang pembeli tampil sebagai piutang bank | Akuntan: perlu reklas otomatis saat invoice KEKURANGAN terbit? |
| **T-4** | Dashboard `overdue_schedules` **bukan angka turunan** — membaca kolom `payment_schedules.status`, yang hanya terisi kalau `POST /schedules/mark-overdue` dijalankan. Tidak ada scheduler yang memanggilnya | `dashboard.go:284`; dibuktikan live: sebelum job dashboard 0 vs aging 1; sesudah job `{"marked_overdue":1}` → cocok | Dashboard bisa menampilkan **0 tunggakan padahal ada** | Pilih: (a) pasang cron harian, atau (b) jadikan turunan langsung dari tanggal seperti AR aging (rekomendasi saya: b — sejalan dengan prinsip SSOT) |
| **T-5** | Statement `total_overdue` memakai **nominal cicilan bruto**, mengabaikan `paid_amount`; AR aging memakai `amount − paid` | `internal/sale/statement.go:224`; kasus C-1: statement 100jt vs aging 60jt | Dua laporan menyebut angka tunggakan berbeda untuk pembeli yang sama — **pelanggaran SSOT** | Perbaiki statement mengikuti aging (60jt). Perlu persetujuan karena mengubah angka yang tampil ke pembeli |

### Observasi minor (tidak memblokir)
- Kosakata RAB memakai `construction` sementara realisasi biaya memakai `hard` — sudah dipetakan benar, tapi membingungkan pembaca.
- Entri biaya memerlukan langkah *post* eksplisit terpisah; belum ada di UI flow.
- Penandatanganan kontrak **tidak** otomatis mengunci/reserve unit — masih perlu transisi status manual.
- `days_overdue` pada statement tetap terisi untuk cicilan yang sudah lunas (kosmetik).
- Saldo `1-1100` berakhir −70jt **karena tenant UAT tidak punya setoran modal awal** (reset menghapus semua jurnal). Bukan cacat produk; total kas gabungan +330jt.

### Rekomendasi
- **R-A** — catat audit trail untuk `PUT /charges/policy` (aktor, waktu, nilai lama → baru). Flag ini
  mengatur gate finansial; perubahannya harus bisa dipertanggungjawabkan.
- **R-B** — jadikan `overdue_schedules` turunan (T-4) agar tidak ada angka "tersimpan" yang bisa basi.
- **R-C** — samakan definisi tunggakan statement dengan engine aging (T-5).

---

## 6. Status kebersihan lingkungan

| Tenant | Nama | `require_realization_settled` | Catatan |
|---|---|---|---|
| 9900244 | CG Test | 0 | fixture integration test — nilai alaminya setelah cleanup |
| 9900245 | CG Smoke Dev | 1 | |
| 9900246 | UAT Batch 2 | 1 | bertahan utuh sepanjang 657 cek |

---

## 7. Kesimpulan

- **Verifikasi #1 (UAT + SSOT): LULUS** — 657 cek, 0 gagal, dari state bersih, deterministik. Setiap event
  punya bukti jurnal + outstanding, dan setiap angka kunci terbukti dibaca identik oleh seluruh laporan.
- **Verifikasi #2 (root cause): LULUS** — penyebab pasti teridentifikasi (skrip smoke mengembalikan flag ke
  default lewat endpoint resmi), dengan bukti forensik log, enumerasi penulis, dan eksperimen terkontrol
  yang membuktikan test tidak dapat menyentuh tenant lain. Tidak ada jalur produksi yang bisa mengubah nilai
  ini secara diam-diam.

Yang tersisa untuk dinyatakan production-ready adalah **keputusan klien/akuntan atas T-1, T-3, T-4, T-5**
dan konfirmasi bahwa **T-2 (true-up P0-4)** tetap dijadwalkan. Semuanya perlu keputusan bisnis, bukan tebakan
saya — sesuai aturan `CLAUDE.md`: celah yang ditandai lebih baik daripada angka yang salah.

*(Ditulis sebelum keputusan klien. Semua keputusan itu sudah turun — lihat §8; sisa: T-2/P0-4.)*

---

## 8. Keputusan klien & penyelesaian (2026-08-05)

Semua temuan §5 sudah diputuskan. Berikut keputusan, apa yang dikerjakan, dan bukti verifikasinya.

| # | Keputusan klien | Yang dikerjakan | Bukti |
|---|---|---|---|
| **T-1** | Transfer Titipan → Harga Rumah adalah **transfer internal**. Tidak boleh terbit kwitansi pembayaran baru; buat **memo transfer internal** untuk audit trail; tidak boleh terlihat sebagai kas masuk baru. | Kwitansi ditekan untuk `payment_source='realization_transfer'`. Terbit **Memo Transfer Internal** berseri `MTI/YYYY/NNNNNN` lewat `billing.NextDocumentNumber` (migration `000060`), untuk `transfer_house` **dan** `transfer_group`. Seam `charge.MemoNumberer` **fail-closed**: transfer ditolak (`ErrMemoNumbererMissing`) kalau penomoran belum di-wire. Endpoint `GET /charges/settlements/{id}/memo`; cetak A4 di frontend (`PrintTransferMemoButton`), berlabel "BUKAN bukti terima uang". | UAT **E22b**: 0 kwitansi dari transfer, memo hanya pada aksi transfer, seri `MTI/2026/000001..2`, refund → 400. Integration test lifecycle: memo terbit + merujuk jurnal, tanpa numberer transfer ditolak & saldo `2-2400` tidak bergerak. |
| **T-2** | Tetap desain sekarang: budgeted HPP saat BAST, true-up di proses closing sesuai roadmap. | **Tidak ada perubahan kode** (sesuai keputusan). P0-4 tetap prasyarat go-live akuntansi. | — |
| **T-3** | **KEPUTUSAN FINAL (2026-08-05):** setelah pencairan KPR, bila dana bank lebih kecil dari nilai kontrak, selisihnya adalah **Piutang Customer**, bukan Piutang Bank. *Bank sudah selesai pada nilai pencairan aktualnya.* Contoh klien: harga 500jt, DP 50jt, cair 430jt → Piutang Customer 20jt, Piutang Bank 0. **Akun Piutang Bank tidak boleh lagi dipakai untuk kekurangan pasca-pencairan.** | `KPRPolicy.ResolveReceivableAccount` kini memetakan **hanya `akad`** ke `1-2200`; `disbursed` dan seterusnya → `1-2000` (TODO dihapus). Reklas dipusatkan di `Service.reclassOnStateChange` sehingga **setiap** perpindahan state yang mengubah akun piutang menerbitkan jurnal reklas — termasuk jalur milestone otomatis pada pencairan, yang sebelumnya maju state tanpa jurnal. Kemajuan milestone **dibatalkan** bila reklas gagal (state tidak boleh mendahului ledger). `PreviewCollectionPayment` tidak lagi hardcode `1-2000`: memakai resolver yang sama dengan posting. Deskripsi COA `1-2200` + teks sukses modal pencairan diperbarui. | Integration `TestIntegration_T3_ShortfallIsCustomerReceivable` (500/50/430 persis contoh klien). Consistency `EQ_T3_Outstanding_BukanPiutangBank` + `EQ_T3_CreditAccount_Preview_vs_Posting`. UAT **E40/E40b/E42/E42b**: pencairan menerbitkan 2 jurnal (uang masuk + reklas 20jt `1-2200`→`1-2000`), `1-2200` = 0, pratinjau kasir & pelunasan sama-sama `1-2000`. |
| **T-4** | Overdue dashboard harus **turunan tanggal**, sejalan dengan AR Aging, bukan bergantung status hasil job. | `overdue_schedules` kini diturunkan dari tanggal jatuh tempo vs `asOf` memakai definisi aging yang sama; kolom `payment_schedules.status` tidak lagi menjadi sumber angka dashboard. | UAT **L6b-1**: KPI == aging **tanpa** menjalankan job sama sekali, dan **tidak berubah** setelah `POST /schedules/mark-overdue` dijalankan. |
| **T-5** | Statement harus mengikuti angka Aging — pembeli tidak boleh melihat dua angka outstanding berbeda. | `statement.go` memakai definisi neto (`amount − paid`, clamp nol) untuk `outstanding`, `overdue`, dan `total_overdue`; status efektif (`paid/overdue/due_today/scheduled`) kini konsisten dengan kolom Outstanding — baris bersisa nol tidak pernah tampil "menunggak". Frontend statement menampilkan kolom **Dibayar** dan **Sisa**. | UAT **L6c**: `total_overdue` == aging neto, `amount == paid + outstanding` per baris, baris lunas bersisa nol, rekonsiliasi per tanggal jatuh tempo. |
| **R-A** | Audit trail perubahan `charges.policy` ditambahkan (perubahan konfigurasi finansial). | Tabel `tenant_policy_changes` (migration `000060`) merekam `policy_key`, `old_value → new_value`, `changed_by`, `notes`, waktu. `SetRequireRealizationSettled` menerima aktor + alasan; set ulang nilai yang sama **tidak** menghasilkan baris. Endpoint `GET /charges/policy/history`. UI Pengaturan: modal konfirmasi dengan **alasan wajib** + daftar "Riwayat Perubahan Kebijakan". | UAT **E25b**: satu baris `false→true`, aktor & catatan tersimpan, baris persisten di DB, set ulang nilai sama tidak menambah baris. Integration test `TestIntegration_Policy_FailClosed`: 4 kali set → 3 baris audit dengan transisi benar. |

### Verifikasi ulang penuh

| Suite | Cek | Gagal |
|---|---|---|
| s1 skenario dasar | 84 | 0 |
| s2 operasional (termasuk E22b) | 134 | 0 |
| s3 BAST & policy (termasuk E25b) | 99 | 0 |
| s4 KPR & pelunasan (termasuk T-3: E40, E40b, E42, E42b) | 65 | 0 |
| s5 pembatalan | 35 | 0 |
| s6 SSOT lintas laporan (L6b-1, L6c) | 285 | 0 |
| **Total** | **702** | **0** |

`go build ./...`, `go test -count=1 ./...`, dan `go test -tags integration -count=1 ./...` semuanya hijau
(21 paket) setelah perubahan T-3.

### Audit lifecycle T-3 — pembaca yang diperiksa

Semua permukaan di bawah dibuktikan memakai definisi yang sama (kekurangan pasca-pencairan =
**Piutang Customer**), bukan hanya yang diubah:

| Modul | Status | Catatan |
|---|---|---|
| Jurnal pencairan + reklas | **diubah** | Reklas otomatis pada `akad → disbursed`, dari jalur event **maupun** milestone. |
| Invoice Kekurangan | aman | Nominal berasal dari `ContractFinancialSummary` (outstanding kanonik) — tidak pernah menyebut akun. |
| Collection (pratinjau + posting) | **diubah** | Pratinjau dulu hardcode `1-2000` sementara posting memakai resolver; sekarang satu sumber. Label `1-2200` = "Piutang Bank (KPR)". |
| Statement 360 | aman | Hanya menyentuh `BankAccountCode` (rekening kas), bukan akun piutang. |
| Customer Ledger / Aging | aman | `BuildARAging` bekerja dari cicilan & tanggal — nol referensi kode akun. Satu engine aging untuk semua pembaca. |
| Dashboard | aman | Piutang dari aging; total neraca menjumlah `ledger.RoleReceivable` (`1-2000/1-2100/1-2200`) — **totalnya tidak berubah**, hanya komposisinya yang kini benar. |
| Reminder / overdue | aman | Turunan tanggal (T-4), tidak mengenal akun. |
| Reporting (neraca, L/R, project P&L) | aman | Membaca ledger posted — otomatis ikut reklas. |

Tidak ada modul yang masih menganggap outstanding pasca-pencairan sebagai Piutang Bank.

### Sisa yang menunggu klien
- **T-2 / P0-4** — true-up HPP saat closing tetap prasyarat go-live akuntansi (persediaan bisa minus sampai itu ada).
