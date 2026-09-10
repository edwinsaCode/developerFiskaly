# Chart of Accounts (COA) — esaProperti

COA ini adalah sumber kebenaran daftar akun untuk pengembang properti Indonesia.
Diimplementasikan di `backend/internal/ledger/coa.go` dan di-seed otomatis untuk setiap tenant baru via `SeedCOA()`.

**Konvensi:**
- Kode: `VARCHAR(20)`, format `K-NNNN` (K = kelas 1–5)
- Semua nominal disimpan sebagai `DECIMAL(20,4)` — tidak ada `float`
- **★** = akun inti mekanik real estat; wajib ada sebelum jurnal apapun bisa diposting
- **Sistem** = `is_system: true` di seed; jangan dihapus, tidak bisa dinonaktifkan via UI

---

## 1-xxxx — ASET

Saldo normal: **Debit** (bertambah di sisi Debit, berkurang di sisi Kredit).
Pengecualian: 1-4900 Akumulasi Penyusutan adalah kontra-aset — saldo normalnya **Kredit**.

| Kode   | Nama Akun                                    | Kelas | Saldo Normal | Keterangan |
|--------|----------------------------------------------|-------|--------------|------------|
| 1-1100 | Kas — Kas Besar                              | Aset  | D            | Sistem |
| 1-1200 | Kas — Petty Cash                             | Aset  | D            | Sistem |
| 1-1300 | Bank — BCA                                   | Aset  | D            | |
| 1-1400 | Bank — Mandiri                               | Aset  | D            | |
| 1-1500 | Bank — BRI                                   | Aset  | D            | |
| 1-2000 | Piutang Usaha                                | Aset  | D            | Sistem; tagihan buyer pasca-BAST |
| 1-2100 | Piutang Lain-lain                            | Aset  | D            | |
| 1-3000 | ★ Persediaan Real Estat — Tanah              | Aset  | D            | Sistem; biaya tanah yang dikapitalisasi |
| 1-3100 | ★ Persediaan Real Estat — Hard Cost          | Aset  | D            | Sistem; biaya konstruksi (produksi, sarana & prasarana, perizinan) yang dikapitalisasi |
| 1-3200 | ★ Persediaan Real Estat — Soft Cost          | Aset  | D            | Sistem; desain, legal |
| 1-3300 | ★ Persediaan Real Estat — Biaya Pembiayaan   | Aset  | D            | Sistem; bunga/biaya pinjaman yang dikapitalisasi |
| 1-4000 | Aset Tetap — Peralatan Kantor                | Aset  | D            | |
| 1-4100 | Aset Tetap — Kendaraan                       | Aset  | D            | |
| 1-4900 | Akumulasi Penyusutan                         | Aset  | **K**        | Sistem; kontra-aset — saldo normalnya Kredit |
| 1-5000 | Biaya Dibayar di Muka                        | Aset  | D            | |
| 1-5100 | PPN Masukan                                  | Aset  | D            | Pajak masukan yang belum dikreditkan |

---

## 2-xxxx — KEWAJIBAN

Saldo normal: **Kredit** (bertambah di sisi Kredit, berkurang di sisi Debit).

