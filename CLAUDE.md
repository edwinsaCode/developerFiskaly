# CLAUDE.md — esaProperti Coding Rules

Baca ini sebelum menyentuh kode apa pun. Aturan ini adalah invariant — berlaku di setiap fase, setiap file, setiap PR.

---

## 8 Invariant yang tidak bisa ditawar

1. **Jurnal selalu balanced.** Setiap `JournalEntry`: `Σ debit == Σ kredit`. Posting service menolak apa pun yang tidak seimbang. Tanpa pengecualian, selamanya.
2. **Uang tidak pakai float.** Semua nominal adalah `decimal.Decimal` (Go) / `DECIMAL(20,4)` (MySQL). `float64` yang menyentuh nilai uang adalah bug — gagal review.
3. **Alokasi rekonsiliasi sampai rupiah terakhir.** `Σ(biaya_teralokasi_per_unit) == total_biaya_proyek_dikapitalisasi`, persis. Sisa pembulatan ditentukan largest-remainder yang deterministik, tidak pernah dibuang.
4. **HPP = biaya terakumulasi unit.** Saat penjualan, HPP yang diakui untuk sebuah unit sama dengan biaya terakumulasi unit itu pada saat tersebut — bukan estimasi.
5. **Ledger append-only.** Jurnal yang sudah diposting bersifat immutable. Koreksi hanya via jurnal pembalik — tidak pernah edit atau hapus histori.
6. **Isolasi tenant via GORM global scope.** `tenant_id` di setiap baris; GORM global scope `WHERE tenant_id = ?` di setiap repository. Tidak ada query yang boleh lintas tenant — dibuktikan via integration test (MySQL tidak punya RLS).
7. **Penerimaan pra-pengakuan adalah kewajiban.** Kas/termin yang diterima sebelum kriteria pengakuan terpenuhi → Uang Muka Penjualan (kewajiban), bukan pendapatan.
8. **Satu BudgetPlan `active` per proyek/fase.** Saat versi baru di-approve, versi lama otomatis `superseded` — tidak dihapus (audit trail).

---

## Aturan uang (Money)

- **Jangan pernah** buat `Money` dari `float64`. Gunakan `NewMoney(string)` atau `FromInt(int64)`.
- Kolom GORM: semua field uang → `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`.
- Tipe Go: `domain.Money` membungkus `shopspring/decimal`.
- Semua aritmetika melalui method `Money` — jangan akses `.Decimal()` di luar package domain untuk kalkulasi.

## Aturan testing

- **Test dulu.** Fase belum selesai sebelum `go test ./...` hijau.
- Test alokasi uang wajib memverifikasi: `Σ(parts) == original`, persis.
- Test isolasi tenant wajib memverifikasi: query lintas-tenant mengembalikan nol hasil.
- Test posting jurnal wajib memverifikasi: jurnal yang sudah diposting tidak bisa diubah.

## Aturan ketidakpastian

Saat ragu soal aturan pajak, titik pengakuan, atau perlakuan hukum:
- Tinggalkan `// TODO(tax-advisor): <pertanyaan>`
- **Jangan** menebak atau mengarang aturan
- Celah yang ditandai lebih baik daripada angka yang salah

---

## Konvensi package

```
internal/domain/          → value object saja; tidak ada DB, tidak ada HTTP
internal/platform/        → DB, config, HTTP server, middleware
internal/*/repository.go  → akses DB; selalu gunakan GORM global scope untuk tenant
internal/*/service.go     → business logic; tidak ada akses DB langsung
internal/*/handler.go     → HTTP handler; tidak ada business logic
```

Aturan import: `domain` tidak boleh import package lain dari internal. Semua package lain boleh import `domain`. Tidak ada import siklik.

## SQL & Migrasi

- Semua perubahan schema via `backend/migrations/` (golang-migrate, MySQL dialect).
- **Jangan** gunakan GORM AutoMigrate di kode production.
- Semua kolom uang: `DECIMAL(20,4) NOT NULL DEFAULT '0.0000'`
- Semua tabel wajib memiliki: `id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY`, `tenant_id BIGINT UNSIGNED NOT NULL`, `created_at DATETIME(3)`, `updated_at DATETIME(3)`.
- Selalu buat index pada `tenant_id`.

## Jaringan development — loopback saja

Server development **tidak boleh** bisa dijangkau dari perangkat lain. Ini bukan preferensi:
data tenant nyata ada di database dev.

- Frontend: `npm run dev` (sudah `-H 127.0.0.1`). **Jangan** jalankan `next dev` telanjang —
  default Next adalah `0.0.0.0`. Butuh LAN? Itu keputusan pemilik, bukan default.
- Backend: `HOST`/bind address selalu `127.0.0.1`; port container hanya dipetakan
  `127.0.0.1:PORT:PORT` di `docker-compose.yml`.
- Verifikasi browser dilakukan lewat `http://localhost:PORT`. **Jangan pernah** memakai IP LAN
  (`192.168.x.x`, `10.x.x.x`) sebagai jalan pintas ketika browser tidak bisa menjangkau host —
  laporkan ke pemilik, jangan buka aksesnya.
- Setelah menyalakan server dev, buktikan: `ss -ltn` harus menunjukkan `127.0.0.1:PORT`,
  bukan `*:PORT`.

## Module Go

- Module path: `esaproperti`
- Go version: 1.22
- Semua dependency di `go.mod`; `go.sum` harus di-commit sebelum production build.
