"use client";

// PRODUCT HARDENING — Pengaturan: satu tempat untuk seluruh master data
// operasional. Menutup gap "master data tidak bisa dibuat dari UI":
// sales person, skema pembayaran, bank pembiayaan, pelanggan, pengguna.
// Master akuntansi (COA, Pajak) tetap di workspace-nya — ditautkan di sini.

import Link from "next/link";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/components/ui/Tabs";
import { Card } from "@/components/ui/Card";
import { AutoGrid } from "@/components/ui/AutoGrid";
import { UsersSection } from "./UsersSection";
import { SalesSection } from "./SalesSection";
import { SchemesSection } from "./SchemesSection";
import { ProductTypesSection } from "./ProductTypesSection";
import { RealizationChargeTypesSection } from "./RealizationChargeTypesSection";
import { DocumentNumberingSection } from "./DocumentNumberingSection";
import { CustomersSection } from "./CustomersSection";
import { BillingPolicySection } from "./BillingPolicySection";

const ACCOUNTING_LINKS = [
  {
    href: "/accounting/coa",
    title: "Daftar Akun (COA)",
    desc: "Kelola bagan akun — kas/bank, piutang, pendapatan, beban",
  },
  {
    href: "/accounting/opening-balance",
    title: "Saldo Awal",
    desc: "Input saldo awal migrasi dari sistem lama",
  },
  {
    href: "/accounting/periods",
    title: "Periode & Tutup Buku",
    desc: "Kunci periode dan proses tutup buku tahunan",
  },
  {
    href: "/pajak",
    title: "Pajak",
    desc: "PPh Final & PPN — tarif mengikuti kategori proyek (subsidi/komersial)",
  },
  {
    href: "/penjualan/komisi",
    title: "Aturan Komisi",
    desc: "Skema komisi sales — flat / persentase per skema pembayaran",
  },
];

export function SettingsHub({ token }: { token: string }) {
  return (
    <Tabs defaultTab="sales">
      <TabList>
        <TabTrigger id="sales">Sales & Tim</TabTrigger>
        <TabTrigger id="schemes">Skema & Bank</TabTrigger>
        <TabTrigger id="products">Katalog Produk</TabTrigger>
        {/* W-1: pasangan dari Katalog Produk — yang DIJUAL (pendapatan) vs yang
            DITITIPKAN (kewajiban). Sengaja bersebelahan agar admin tidak salah
            menaruh biaya pihak ketiga sebagai produk. */}
        <TabTrigger id="charge-types">Biaya Realisasi</TabTrigger>
        {/* W-2: bentuk nomor kwitansi/invoice kini master data, bukan konstanta
            di program. Ditaruh di deret master agar setara dengan yang lain. */}
        <TabTrigger id="documents">Penomoran Dokumen</TabTrigger>
        <TabTrigger id="customers">Pelanggan</TabTrigger>
        <TabTrigger id="users">Pengguna</TabTrigger>
        <TabTrigger id="accounting">Akuntansi & Lainnya</TabTrigger>
      </TabList>

      <TabPanel id="sales" className="pt-4">
        <SalesSection token={token} />
      </TabPanel>
      <TabPanel id="schemes" className="pt-4">
        <SchemesSection token={token} />
      </TabPanel>
      <TabPanel id="products" className="pt-4">
        <ProductTypesSection token={token} />
      </TabPanel>
      <TabPanel id="charge-types" className="pt-4">
        <RealizationChargeTypesSection token={token} />
      </TabPanel>
      <TabPanel id="documents" className="pt-4">
        <DocumentNumberingSection token={token} />
      </TabPanel>
      <TabPanel id="customers" className="pt-4">
        <CustomersSection token={token} />
      </TabPanel>
      <TabPanel id="users" className="pt-4">
        <UsersSection token={token} />
      </TabPanel>
      <TabPanel id="accounting" className="pt-4">
        <div className="space-y-4">
          {/* Kebijakan BAST. Sejak W-5 syarat "biaya realisasi lunas" dicabut
              (D-3), jadi bagian ini baca saja: yang tersisa adalah penjelasan
              gate yang masih berlaku + jejak audit perubahan kebijakan (R-A). */}
          <BillingPolicySection token={token} />
          <AutoGrid count={ACCOUNTING_LINKS.length} max={3} gap="sm">
            {ACCOUNTING_LINKS.map((l) => (
              <Link key={l.href} href={l.href} className="block h-full">
                <Card padding="sm" className="h-full transition-colors hover:border-accent">
                  <p className="text-sm font-semibold">{l.title}</p>
                  <p className="text-xs text-text-secondary mt-1">{l.desc}</p>
                </Card>
              </Link>
            ))}
          </AutoGrid>
        </div>
      </TabPanel>
    </Tabs>
  );
}