| Kode   | Nama Akun                                    | Kelas      | Saldo Normal | Keterangan |
|--------|----------------------------------------------|------------|--------------|------------|
| 2-1000 | Hutang Usaha                                 | Kewajiban  | K            | Sistem; tagihan vendor/kontraktor |
| 2-2000 | ★ Uang Muka Penjualan                        | Kewajiban  | K            | Sistem; DP/termin buyer **sebelum** BAST — bukan pendapatan (Invariant #7) |
| 2-3000 | PPN Keluaran                                 | Kewajiban  | K            | Sistem; PPN dipungut atas penjualan, wajib disetorkan |
| 2-4000 | ★ Hutang PPh Final Pengalihan                | Kewajiban  | K            | Sistem; PPh Final 2,5% atas pengalihan properti (PP 34/2016) |
| 2-5000 | Hutang Bank                                  | Kewajiban  | K            | KPR konstruksi / kredit modal kerja |
| 2-6000 | Biaya Akrual                                 | Kewajiban  | K            | |
| 2-6100 | Hutang Gaji & Tunjangan                      | Kewajiban  | K            | |

---

## 3-xxxx — EKUITAS

Saldo normal: **Kredit**.

| Kode   | Nama Akun                                    | Kelas   | Saldo Normal | Keterangan |
|--------|----------------------------------------------|---------|--------------|------------|
| 3-1000 | Modal Disetor                                | Ekuitas | K            | Sistem |
| 3-2000 | Laba Ditahan                                 | Ekuitas | K            | Sistem; akumulasi laba periode lalu |
| 3-3000 | Laba/Rugi Tahun Berjalan                     | Ekuitas | K            | Sistem; ditutup ke 3-2000 setiap akhir periode |

---

## 4-xxxx — PENDAPATAN

Saldo normal: **Kredit**.

| Kode   | Nama Akun                                    | Kelas      | Saldo Normal | Keterangan |
|--------|----------------------------------------------|------------|--------------|------------|
| 4-1000 | ★ Pendapatan Penjualan Unit                  | Pendapatan | K            | Sistem; diakui point-in-time saat BAST (PSAK 72) |
| 4-2000 | Pendapatan Lain-lain                         | Pendapatan | K            | |

---

## 5-xxxx — BEBAN

Saldo normal: **Debit**.

| Kode   | Nama Akun                                    | Kelas | Saldo Normal | Keterangan |
|--------|----------------------------------------------|-------|--------------|------------|
| 5-1000 | ★ Harga Pokok Penjualan (HPP)                | Beban | D            | Sistem; biaya terakumulasi unit dipindah dari Persediaan saat unit terjual |
| 5-2000 | ★ Beban PPh Final Pengalihan                 | Beban | D            | Sistem; 2,5% nilai pengalihan per unit (PP 34/2016) |
| 5-3000 | Beban Pemasaran                              | Beban | D            | |
| 5-3100 | Beban Komisi Penjualan                       | Beban | D            | |
| 5-4000 | Beban Umum & Administrasi                    | Beban | D            | G&A korporat; tidak dikapitalisasi ke proyek |
| 5-4100 | Beban Gaji & Tunjangan                       | Beban | D            | |
| 5-4200 | Beban Sewa Kantor                            | Beban | D            | |
| 5-4300 | Beban Utilitas                               | Beban | D            | |
| 5-4400 | Beban Perjalanan Dinas                       | Beban | D            | |
| 5-4500 | Beban Penyusutan                             | Beban | D            | |
| 5-5000 | Beban Bunga                                  | Beban | D            | Bunga yang **tidak** dikapitalisasi; sudah terealisasi |

---

## Ringkasan akun ★ (inti mekanik real estat)

| Kode     | Nama                              | Peran dalam mekanik |
|----------|-----------------------------------|---------------------|
| 1-3000   | Persediaan Real Estat — Tanah     | Menampung biaya tanah yang dikapitalisasi |
| 1-3100   | Persediaan Real Estat — Hard Cost | Menampung biaya konstruksi (produksi, sarana & prasarana, perizinan) yang dikapitalisasi — RULE KLIEN 2026-09-03: HPP Konstruksi = Produksi + Sarana & Prasarana + Perizinan |
| 1-3200   | Persediaan Real Estat — Soft Cost | Menampung biaya desain/legal yang dikapitalisasi (perizinan TIDAK di sini — lihat 1-3100) |
| 1-3300   | Persediaan Real Estat — Pembiayaan| Menampung bunga/biaya pinjaman yang dikapitalisasi |
| 2-2000   | Uang Muka Penjualan               | Kewajiban atas DP/termin sebelum BAST; **bukan pendapatan** |
| 2-4000   | Hutang PPh Final Pengalihan       | Kewajiban pajak final 2,5% per unit yang dialihkan |
| 4-1000   | Pendapatan Penjualan Unit         | Pendapatan diakui hanya saat BAST (PSAK 72 point-in-time) |
| 5-1000   | HPP                               | Biaya terakumulasi unit berpindah dari Persediaan ke Laba Rugi saat terjual |
| 5-2000   | Beban PPh Final Pengalihan        | Biaya pajak final yang masuk Laba Rugi saat pengalihan |

---

## Verifikasi akun yang dibutuhkan oleh posting-rules.md

Tabel di bawah memverifikasi bahwa semua akun yang dibutuhkan oleh `docs/posting-rules.md` sudah ada di `coa.go`.

| Akun yang dibutuhkan            | Status | Kode di COA |
|---------------------------------|--------|-------------|
| Kas/Bank                        | ✅ Ada | 1-1100, 1-1200, 1-1300, 1-1400, 1-1500 |
| Piutang Usaha                   | ✅ Ada | 1-2000 |
| Hutang Usaha                    | ✅ Ada | 2-1000 |
| Persediaan Real Estat           | ✅ Ada | 1-3000, 1-3100, 1-3200, 1-3300 |
| Uang Muka Penjualan             | ✅ Ada | 2-2000 |
| PPN Keluaran                    | ✅ Ada | 2-3000 |
| Hutang PPh Final                | ✅ Ada | 2-4000 |
| Modal                           | ✅ Ada | 3-1000 (Modal Disetor) |
| Pendapatan Penjualan Real Estat | ✅ Ada | 4-1000 (Pendapatan Penjualan Unit) |
| HPP                             | ✅ Ada | 5-1000 |
| Beban PPh Final Pengalihan      | ✅ Ada | 5-2000 |
| Beban Operasional / G&A         | ✅ Ada | 5-4000 (Beban Umum & Administrasi) |

**Tidak ada akun yang perlu ditambahkan.** Semua akun yang dibutuhkan sudah tersedia di `coa.go`.

---

## Total akun

| Kelas      | Jumlah |
|------------|--------|
| Aset       | 16     |
| Kewajiban  | 7      |
| Ekuitas    | 3      |
| Pendapatan | 2      |
| Beban      | 11     |
| **Total**  | **39** |
