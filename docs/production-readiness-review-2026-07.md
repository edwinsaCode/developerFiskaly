# Production Readiness Review — 2026-07-22

> Setelah roadmap R1–R7 selesai. Basis: kode aktual + E2E API + smoke test Chrome.

## 1. Kesehatan teknis (diverifikasi hari ini)

| Area | Status |
|---|---|
| `go build ./...` | ✅ |
| `go test ./...` (19 paket) | ✅ hijau |
| `tsc --noEmit` | ✅ |
| Migration | ✅ versi **51**, clean, up/down lengkap |
| Neraca saldo E2E | ✅ balanced persis (Σ 1.622.500.000 dua sisi) |
| Invariant #1–#8 | ✅ tidak ada yang disentuh roadmap ini (R1–R7 additive; satu-satunya sentuhan posting = R1 gate/settle yang sudah ber-test) |

## 2. Flow bisnis inti — status production

| Flow | Status |
|---|---|
| Booking → kontrak → skema (KPR/tunai/inhouse) | ✅ |
| KPR end-to-end (pengajuan→SP3K→akad→cair→kekurangan→lunas→BAST) | ✅ E2E |
| Pencairan parsial + invoice kekurangan + settle otomatis | ✅ |
| HPP budgeted (RAB → BAST → true-up P0-4) | ✅ (true-up ada) |
| Pembatalan pra/pasca-BAST + refund + clawback komisi | ✅ (increment 8/9) |
| Statement 360, pipeline KPR, dashboard eksekutif | ✅ |
| Progress fisik vs biaya vs collection | ✅ |
| Periode & tutup buku, jurnal berulang, GL drill-down | ✅ (PS-5/6) |

## 3. Risiko & gap yang HARUS diketahui sebelum release

1. **Booking fee outside price (R4) belum dibangun** — design §4 architecture review
   masih menunggu approval. Klien yang praktiknya "fee di luar harga" belum terlayani;
   default sekarang fee = bagian pembayaran.
2. ~~**Backend race saat boot**~~ — ✅ **DIPERBAIKI 2026-07-25.** Akar masalah: `db.Connect`
   me-retry hanya `Ping`, sedang `gorm.Open` sendiri gagal "connection refused" saat MySQL
   belum nyala dan langsung return; `depends_on:service_healthy` hanya berlaku saat
   `compose up`, TIDAK pada auto-restart pasca-reboot. Fix: seluruh open+ping di-retry
   (tenggat 90 dtk) → backend self-heal. Diverifikasi: MySQL dimatikan → backend retry
   (bukan mati) → MySQL nyala → backend connect otomatis, /health 200. Plus `make start`
   idempoten (service + migrasi satu perintah) & `make restart-backend`.
3. **Kop legal dokumen tenant** (alamat + NPWP di kwitansi/invoice) — backlog H-2,
   klien mencantumkannya di referensi. Kecil, tapi terlihat oleh customer akhir.
4. **Branding sidebar hardcoded "NATA ALAM"** — wajar untuk satu klien; untuk multi-
   tenant komersial harus dari profil tenant.
5. **Recurring journals belum auto-run** (PS5-B2) — tombol manual; cron-safe endpoint
   sudah ada, tinggal scheduler.
6. **Progress fisik = disiplin input manual** — mitigasi sudah ada (append-only,
   audit, "terakhir update"); tambah reminder >30 hari (R3-B4) bila jadi keluhan.
7. **Dashboard komposit satu endpoint** — cepat untuk data sekarang; ukur saat data
   klien nyata masuk (R5-B2 lazy per seksi sudah disiapkan sebagai rencana).
8. **Verifikasi mobile** baru level kode (R7-B4) — cek di perangkat nyata.

## 4. Rekomendasi sebelum release ke klien (urut prioritas)

1. Perbaiki startup race backend (item 3.2) — setengah hari.
2. Kop legal tenant di kwitansi/invoice (item 3.3) — kecil, dampak persepsi besar.
3. Jalankan UAT dengan data riil Nata Alam satu proyek penuh (RAB → booking →
   KPR → BAST → tutup buku) di staging.
4. Putuskan R4 (booking fee outside price): approve design §4 → build terisolasi.
5. Aktifkan cron recurring + mark-overdue harian.
6. Smoke test mobile di HP.
