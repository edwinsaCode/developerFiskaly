# Posting Rules — Kontrak Jurnal per Event Bisnis

Dokumen ini adalah **kontrak** antara modul bisnis (cost, sales, payment, tax) dan engine ledger.
Setiap event bisnis yang menghasilkan transaksi keuangan **wajib** mengikuti pola jurnal di sini.
Kode akun merujuk ke `docs/coa.md` (sumber: `backend/internal/ledger/coa.go`).

> ⚠️ **Bukan nasihat pajak.** Aturan PPh dan PPN di dokumen ini bersifat default ilustratif berdasarkan PP 34/2016 dan praktik umum. Perlakuan persisnya wajib dikonfirmasi ke akuntan/konsultan pajak Indonesia berlisensi sebelum go-live.

---

## Invariant yang wajib ditegakkan di setiap jurnal

Keempat aturan di bawah adalah invariant keras — `PostingService` di `backend/internal/ledger/posting_service.go` menolak jurnal yang melanggar nomor 1 dan 2:
- Pelanggaran **#1** → `ErrJournalNotBalanced` (Σ debit ≠ Σ kredit)
- Pelanggaran **#2** → `ErrAmountNotWholeRupiah` (ada pecahan sen di baris manapun); engine **menolak**, tidak auto-round

Nomor 3 dan 4 ditegakkan oleh layer bisnis sebelum jurnal dibuat.

1. **Σ debit == Σ kredit.** Setiap jurnal harus seimbang. Tidak ada pengecualian.
2. **Semua baris jurnal bertipe rupiah bulat.** Pecahan sen tidak boleh masuk ke jurnal. Alokasi biaya dibulatkan (metode largest-remainder) **sebelum** jurnal dibuat, bukan sesudah.
3. **Uang muka pra-pengakuan adalah kewajiban, bukan pendapatan.** Kas/termin yang diterima sebelum kriteria pengakuan PSAK 72 terpenuhi wajib dikreditkan ke 2-2000 Uang Muka Penjualan (kewajiban), bukan ke 4-1000 Pendapatan.
4. **HPP suatu unit = biaya terakumulasi unit itu pada saat penjualan.** HPP yang diakui tidak boleh berupa estimasi, rata-rata sederhana, atau persentase; ia harus berasal dari akumulasi biaya aktual unit tersebut yang tersimpan di akun Persediaan Real Estat.

---

## Event 1 — Kapitalisasi biaya pengembangan

**Nama:** Kapitalisasi Biaya Pengembangan
**Pemicu:** Pembayaran atau pengakuan tagihan atas biaya tanah, konstruksi (hard cost), perizinan/desain/legal (soft cost), atau bunga/biaya pinjaman yang memenuhi syarat kapitalisasi.

**Prinsip:** Biaya pengembangan **tidak masuk Laba Rugi** saat dibelanjakan. Biaya menumpuk di neraca pada akun Persediaan Real Estat sampai unit terjual. Pilih sub-akun Persediaan sesuai kategori biaya:

| Kategori biaya           | Sub-akun Persediaan yang didebit |
|--------------------------|----------------------------------|
| Biaya tanah              | 1-3000 Persediaan Real Estat — Tanah |
| Konstruksi / hard cost   | 1-3100 Persediaan Real Estat — Hard Cost |
| Perizinan / desain / legal (soft cost) | 1-3200 Persediaan Real Estat — Soft Cost |
| Bunga / biaya pinjaman yang dikapitalisasi | 1-3300 Persediaan Real Estat — Biaya Pembiayaan |

**Jurnal (bayar via bank):**

| Akun | Debit | Kredit |
|------|-------|--------|
| 1-31xx Persediaan Real Estat — [kategori] | biaya | — |
| 1-1300 Bank — BCA | — | biaya |

**Jurnal (terima tagihan, belum bayar):**

| Akun | Debit | Kredit |
|------|-------|--------|
| 1-31xx Persediaan Real Estat — [kategori] | biaya | — |
| 2-1000 Hutang Usaha | — | biaya |

**Tag wajib pada setiap baris:** `project_id`. Tambahkan `phase_id` bila biaya per fase, dan `unit_id` bila biaya dapat diatribusi langsung ke unit tertentu.

