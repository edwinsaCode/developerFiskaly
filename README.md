# esaProperti

> **Nama kode sementara — bebas diganti.** Platform akuntansi & pelaporan keuangan multi-tenant yang dibangun khusus untuk **pengembang properti (real estate) Indonesia**. Produk saudara dari esaFiskaly, tapi dirancang untuk akuntansi pengembangan berbasis proyek, bukan pembukuan UMKM umum.

---

## 1. Ini sistem apa

Tools akuntansi umum (Accurate, Mekari Jurnal, Zahir) memperlakukan biaya seperti warung kopi: beli stok, jual, akui COGS. Pengembangan properti tidak begitu. Sebuah vila butuh 2–3 tahun untuk dibangun, dan biaya dari tanah, konstruksi, perizinan, konsultan, serta pembiayaan **menumpuk di neraca sebagai persediaan** — bukan masuk Laba Rugi saat dibelanjakan. HPP baru muncul **saat sebuah unit terjual**, dan hanya sebesar *porsi biaya unit itu* dari total biaya proyek.

esaProperti adalah engine akuntansi yang melakukan ini dengan benar, dan menghasilkan dua tingkat laporan keuangan:

- **Laporan per-proyek** — apakah pengembangan *ini* benar-benar untung? (mis. LITHOS: pendapatan dari unit terjual − HPP unit tersebut)
- **Laporan konsolidasi entitas** — Neraca + Laba Rugi yang menggabungkan semua proyek plus G&A korporat.

Mekanik "set HPP per proyek" adalah tulang punggung seluruh sistem. Semua hal lain bergantung padanya.

## 2. Domain dalam satu layar

Baca ini sebelum menulis kode apa pun. Ini adalah *alasan* di balik data model.

| Konsep | Artinya di sini |
|---|---|
| **Project** | Sebuah pengembangan (mis. LITHOS). Mengakumulasi seluruh biaya yang dikapitalisasi. |
| **ProjectPhase** | Tahap dalam proyek (mis. Tahap 1 = 30 unit, Tahap 2 = 20 unit). Unit dan biaya bisa ditag ke fase spesifik. RAB juga bisa per-fase. |
| **Unit** | Unit yang dijual di dalam proyek/fase (mis. salah satu dari 18 vila). Punya luas terjual (saleable area), fase, harga jual, status, pembeli. |
| **BudgetPlan (RAB)** | Rencana Anggaran Biaya per proyek/fase. Mendukung versioning (RAB v1, v2, ...). Hanya satu versi yang `active` dalam satu waktu. Versi lama menjadi `superseded` — tidak dihapus, untuk audit trail. |
| **BudgetItem** | Baris RAB per kategori biaya (tanah, konstruksi, soft cost, pembiayaan, dll). Target nominal yang akan dibandingkan realisasi. |
| **Biaya kapitalisasi** | Tanah, konstruksi (hard), soft cost (desain/perizinan/legal), pembiayaan yang dikapitalisasi. Diposting ke **Persediaan Real Estat** (inventory), ditag ke proyek/fase dan — bila memungkinkan — ke unit spesifik. |
| **Alokasi biaya** | Biaya tingkat-proyek (tanah, infrastruktur bersama) disebar ke unit-unit dengan **basis** eksplisit (luas terjual atau nilai jual relatif), menghasilkan biaya terakumulasi per unit. |
| **PaymentSchedule** | Jadwal pembayaran buyer per unit: DP, angsuran (KPR/tunai), pelunasan. Setiap termin punya tanggal jatuh tempo dan status (scheduled/received/overdue). Pembayaran sebelum BAST = Uang Muka Penjualan (kewajiban). |
| **Penjualan / pengalihan** | Event pemicu. Saat pengakuan (BAST), ia memicu **pendapatan** *dan* **HPP** *dan* **pajak** dalam satu transaksi yang seimbang. |
| **HPP (COGS)** | Saat penjualan unit = biaya terakumulasi unit itu, dipindahkan dari persediaan ke Laba Rugi. |
| **Pengakuan pendapatan** | PSAK 72. Default v1: **point-in-time saat serah terima (BAST)**. Cicilan (termin) sebelum itu dicatat sebagai **Uang Muka Penjualan**. |
| **PPh Final Pengalihan** | Pajak final 2,5% atas setiap pengalihan properti (PP 34/2016). Khusus pengembang — tidak ada di esaFiskaly. |

> ⚠️ **Bukan nasihat pajak/akuntansi.** Aturan posting dan kode COA di repo ini bersifat default ilustratif. Perlakuan persisnya — terutama struktur HGB-80/leasehold LITHOS ke pembeli asing, yang mungkin merupakan premi sewa, bukan pengalihan hak milik penuh — wajib dikonfirmasi ke akuntan/konsultan pajak Indonesia berlisensi sebelum go-live.

## 3. Tech stack

