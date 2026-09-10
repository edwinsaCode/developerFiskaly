"use client";

import { ComponentType, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { Input } from "@/components/ui/Input";
import { PANDUAN_NAV, PANDUAN_FLAT_NAV } from "./nav";
import { FlowSection } from "./content-flow";
import { AccountingHppSection } from "./content-accounting-hpp";
import { JournalReferenceSection } from "./content-journal-reference";
import {
  OverviewSection,
  SetupProjectSection,
  RabRealisasiSection,
  UnitBlockSection,
  BiayaSection,
  PersediaanHppSection,
  AlokasiHppSection,
  BookingSection,
  PenjualanAkadSection,
  PembayaranReceiptSection,
  KprJaminanBankSection,
  KelebihanTanahSection,
  PajakSection,
  SalesCommissionSection,
  FixedAssetSection,
  LaporanKeuanganSection,
  AccountingJurnalSection,
  TroubleshootingSection,
  GlossarySection,
} from "./content-sections";

const SECTION_COMPONENTS: Record<string, ComponentType> = {
  alur: FlowSection,
  "accounting-hpp": AccountingHppSection,
  "jurnal-referensi": JournalReferenceSection,
  overview: OverviewSection,
  "setup-project": SetupProjectSection,
  "rab-realisasi": RabRealisasiSection,
  "unit-block": UnitBlockSection,
  biaya: BiayaSection,
  "persediaan-hpp": PersediaanHppSection,
  "alokasi-hpp": AlokasiHppSection,
  booking: BookingSection,
  "penjualan-akad": PenjualanAkadSection,
  "pembayaran-receipt": PembayaranReceiptSection,
  "kpr-jaminan-bank": KprJaminanBankSection,
  "kelebihan-tanah": KelebihanTanahSection,
  pajak: PajakSection,
  "sales-commission": SalesCommissionSection,
  "fixed-asset": FixedAssetSection,
  "laporan-keuangan": LaporanKeuanganSection,
  "accounting-jurnal": AccountingJurnalSection,
  troubleshooting: TroubleshootingSection,
  glossary: GlossarySection,
};

export function PanduanView() {
  const [query, setQuery] = useState("");
  const [activeId, setActiveId] = useState(PANDUAN_FLAT_NAV[0]?.id ?? "");
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  const q = query.trim().toLowerCase();
  const filteredNav = useMemo(() => {
    if (!q) return PANDUAN_NAV;
    return PANDUAN_NAV.map((group) => ({
      ...group,
      items: group.items.filter(
        (item) => item.label.toLowerCase().includes(q) || item.keywords?.toLowerCase().includes(q)
      ),
    })).filter((group) => group.items.length > 0);
  }, [q]);

  const resultCount = filteredNav.reduce((n, g) => n + g.items.length, 0);

  const sectionRefs = useRef<Record<string, HTMLElement | null>>({});

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setActiveId(visible[0].target.id);
      },
      { rootMargin: "-96px 0px -70% 0px", threshold: 0 }
    );
    PANDUAN_FLAT_NAV.forEach((item) => {
      const el = sectionRefs.current[item.id];
      if (el) observer.observe(el);
    });
    return () => observer.disconnect();
  }, []);

  function goTo(id: string) {
    setMobileNavOpen(false);
    sectionRefs.current[id]?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  return (
    <div>
      <p className="text-xs text-text-tertiary">
        <Link href="/dashboard" className="hover:text-accent">Beranda</Link> / Panduan Sistem
      </p>

      <div className="mt-3 lg:hidden">
        <button
          type="button"
          onClick={() => setMobileNavOpen((v) => !v)}
          className="flex w-full items-center justify-between rounded-lg border border-border bg-surface px-4 py-2.5 text-sm font-medium text-text-primary"
        >
          Daftar Isi
          <svg
            width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
            strokeLinecap="round" strokeLinejoin="round"
            className={`transition-transform ${mobileNavOpen ? "rotate-180" : ""}`}
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
      </div>

      <div className="mt-4 flex flex-col gap-8 lg:flex-row lg:items-start">
        <aside
          className={`${mobileNavOpen ? "block" : "hidden"} lg:sticky lg:top-[calc(var(--header-height)+1.5rem)] lg:block lg:w-64 lg:shrink-0`}
        >
          <div className="scrollbar-thin space-y-4 rounded-lg border border-border bg-surface p-3 lg:max-h-[calc(100vh-var(--header-height)-3rem)] lg:overflow-y-auto">
            <Input
              placeholder="Cari panduan..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="Cari panduan"
            />
            {q && (
              <p className="px-1 text-xs text-text-tertiary">
                {resultCount === 0 ? "Tidak ada hasil" : `${resultCount} hasil untuk "${query}"`}
              </p>
            )}
            <nav className="space-y-4">
              {filteredNav.map((group) => (
                <div key={group.label}>
                  <p className="eyebrow mb-1.5 px-2">{group.label}</p>
                  <ul className="space-y-0.5">
                    {group.items.map((item) => {
                      const isActive = activeId === item.id;
                      return (
                        <li key={item.id}>
                          <button
                            type="button"
                            onClick={() => goTo(item.id)}
                            className={`block w-full rounded-md px-2 py-1.5 text-left text-sm transition-colors ${
                              isActive
                                ? "bg-accent-light font-semibold text-accent"
                                : "text-text-secondary hover:bg-border-subtle hover:text-text-primary"
                            }`}
                          >
                            {item.label}
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                </div>
              ))}
              {resultCount === 0 && q && (
                <p className="px-2 text-sm text-text-tertiary">Coba kata kunci lain, mis. &ldquo;akad&rdquo;, &ldquo;kpr&rdquo;, &ldquo;pajak&rdquo;.</p>
              )}
            </nav>
          </div>
        </aside>

        <div className="min-w-0 flex-1 space-y-12">
          {PANDUAN_FLAT_NAV.map((item) => {
            const Component = SECTION_COMPONENTS[item.id];
            if (!Component) return null;
            return (
              <section
                key={item.id}
                id={item.id}
                ref={(el) => {
                  sectionRefs.current[item.id] = el;
                }}
                className="scroll-mt-20"
              >
                <Component />
              </section>
            );
          })}

          <div className="border-t border-border pt-6 text-sm text-text-secondary">
            Dokumentasi lengkap &amp; referensi teknis penuh (COA lengkap, semua contoh jurnal, dan
            catatan diskrepansi) tersedia di{" "}
            <code className="rounded bg-border-subtle px-1.5 py-0.5 font-mono text-xs text-text-primary">
              docs/SYSTEM-DOCUMENTATION.md
            </code>
            .
          </div>
        </div>
      </div>
    </div>
  );
}
