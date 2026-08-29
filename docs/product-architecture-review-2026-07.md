# Product Architecture Review — Roadmap Berbasis Feedback Klien

> 2026-07-22 · Principal Product Architect review · **APPROVED (roadmap R1–R6) — R1 SHIPPED, lihat product-sprint-6-r1-kpr.md.** Design §4 (booking fee) masih menunggu approval terpisah.
> Semua temuan diverifikasi terhadap kode aktual (path per klaim). Menggantikan urutan roadmap di product-audit-2026-07.md.

---

## 1. Audit per feedback klien — apa kata KODE

### F1 · Booking fee di luar harga unit

**Kondisi kode sekarang** (`internal/sale/booking.go`, `booking_service.go`):
- Fee diterima → `Dr Bank / Cr 2-2100 Titipan Booking` ✅ (lifecycle sendiri: `held / transferred / forfeited / pending_refund`).
- Hangus ✅ (`Dr 2-2100 / Cr 4-2000`). Refund ✅.
- **TAPI saat konversi ke kontrak, fee SELALU direklas menjadi bagian pembayaran** (`Dr 2-2100 / Cr 2-2000` + baris termin yang ikut dihitung `SumTerminsByUnit` → mengurangi Terutang). Terverifikasi runtime: kontrak 250jt + fee 5jt → sisa tagihan 245jt.
- Artinya semantik hari ini = "booking fee adalah bagian DP". Semantik klien ("di luar harga, kontrak tetap 500jt") **tidak didukung**.

**Klasifikasi: MENYENTUH POSTING AKUNTANSI** → design review di §4 dokumen ini; implementasi digerbang approval terpisah.

### F2 · KPR Realization (prioritas tertinggi)

**Kondisi kode sekarang:**
| Tahap flow klien | Status | Bukti |
|---|---|---|
| Booking → Kontrak | ✅ | Increment 7 |
| Pengajuan KPR | ✅ | state `submitted_to_bank` (`scheme/types.go:57`) |
| SP3K | ✅ (beda nama) | state `bank_approved` — **hanya perlu relabel UI "SP3K Terbit"** |
| Akad | ✅ | state `akad` + UI milestone |
| Dana cair — STATE | ✅ | state `disbursed` + tombol UI |
| Dana cair — UANG | ⚠️ engine ✅, flow ❌ | `POST /collections/payment` dengan `financing_source_id` → source `kpr_disbursement` → `ReceivePayment` (jurnal Event 2 biasa, kwitansi otomatis) — **`handler.go:264`, SUDAH JALAN**. Tapi UI tidak pernah mengirim `financing_source_id`; tombol "Catat Pencairan Dana" hanya mengubah state tanpa uang |
| Nominal cair ≠ plafon | ✅ otomatis | H-2: `ContractFinancialSummary` — 500jt, cair 430, buyer 50 → diterima 480, Terutang 20 (ADA TEST-nya, `financial_summary_test.go`) |
| Invoice Kekurangan otomatis | ❌ | Tipe invoice hanya `DP/TERMIN/PELUNASAN` (`billing/model.go:15`) — tidak ada generator dari Outstanding |
| Guard urutan (cair sebelum akad?) | ❌ | Tidak ada validasi state saat source `kpr_disbursement` (grep `akad` di `collection.go` kosong) — integritas flow, bukan jurnal |
| KPR pipeline view | ❌ | Tidak ada agregat `scheme_state` di reporting mana pun |

**Kesimpulan: TANPA workaround engine SUDAH menghitung benar** (pencairan = termin biasa; outstanding satu rumus). Gap-nya: orkestrasi flow (atomik uang+state), invoice kekurangan, guard, dan view. **Semua additive; TIDAK menyentuh jurnal** (pakai Event 2 existing).

### F3 · Progress fisik vs biaya vs collection