| Layer | Pilihan |
|---|---|
| Frontend | **Next.js** (App Router) + **TypeScript** |
| Backend API | **Go (Golang)** |
| ORM | **GORM** |
| Database | **MySQL 8.0** |
| Tipe uang | `github.com/shopspring/decimal` → `DECIMAL(20,4)` di MySQL. **Jangan pernah `float`.** |
| Migrasi | SQL berversi (golang-migrate) — GORM AutoMigrate hanya untuk dev |
| API | REST (JSON) over HTTP; spec OpenAPI disimpan di `/docs/api` |
| Infra | **Docker** + **docker-compose** (dev & prod); **Makefile** sebagai entrypoint perintah |

## 4. Arsitektur

```
┌──────────────────────────────────────────────────────────────────────┐
│                         Docker Compose                               │
│                                                                      │
│  ┌──────────────┐   HTTPS/JSON   ┌───────────────────────────────┐  │
│  │  Next.js     │ ─────────────► │     Go API (GORM)             │  │
│  │  :3000       │                │     :8080                     │  │
│  │  frontend/   │ ◄───────────── │  ledger · projects · budget · │  │
│  └──────────────┘                │  cost · cogs · sales ·        │  │
│         ▲                        │  payment · tax · reporting     │  │
│         │                        └──────────────┬────────────────┘  │
│    nginx :80/:443                               │                   │
│    (prod only)                          ┌───────▼──────┐           │
│                                         │  MySQL 8.0   │           │
│                                         │  DECIMAL(20,4│           │
│                                         │  tenant scope│           │
│                                         └──────────────┘           │
└──────────────────────────────────────────────────────────────────────┘
```

Backend Go memegang **seluruh** logika akuntansi. Frontend tidak pernah menghitung saldo, angka HPP, jumlah pajak, atau persentase realisasi RAB — ia hanya menampilkan apa yang dikembalikan API. Ini menjaga sumber kebenaran keuangan di satu tempat yang strongly-typed dan mudah dites.

## 5. Struktur repository

```
.
├── README.md                  ← Anda di sini
├── ROADMAP.md                 ← rencana build berfase untuk Claude Code
├── CLAUDE.md                  ← aturan repo & invariant untuk Claude Code (lihat ROADMAP P0)
├── Makefile                   ← entrypoint semua perintah (dev/prod/migrate/test/lint)
├── docker-compose.yml         ← development: MySQL + backend (air) + frontend (dev)
├── docker-compose.prod.yml    ← production: MySQL + backend (binary) + frontend (standalone) + nginx
├── .env.example               ← template env untuk docker-compose
├── backend/
│   ├── Dockerfile             ← multi-stage: target `dev` (air) + target `prod` (binary)
│   ├── .env.example
│   ├── cmd/api/               ← entrypoint utama
│   ├── cmd/seed/              ← seed data fixture LITHOS
│   ├── internal/
│   │   ├── domain/            ← value object: Money, AllocationBasis, dll.
│   │   ├── ledger/            ← jurnal, baris, posting, penjaga double-entry
│   │   ├── tenant/            ← multi-tenancy, GORM global scope
│   │   ├── project/           ← proyek + fase proyek (ProjectPhase)
│   │   ├── unit/              ← unit yang dijual
│   │   ├── budget/            ← RAB: BudgetPlan (versioned), BudgetItem, realisasi
│   │   ├── cost/              ← kapitalisasi biaya → posting persediaan
│   │   ├── cogs/              ← engine alokasi (jantungnya)
│   │   ├── sales/             ← event penjualan, pengakuan pendapatan, termin/uang muka
│   │   ├── payment/           ← PaymentSchedule, Payment (KPR/tunai), status overdue
│   │   ├── tax/               ← PPh Final, PPN
│   │   ├── reporting/         ← semua laporan keuangan
│   │   └── platform/          ← db, config, http server, middleware
│   ├── migrations/            ← SQL berversi (golang-migrate, MySQL dialect)
│   └── go.mod
├── frontend/
│   ├── Dockerfile             ← multi-stage: target `dev` + target `prod` (standalone)
│   ├── .env.example
│   ├── app/
│   ├── components/
│   ├── lib/api/               ← API client bertipe
│   └── package.json
├── nginx/
│   └── nginx.conf             ← reverse proxy (prod): frontend :3000, backend /api :8080
└── docs/
    ├── posting-rules.md       ← setiap jurnal yang dihasilkan tiap event bisnis
    ├── coa.md                 ← chart of accounts
    └── api/                   ← OpenAPI
```

## 6. Invariant yang tidak bisa ditawar

Inilah yang membedakan *demo yang jalan* dari *ledger yang bisa dipercaya*. Setiap fase wajib menegakkan semuanya. Diulang di `CLAUDE.md` dan ditegakkan lewat test.

