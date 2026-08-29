"use client";

// PS-5 — Accounting Workspace: satu bar navigasi konsisten untuk seluruh
// halaman akuntansi inti (workflow akuntan, bukan halaman terpisah-pisah).

import Link from "next/link";
import { usePathname } from "next/navigation";

const TABS = [
  { href: "/accounting/jurnal", label: "Jurnal" },
  { href: "/accounting/gl", label: "Buku Besar" },
  { href: "/laporan?tab=trial", label: "Neraca Saldo", match: "__never__" },
  { href: "/accounting/coa", label: "Daftar Akun" },
  { href: "/accounting/opening-balance", label: "Saldo Awal" },
  // W-7 — rincian piutang dari proyek sebelum sistem ini dipakai. Bertetangga
  // dengan Saldo Awal & Laporan Historis: ketiganya menjawab pertanyaan yang
  // sama, "apa yang sudah ada sebelum buku ini dibuka?", dan dikerjakan admin
  // dalam satu duduk saat onboarding.
  { href: "/accounting/legacy-ar", label: "Piutang Proyek Lama" },
  // W-11 — hutang usaha & masternya. Ditempatkan sesudah Saldo Awal karena
  // keduanya pekerjaan akuntan, bukan pekerjaan penagihan.
  { href: "/accounting/hutang", label: "Hutang Usaha" },
  // Pembayaran berdiri sendiri di sebelah Hutang Usaha: pengakuan kewajiban dan
  // pengeluaran kas adalah dua peristiwa berbeda, dan sering dikerjakan orang
  // yang berbeda pula.
  { href: "/accounting/pembayaran-vendor", label: "Pembayaran Vendor" },
  { href: "/accounting/vendor", label: "Master Vendor" },
  { href: "/accounting/recurring", label: "Jurnal Berulang" },
  // Dokumen = register harian (nomor apa saja yang sudah terbit), bukan setelan
  // penomoran — yang itu tetap di /pengaturan.
  { href: "/accounting/dokumen", label: "Dokumen" },
  { href: "/accounting/periods", label: "Periode & Tutup Buku" },
  // W-6 — laporan tahun sebelum sistem dipakai. Sengaja bertetangga dengan
  // Saldo Awal: keduanya soal "keadaan sebelum buku ini dibuka".
  { href: "/accounting/historis", label: "Laporan Historis" },
  { href: "/pajak", label: "Pajak" },
];

export function AccountingNav() {
  const pathname = usePathname();
  return (
    <div className="mb-5 flex gap-1 overflow-x-auto scrollbar-none border-b border-border" role="tablist">
      {TABS.map((t) => {
        const base = t.match ?? t.href.split("?")[0];
        const active = pathname === base || pathname.startsWith(base + "/");
        return (
          <Link
            key={t.href}
            href={t.href}
            className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm transition-colors
              ${active
                ? "border-accent font-medium text-accent"
                : "border-transparent text-text-secondary hover:text-accent hover:border-accent/30"}`}
          >
            {t.label}
          </Link>
        );
      })}
    </div>
  );
}