- Progress **biaya** ✅ (ledger), **collection** ✅, **fisik ❌ tidak dimodelkan sama sekali** (grep kosong).
- Rancangan: `project_progress_entries` append-only (project_id, phase_id?, progress_pct, as_of_date, notes, created_by, created_at) — **operasional, BUKAN ledger, bukan duplicate SoT** (tidak ada angka keuangan di dalamnya). Kartu proyek: `Fisik 40% · Biaya 65% · Collection 20% → ⚠️ biaya mendahului fisik`.
- Klasifikasi: **backend additive murni + UI**.

### F4 · Dashboard

Setuju dengan klien: **dashboard belakangan**. Seksi KPR, profit per unit, forecast — semua butuh data R1–R3 benar dulu. Tidak ada pekerjaan dashboard sampai data sprint sebelumnya rampung.

### F5 · Customer Statement (satu halaman perjalanan customer)

**Sudah ada** `GET /sale-contracts/{id}/statement` + halaman `/accounting/receivable/[contractId]` — tapi isinya: nilai kontrak, total bayar, sisa, jadwal + overdue (`statement.go:37`). **Belum ada:** harga unit & diskon (sudah tersedia di summary H-2, tinggal dirangkai), rincian pembayaran per sumber (booking fee / DP / manual / **pencairan bank** — data ada di `termin_payments.payment_source`), daftar invoice, link kwitansi, timeline milestone KPR (data ada di scheme events), info kontrak.
**Klasifikasi: backend additive kecil (perluas payload dari sumber yang SEMUA sudah ada) + penyusunan ulang UI.** Tidak menyentuh jurnal.

## 2. Matriks klasifikasi gap

| Gap | Hanya UI | Backend additive | Sentuh jurnal |
|---|---|---|---|
| Relabel SP3K | ✔ | | |
| Modal pencairan (nominal+bank) | ✔ (endpoint ada) | | |
| Atomik pencairan uang+state | | ✔ (orkestrasi service; state bukan jurnal) | |
| Guard cair-setelah-akad | | ✔ (validasi) | |
| Invoice KEKURANGAN | | ✔ (tipe+generator dari summary H-2) | |
| KPR pipeline board | | ✔ (reporting read-only) | |
| Statement 360 | | ✔ (perluas payload) | |
| Progress fisik | | ✔ (entitas baru non-ledger) | |
| Dashboard 2.0 | | ✔ (reporting read-only) | |
| **Booking fee di luar harga** | | | **✔ (cabang posting konversi + rumus TotalPaid)** |

## 3. Penolakan / koreksi requirement (dengan alasan)

1. **"Sistem OTOMATIS membuat Invoice Kekurangan"** — ditolak dalam bentuk full-otomatis. Pencairan bisa parsial/bertahap; auto-invoice pada setiap pencairan menghasilkan dokumen tagihan prematur yang lalu harus dibatalkan (dokumen bernomor = jejak audit kotor). **Alternatif benar:** setelah pencairan tercatat, sistem MENAWARKAN draft "Invoice Kekurangan Rp X" satu klik (default), dengan opsi kebijakan per-tenant `auto_shortfall_invoice = true` bagi yang mau otomatis penuh. Angka SELALU dari `ContractFinancialSummary` (satu rumus).
2. **"Booking fee bukan DP" sebagai default baru** — ditolak untuk data berjalan. Kontrak/booking lama sudah diposting dengan semantik lama; mengubah default = mengubah makna histori (melanggar append-only). **Alternatif:** kebijakan per-booking `fee_policy: toward_price | outside_price`, default `toward_price` (perilaku lama), default tenant bisa diset `outside_price` untuk booking BARU. Histori tidak tersentuh.

## 4. Design Review — Booking Fee di Luar Harga (gerbang approval terpisah)

**Prinsip:** jurnal lama tak diubah; hanya CABANG BARU pada konversi untuk booking berkebijakan `outside_price`.