1. **Jurnal selalu balanced.** Setiap jurnal: `Σ debit == Σ kredit`. Posting service menolak apa pun yang tidak seimbang. Tanpa pengecualian, selamanya.
2. **Uang tidak pakai float.** Semua nominal adalah `decimal.Decimal` (Go) / `NUMERIC` (Postgres). `float64` yang menyentuh nilai uang adalah bug.
3. **Alokasi rekonsiliasi sampai rupiah terakhir.** `Σ(biaya teralokasi per unit) == total biaya proyek yang dikapitalisasi`, persis. Sisa pembulatan ditentukan oleh aturan eksplisit & deterministik (largest-remainder), tidak pernah dibuang.
4. **HPP = biaya terakumulasi unit.** Saat penjualan, HPP yang diakui untuk sebuah unit sama dengan biaya terakumulasi unit itu pada saat tersebut — bukan estimasi.
5. **Ledger append-only.** Jurnal yang sudah diposting bersifat immutable. Koreksi dilakukan via jurnal pembalik (reversing entry), bukan dengan mengedit atau menghapus histori.
6. **Isolasi tenant.** `tenant_id` di setiap baris; GORM global scope `WHERE tenant_id = ?` diterapkan di setiap repository; middleware memvalidasi tenant context di setiap request. Tidak ada query yang boleh lintas tenant — dibuktikan via test integration, bukan RLS (MySQL tidak mendukung RLS).
7. **Disiplin pengakuan.** Kas/termin yang diterima sebelum kriteria pengakuan terpenuhi adalah kewajiban (Uang Muka Penjualan), bukan pendapatan.
8. **RAB: hanya satu versi active per proyek/fase.** Saat RAB baru di-approve, versi lama otomatis menjadi `superseded` — tidak dihapus. Perbandingan realisasi selalu mengacu versi `active`.

## 7. Memulai (dev lokal)

> Scaffolding dibangun di **Fase 0** ROADMAP. Sampai itu jadi, ini adalah bentuk target.

```bash
# 1. Salin environment file
cp .env.example .env

# 2. Jalankan semua service development (MySQL + backend hot-reload + frontend dev)
make dev-up

# 3. Jalankan migrasi database
make migrate

# 4. (Opsional) seed data fixture LITHOS
make seed

# Akses:
#   Frontend: http://localhost:3000
#   Backend:  http://localhost:8080
#   MySQL:    localhost:3306

# Perintah lain yang berguna
make dev-logs        # tail semua log
make test            # run backend tests
make lint            # run linter
make dev-shell-db    # buka MySQL shell
make help            # lihat semua perintah
```

## 8. Glosarium istilah domain

| Istilah | Arti |
|---|---|
| **Persediaan / Aset Real Estat** | Persediaan real estat (biaya proyek yang dikapitalisasi di neraca) |
| **HPP** | Harga Pokok Penjualan = COGS |
| **Neraca** | Laporan posisi keuangan (balance sheet) |
| **Laba Rugi** | Laporan laba rugi (income statement) |
| **Arus Kas** | Laporan arus kas (cash flow statement) |
| **RAB** | Rencana Anggaran Biaya — anggaran biaya proyek sebelum/selama konstruksi |
| **Realisasi RAB** | Perbandingan biaya aktual vs RAB per kategori |
| **Uang Muka Penjualan** | Uang muka/DP pelanggan (kewajiban sampai pendapatan diakui) |
| **Termin** | Cicilan yang terkait dengan milestone konstruksi |
| **BAST** | Berita Acara Serah Terima (pemicu pengakuan point-in-time) |
| **KPR** | Kredit Pemilikan Rumah — cicilan via bank yang dilacak per unit |
| **PaymentSchedule** | Jadwal pembayaran buyer: DP, angsuran KPR/tunai, pelunasan |
| **ProjectPhase / Tahap** | Sub-divisi proyek (Tahap 1, Tahap 2, dst.) dengan unit dan RAB masing-masing |
| **BudgetPlan** | Satu versi RAB (v1, v2, ...) untuk proyek atau fase tertentu |
| **PPh Final Pengalihan** | Pajak penghasilan final atas pengalihan tanah/bangunan, 2,5% (PP 34/2016) |
| **PPN Keluaran** | PPN yang dipungut atas penjualan |
| **PKP** | Pengusaha Kena Pajak (entitas yang wajib memungut PPN) |
| **COA** | Chart of Accounts (daftar akun) |
| **PSAK 72** | Standar pengakuan pendapatan Indonesia (≈ IFRS 15) |

## 9. Bekerja di repo ini dengan Claude Code

1. Baca README ini, lalu `CLAUDE.md`, lalu `ROADMAP.md`.
2. Kerjakan **satu fase ROADMAP dalam satu waktu**, berurutan. Jangan melompat.
3. **Tulis test invariant lebih dulu**, baru implementasi (lihat §6).
4. Perlakukan `docs/posting-rules.md` sebagai kontrak jurnal apa yang dihasilkan tiap event.
5. Saat ragu soal aturan pajak atau pengakuan, tinggalkan penanda `// TODO(tax-advisor):` daripada menebak — angka yang salah lebih buruk daripada celah yang ditandai.
