# Fixed Asset & Depreciation — Audit Kesiapan + Roadmap (UAT Batch 2 §5)

> 2026-07-30 · **KEPUTUSAN: belum dibutuhkan flow inti penjualan → roadmap saja.**
> Tidak ada perubahan kode modul penjualan.

## Audit kesiapan hari ini
- ✅ COA sudah punya struktur aset tetap: `1-4xxx` (aset tetap) + `1-4900`
  (akumulasi depresiasi — kontra-aset, sudah ditangani `ComputeNeraca`).
- ✅ Laporan arus kas sudah mengklasifikasikan gerakan `1-4%` (≠1-4900) sebagai
  aktivitas INVESTASI (`reporting.GetCashMovements`).
- ✅ Jurnal manual + periode tutup buku sudah ada → pembelian aset & depresiasi
  MANUAL sudah bisa dicatat hari ini tanpa modul (Dr 1-4xxx/Cr Bank; depresiasi
  Dr 5-xxxx Beban Depresiasi / Cr 1-4900) — tidak menyentuh modul penjualan.
- ❌ Belum ada: register aset (master), jadwal depresiasi otomatis, jurnal
  depresiasi berkala, disposal/penjualan aset, laporan daftar aset.

## Roadmap modul (fase terpisah, tidak mengganggu penjualan)
1. **FA-1 Register Aset**: tabel `fixed_assets` (kode, nama, kategori, tanggal
   perolehan, biaya perolehan, umur manfaat, metode — garis lurus dulu, akun
   aset/akumulasi/beban dari COA mapping per kategori), link jurnal perolehan.
2. **FA-2 Depreciation Run**: job bulanan idempoten (pola recurring journal
   PS-5 yang SUDAH ada — `last_run_ym`): hitung depresiasi garis lurus
   (decimal, largest-remainder utk bulan terakhir), posting Dr Beban / Cr 1-4900
   per aset; satu run = satu jurnal ber-referensi.
3. **FA-3 Disposal**: jual/hapus aset — reversal akumulasi + laba/rugi pelepasan
   (akun via COA). Append-only.
4. **FA-4 Laporan**: daftar aset + nilai buku (derived: perolehan − akumulasi
   ledger, via AccountRoleRegistry role baru `fixed_asset`/`accum_depreciation`)
   + rekonsiliasi register ↔ saldo 1-4xxx (pola EQ suite).
Prasyarat teknis sudah tersedia semua (posting service, period guard, recurring
engine, registry, suite). Estimasi: FA-1+FA-2 satu increment; FA-3+FA-4 menyusul.
`TODO(tax-advisor): umur manfaat & metode fiskal vs komersial (beda depresiasi).`