**Contoh — pembayaran kontraktor pondasi Proyek LITHOS via Bank BCA:**

| Akun | Debit | Kredit |
|------|------:|------:|
| 1-3100 Persediaan Real Estat — Hard Cost | 500.000.000 | — |
| 1-1300 Bank — BCA | — | 500.000.000 |
| **Total** | **500.000.000** | **500.000.000** |

---

## Event 2 — Penerimaan uang muka / termin (sebelum pengakuan pendapatan)

**Nama:** Penerimaan Uang Muka / Termin
**Pemicu:** Penerimaan pembayaran (DP, angsuran, atau termin konstruksi) dari buyer **sebelum** kriteria pengakuan pendapatan PSAK 72 terpenuhi, yaitu sebelum serah terima unit (BAST).

**Prinsip:** Kas masuk, tapi pendapatan **belum boleh diakui**. Penerimaan ini adalah kewajiban (Invariant #3) — dikreditkan ke 2-2000 Uang Muka Penjualan sampai BAST terjadi.

| Akun | Debit | Kredit |
|------|-------|--------|
| 1-13xx Bank — [nama bank] | jumlah diterima | — |
| 2-2000 Uang Muka Penjualan | — | jumlah diterima |

**Contoh — penerimaan DP buyer Unit Villa 05 via Bank BCA:**

| Akun | Debit | Kredit |
|------|------:|------:|
| 1-1300 Bank — BCA | 200.000.000 | — |
| 2-2000 Uang Muka Penjualan | — | 200.000.000 |
| **Total** | **200.000.000** | **200.000.000** |

---

## Event 3 — Pengakuan pendapatan saat serah terima / BAST

**Nama:** Pengakuan Pendapatan (BAST)
**Pemicu:** Penandatanganan Berita Acara Serah Terima (BAST) unit kepada buyer.
**Metode:** Point-in-time, sesuai PSAK 72 — pendapatan diakui seluruhnya pada saat BAST, bukan secara bertahap.

**Prinsip:** Seluruh Uang Muka Penjualan yang sudah diterima untuk unit ini dinolkan (dipindah dari kewajiban ke pendapatan). Sisa harga jual yang belum diterima menjadi Piutang Usaha.

Jika PKP: Piutang Usaha = sisa nilai **BRUTO** (harga belum dibayar + PPN-nya), sehingga Σ debit menutup Pendapatan + PPN Keluaran.

| Akun | Debit | Kredit |
|------|-------|--------|
| 2-2000 Uang Muka Penjualan | total uang muka unit ini | — |
| 1-2000 Piutang Usaha | sisa bruto belum dibayar | — |
| 4-1000 Pendapatan Penjualan Unit | — | harga jual (DPP) |
| 2-3000 PPN Keluaran *(jika PKP)* | — | PPN atas DPP |

> // TODO(tax-advisor): konfirmasi applicability PPN atas penjualan unit properti. Konfirmasi juga apakah PPN terutang saat pembayaran/termin (bukan ditunda ke BAST) — bila ya, pola Event 2 berubah.

**Contoh A — BAST Unit Villa 05 (bukan PKP, harga jual Rp 1.000.000.000, uang muka diterima Rp 800.000.000, sisa piutang Rp 200.000.000):**

| Akun | Debit | Kredit |
|------|------:|------:|
| 2-2000 Uang Muka Penjualan | 800.000.000 | — |
| 1-2000 Piutang Usaha | 200.000.000 | — |
| 4-1000 Pendapatan Penjualan Unit | — | 1.000.000.000 |
| **Total** | **1.000.000.000** | **1.000.000.000** |

**Contoh B — BAST Unit Villa 05 (PKP, DPP Rp 1.000.000.000, PPN efektif 11% = Rp 110.000.000, bruto Rp 1.110.000.000, uang muka diterima Rp 800.000.000, sisa Piutang bruto Rp 310.000.000):**

| Akun | Debit | Kredit |
|------|------:|------:|
| 2-2000 Uang Muka Penjualan | 800.000.000 | — |
| 1-2000 Piutang Usaha | 310.000.000 | — |
| 4-1000 Pendapatan Penjualan Unit | — | 1.000.000.000 |
| 2-3000 PPN Keluaran | — | 110.000.000 |
| **Total** | **1.110.000.000** | **1.110.000.000** |

> Titik ajar: Piutang Rp 310 juta adalah **bruto** (bukan Rp 200 juta neto). Buyer berutang harga jual neto Rp 200 juta + PPN Rp 110 juta = Rp 310 juta.

**Catatan tarif PPN:**

> Tarif PPN dalam Contoh B (11%) adalah **ILUSTRASI** untuk barang/jasa non-mewah (mekanisme PMK 131/2024: 12% × DPP nilai lain 11/12 = efektif 11%).
> Tarif **TIDAK boleh di-hardcode** di kode — wajib jadi aturan bertanggal & per-kategori (lihat ROADMAP Phase 7).
>
> // TODO(tax-advisor): tentukan apakah villa LITHOS tergolong **HUNIAN MEWAH**.
> Jika ya → PPN 12% PENUH dari harga jual (bukan 11/12). Jika tidak → efektif 11%.
> Ini mengubah angka PPN secara material; konfirmasi sebelum go-live.

---

## Event 4 — Pengakuan HPP (bersamaan dengan Event 3)

**Nama:** Pengakuan Harga Pokok Penjualan
**Pemicu:** Sama dengan Event 3 (BAST unit yang sama). Jurnal HPP **selalu diposting bersamaan** dengan jurnal pendapatan — tidak boleh terpisah.

**Prinsip:** HPP = biaya terakumulasi unit pada saat BAST (Invariant #4). Biaya tersebut dipindahkan dari neraca (Persediaan Real Estat) ke Laba Rugi (HPP). Sisi kredit dipisah per sub-akun Persediaan sesuai komposisi biaya terakumulasi unit.

| Akun | Debit | Kredit |
|------|-------|--------|
| 5-1000 Harga Pokok Penjualan (HPP) | total biaya terakumulasi unit | — |
| 1-3000 Persediaan Real Estat — Tanah | — | porsi tanah unit |
| 1-3100 Persediaan Real Estat — Hard Cost | — | porsi hard cost unit |
| 1-3200 Persediaan Real Estat — Soft Cost | — | porsi soft cost unit |
| 1-3300 Persediaan Real Estat — Biaya Pembiayaan | — | porsi pembiayaan unit |

**Contoh — HPP Unit Villa 05 (biaya terakumulasi Rp 650.000.000, seluruhnya hard cost untuk penyederhanaan):**

| Akun | Debit | Kredit |
|------|------:|------:|
| 5-1000 Harga Pokok Penjualan (HPP) | 650.000.000 | — |
| 1-3100 Persediaan Real Estat — Hard Cost | — | 650.000.000 |
| **Total** | **650.000.000** | **650.000.000** |

> Pada kasus nyata, sisi kredit biasanya terbagi ke beberapa sub-akun 1-3xxx sesuai komposisi biaya terakumulasi unit. Jumlah kredit total tetap harus sama persis dengan debit HPP.

---

## Event 5 — PPh Final Pengalihan 2,5% (PP 34/2016)

**Nama:** PPh Final Pengalihan Hak atas Tanah dan/atau Bangunan
**Pemicu:** Pengalihan hak atas unit properti (termasuk BAST yang mengakibatkan pengalihan hak).
**Dasar hukum:** PP 34/2016 — tarif 2,5% atas nilai pengalihan bruto.

> // TODO(tax-advisor): konfirmasi bahwa basis pengenaan adalah nilai pengalihan bruto (bukan DPP PPN), dan bahwa pajak ini bersifat final (tidak dapat dikreditkan). Konfirmasi juga perlakuan untuk struktur HGB-80/leasehold ke pembeli asing.

**Jurnal 5a — saat pengalihan (akrual kewajiban pajak):**

| Akun | Debit | Kredit |
|------|-------|--------|
| 5-2000 Beban PPh Final Pengalihan | 2,5% × nilai pengalihan | — |
| 2-4000 Hutang PPh Final Pengalihan | — | 2,5% × nilai pengalihan |

**Jurnal 5b — saat pelunasan ke kas negara:**

| Akun | Debit | Kredit |
|------|-------|--------|
| 2-4000 Hutang PPh Final Pengalihan | jumlah yang dibayar | — |
| 1-13xx Bank — [nama bank] | — | jumlah yang dibayar |

**Contoh — PPh Final atas pengalihan Unit Villa 05 (nilai pengalihan Rp 1.000.000.000):**

*Saat pengalihan:*

| Akun | Debit | Kredit |
|------|------:|------:|
| 5-2000 Beban PPh Final Pengalihan | 25.000.000 | — |
| 2-4000 Hutang PPh Final Pengalihan | — | 25.000.000 |
| **Total** | **25.000.000** | **25.000.000** |

*Saat setor ke kas negara via Bank BCA:*

| Akun | Debit | Kredit |
|------|------:|------:|
| 2-4000 Hutang PPh Final Pengalihan | 25.000.000 | — |
| 1-1300 Bank — BCA | — | 25.000.000 |
| **Total** | **25.000.000** | **25.000.000** |

---

## Event 6 — Jurnal pembalik (reversing entry)

**Nama:** Jurnal Pembalik / Koreksi
**Pemicu:** Kebutuhan koreksi atas jurnal yang sudah diposting — salah akun, salah nominal, salah periode, atau pembatalan transaksi.

**Prinsip — jalur immutability (Invariant #5):**
- Jurnal yang sudah diposting bersifat **immutable** — tidak boleh diedit atau dihapus.
- Koreksi dilakukan dengan membuat jurnal baru yang **membalik** seluruh debit dan kredit jurnal asal.
- Setelah pembalikan, posting jurnal pengganti yang benar (bila diperlukan).
- `PostingService.Reverse()` menangani ini: ia membuat dan langsung memposting jurnal pembalik, serta mencatat `reverses_id` untuk audit trail.

**Pola jurnal pembalik:**

| Akun | Debit | Kredit |
|------|-------|--------|
| [akun yang semula di-kredit di jurnal asal] | nominal asal | — |
| [akun yang semula di-debit di jurnal asal] | — | nominal asal |

Dengan kata lain: **setiap debit menjadi kredit, setiap kredit menjadi debit**, nominal sama persis.

**Contoh — membalik kapitalisasi biaya konstruksi yang salah di-input (Rp 500.000.000):**

*Jurnal asal yang sudah terposting (tidak boleh diubah):*

| Akun | Debit | Kredit |
|------|------:|------:|
| 1-3100 Persediaan Real Estat — Hard Cost | 500.000.000 | — |
| 2-1000 Hutang Usaha | — | 500.000.000 |

*Jurnal pembalik (dibuat via `POST /ledger/journals/:id/reverse`):*

| Akun | Debit | Kredit |
|------|------:|------:|
| 2-1000 Hutang Usaha | 500.000.000 | — |
| 1-3100 Persediaan Real Estat — Hard Cost | — | 500.000.000 |
| **Total** | **500.000.000** | **500.000.000** |

Setelah pembalikan, posting jurnal baru yang benar (bila memang ada biaya yang valid).

---

## Alur event untuk satu unit (ringkasan)

```
[Konstruksi berjalan]
  Event 1: Dr Persediaan (1-31xx) / Cr Hutang Usaha (2-1000)
  Event 1: Dr Persediaan (1-31xx) / Cr Hutang Usaha (2-1000)
  ... (berulang per tagihan/biaya)

[Buyer bayar DP dan termin]
  Event 2: Dr Bank (1-13xx) / Cr Uang Muka Penjualan (2-2000)
  Event 2: Dr Bank (1-13xx) / Cr Uang Muka Penjualan (2-2000)
  ... (berulang per termin)

[Serah terima / BAST]
  Event 3: Dr Uang Muka Penjualan (2-2000) + Dr Piutang (1-2000)
             / Cr Pendapatan (4-1000) [+ Cr PPN Keluaran (2-3000) jika PKP]
  Event 4: Dr HPP (5-1000) / Cr Persediaan (1-31xx)   ← bersamaan dengan Event 3
  Event 5a: Dr Beban PPh Final (5-2000) / Cr Hutang PPh Final (2-4000)

[Setoran pajak]
  Event 5b: Dr Hutang PPh Final (2-4000) / Cr Bank (1-13xx)

[Koreksi bila perlu]
  Event 6: Jurnal pembalik → jurnal baru yang benar
```
