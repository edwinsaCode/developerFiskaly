import { DataTable, Lead, P, SectionHeader } from "./blocks";

// Jurnal Referensi Cepat — satu tabel scan-cepat untuk transaksi utama.
// Nominal & akun konsisten dengan contoh di Alur End-to-End Bisnis dan
// docs/SYSTEM-DOCUMENTATION.md §15.

interface RefRow {
  transaksi: string;
  debit: string;
  kredit: string;
  contoh: string;
}

const ROWS: RefRow[] = [
  { transaksi: "Realisasi biaya tanah (pool)", debit: "1-3000 Persediaan Tanah", kredit: "1-1300 Bank / 2-1000 Hutang Usaha", contoh: "Rp 900.000.000" },
  { transaksi: "Realisasi hard cost — Produksi Subsidi", debit: "1-3100 Persediaan Hard Cost", kredit: "1-1300 Bank / 2-1000 Hutang Usaha", contoh: "Rp 200.000.000" },
  { transaksi: "Realisasi Pemasaran", debit: "6-xxxx Beban Pemasaran", kredit: "1-1300 Bank / 2-1000 Hutang Usaha", contoh: "Beban langsung, tidak masuk Persediaan" },
  { transaksi: "Booking fee diterima", debit: "1-1300 Bank", kredit: "4-2100 Pendapatan Booking", contoh: "Rp 5.000.000" },
  { transaksi: "DP / uang muka sebelum Akad", debit: "1-1300 Bank", kredit: "2-2000 Uang Muka Penjualan", contoh: "Rp 10.000.000" },
  { transaksi: "Akad — pengakuan pendapatan", debit: "2-2000 Uang Muka + 1-2200 Dana Jaminan Bank + 1-2000 Piutang Usaha", kredit: "4-1000 Pendapatan Penjualan Unit", contoh: "Rp 180.000.000" },
  { transaksi: "Akad — pengakuan HPP", debit: "5-1000 Beban Pokok Penjualan", kredit: "1-3000 Persediaan Tanah + 1-3100 Persediaan Hard Cost", contoh: "Rp 115.000.000" },
  { transaksi: "Pencairan KPR bertahap", debit: "1-1400 Bank (rekening pencairan)", kredit: "1-2200 Dana Jaminan Bank", contoh: "Rp 100.000.000" },
  { transaksi: "True-Up HPP (koreksi ke biaya aktual final)", debit: "5-1000 Beban Pokok Penjualan", kredit: "1-3000 / 1-3100 Persediaan", contoh: "Rp 15.000.000" },
  { transaksi: "Kelebihan Tanah — HPP saat terjual", debit: "5-1000 Beban Pokok Penjualan", kredit: "1-3000 Persediaan Tanah (Kelebihan Tanah)", contoh: "land_area × harga beli/m²" },
  { transaksi: "Komisi sales (Akad selesai)", debit: "6-xxxx Beban Komisi Sales", kredit: "2-1000 Hutang Komisi / 1-1300 Bank", contoh: "2% × harga jual, mis. Rp 3.600.000" },
  { transaksi: "PPh Final atas penjualan (subsidi)", debit: "6-xxxx Beban PPh Final", kredit: "2-1000 Hutang PPh Final", contoh: "1% × DPP" },
  { transaksi: "Kwitansi / penerimaan pembayaran termin", debit: "1-1300 Bank", kredit: "1-2000 Piutang Usaha (atau 2-2000 bila pra-Akad)", contoh: "sesuai nominal termin" },
  { transaksi: "Penyusutan aset tetap (bulanan)", debit: "6-xxxx Beban Penyusutan", kredit: "1-6xxx Akumulasi Penyusutan", contoh: "sesuai umur manfaat" },
  { transaksi: "Tutup buku tahunan", debit: "4-xxxx / 3-xxxx Ekuitas", kredit: "Saldo P&L periode berjalan → Laba Ditahan", contoh: "Laba/rugi tahun berjalan" },
];

export function JournalReferenceSection() {
  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow="Referensi Inti"
        title="Jurnal Referensi Cepat"
        lead={
          <Lead>
            Tabel ringkas Debit/Kredit untuk transaksi paling sering terjadi. Untuk penjelasan
            lengkap tiap transaksi (kapan terjadi, dampak Neraca &amp; Laba/Rugi), lihat langkah
            terkait di Alur End-to-End Bisnis atau section modul masing-masing.
          </Lead>
        }
      />
      <DataTable
        head={["Transaksi", "Debit", "Kredit", "Contoh Nominal"]}
        rows={ROWS.map((r) => [r.transaksi, r.debit, r.kredit, r.contoh])}
      />
      <P>
        Kode akun mengikuti format <code className="font-mono">segmen-nomor</code> (mis.{" "}
        <code className="font-mono">1-3000</code> = kelompok Aset, Persediaan Tanah). Daftar lengkap
        akun ada di Chart of Accounts pada modul Accounting.
      </P>
    </div>
  );
}