- Kolom additive: `bookings.fee_policy ENUM('toward_price','outside_price') DEFAULT 'toward_price'`; `termin_payments.counts_toward_price BOOL NOT NULL DEFAULT TRUE`.
- Terima fee (kedua kebijakan, TIDAK berubah): `Dr Bank / Cr 2-2100`.
- Konversi booking `outside_price`: fee **TIDAK** direklas ke 2-2000 dan termin fee ditandai `counts_toward_price = FALSE`. Fee tetap kewajiban 2-2100 sampai disposisi:
  - **Jadi biaya administrasi**: `Dr 2-2100 / Cr 4-2100 Pendapatan Administrasi` (akun baru via COA master, bukan hardcode). TODO(tax-advisor): perlakuan PPN atas fee administrasi.
  - **Hangus**: jalur existing 4-2000. **Refund**: jalur refund existing.
- `ContractFinancialSummary.TotalPaid` → `SUM(amount) WHERE counts_toward_price = TRUE` (default TRUE = histori & perilaku lama 100% identik; satu rumus tetap satu).
- Invariant baru: kontrak `outside_price` → GrossAmount penuh ditagih; fee tidak pernah menyentuh 2-2000/4-1000.
- Risiko: menyentuh `convertBookingWithContract` (tx atomik konversi) — wajib integration test balanced + test summary exclude.

## 5. ROADMAP BARU (urutan = nilai bisnis)

| # | Sprint | Isi | Alasan urutan |
|---|---|---|---|
| **R1** | **KPR Realization** | Modal "Catat Pencairan" (nominal, bank, tanggal, ref) → SATU aksi atomik: termin `kpr_disbursement` + state `disbursed`; guard akad; tawaran Invoice KEKURANGAN (tipe baru) dari summary H-2; KPR pipeline board (agregat scheme_state + Σ plafon vs Σ cair); relabel SP3K | Prioritas tertinggi klien; fondasi H-2 sudah siap → effort kecil, nilai maksimum; TANPA sentuh jurnal |
| **R2** | **Customer Statement 360** | Perluas payload statement: summary H-2 (harga/diskon/terutang), pembayaran per sumber (booking/DP/manual/**pencairan bank**), invoice, kwitansi, timeline KPR; satu halaman UI | Melengkapi R1 (pencairan langsung terlihat di perjalanan customer); semua sumber data sudah ada; dokumen yang dipegang klien saat menghadap customer |
| **R3** | **Progress Fisik & Project Control** | Entitas append-only + input UI + kartu Fisik/Biaya/Collection + alert selisih | Permintaan eksplisit owner; additive murni; independen dari R1/R2 |
| **R4** | **Booking Fee Outside Price** | Implementasi §4 SETELAH design review di-approve | Menyentuh posting → digerbang; nilai tinggi tapi risiko tertinggi, jangan campur dengan sprint lain |
| **R5** | **Executive Dashboard 2.0** | Seksi KPR (data R1), fisik-vs-biaya (R3), outstanding total, forecast kas, top penunggak, profit per unit, margin waterfall | Sesuai arahan klien: dashboard SETELAH data benar — sekarang tinggal membaca |
| **R6** | UX batch & Enterprise design | auto-schedule dari skema, invoice page, command palette, mobile | Pengurangan beban kerja setelah flow inti benar |

Estimasi ketergantungan: R1→R2 (statement menampilkan pencairan), R1+R3→R5 (dashboard membaca keduanya). R4 independen, bisa paralel setelah approve design.

## 6. Risiko

1. **R4 satu-satunya yang menyentuh posting** — terisolasi, digerbang approval, wajib test balanced + summary.
2. R1 atomik uang+state: state milestone BUKAN jurnal — jika update state gagal pasca-termin, termin tetap sah (uang benar); retry state idempoten. Didokumentasikan di implementasi.
3. R3 kualitas input manual — mitigasi: append-only + audit siapa/kapan + "terakhir update X hari".
4. R5 beban query komposit — ukur; pecah per-seksi bila perlu.
