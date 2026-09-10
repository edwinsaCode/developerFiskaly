"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useUserSafe } from "@/lib/context/UserContext";
import { ROLE_LABELS } from "@/lib/roles";
import { BRAND_NAME, BRAND_PREFIX, BRAND_ACCENT, BRAND_TAGLINE } from "@/lib/brand";

interface NavItem {
  href: string;
  label: string;
  icon: string;
  disabled?: boolean;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

const NAV_GROUPS: NavGroup[] = [
  {
    label: "OPERASIONAL",
    items: [
      {
        href: "/dashboard",
        label: "Dashboard",
        icon: "M3 9l9-7 9 7v11a2 2 0 01-2 2H5a2 2 0 01-2-2z",
      },
      {
        href: "/proyek",
        label: "Proyek",
        icon: "M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z",
      },
      {
        href: "/penjualan",
        label: "Unit & Penjualan",
        icon: "M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z",
      },
      {
        href: "/penjualan/sales",
        label: "Sales & CRM",
        icon: "M17 20h5v-2a4 4 0 00-3-3.87M9 20H4v-2a4 4 0 013-3.87m6-1.13a4 4 0 10-4-4 4 4 0 004 4zm6 0a4 4 0 10-2.5-7.12",
      },
      {
        href: "/penjualan/booking",
        label: "Booking",
        icon: "M5 13l4 4L19 7",
      },
      {
        href: "/penjualan/kpr",
        label: "KPR",
        icon: "M3 21h18M5 21V7l7-4 7 4v14M9 21v-6h6v6M9 10h.01M15 10h.01",
      },
      {
        href: "/penjualan/komisi",
        label: "Komisi Sales",
        icon: "M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z",
      },
      {
        href: "/penjualan/pembatalan",
        label: "Pembatalan & Refund",
        icon: "M16 15v-1a4 4 0 00-4-4H8m0 0l3 3m-3-3l3-3m9 14V5a2 2 0 00-2-2H6a2 2 0 00-2 2v16l4-2 4 2 4-2 4 2z",
      },
    ],
  },
  {
    label: "KEUANGAN",
    items: [
      {
        href: "/accounting/invoices",
        label: "Invoice Customer",
        icon: "M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z",
      },
      {
        href: "/accounting/receivable",
        label: "Piutang Customer",
        icon: "M17 9V7a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2m2 4h10a2 2 0 002-2v-6a2 2 0 00-2-2H9a2 2 0 00-2 2v6a2 2 0 002 2zm7-5a2 2 0 11-4 0 2 2 0 014 0z",
      },
      {
        // W-8/BD-1: jadwal pra-BAST bukan piutang, jadi ia tidak muncul di
        // Piutang Customer — menunya harus ada sendiri, kalau tidak pekerjaan
        // penagihan pra-serah-terima tidak punya pintu masuk.
        href: "/accounting/billing-schedule",
        label: "Jadwal Penagihan",
        icon: "M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z",
      },
      {
        href: "/accounting/collection",
        label: "Penagihan",
        icon: "M9 17v-2a4 4 0 014-4h4M9 17H5a2 2 0 01-2-2V5a2 2 0 012-2h10a2 2 0 012 2v4M9 17l3 3m0 0l3-3m-3 3V11",
      },
      {
        href: "/accounting/notaris",
        // Jalur warisan: intake sudah pindah ke Biaya Realisasi (master-driven).
        // Menu ditahan supaya titipan lama yang tertahan tetap bisa dibayarkan.
        label: "Titipan Notaris",
        icon: "M3 6l3 1m0 0l-3 9a5.002 5.002 0 006.001 0M6 7l3 9M6 7l6-2m6 2l3-1m-3 1l-3 9a5.002 5.002 0 006.001 0M18 7l3 9m-3-9l-6-2m0-2v2m0 16V5m0 16H9m3 0h3",
      },
      {
        // W-10: satu pintu masuk semua uang keluar. Sebelumnya biaya operasional
        // hanya bisa lewat Jurnal Umum — pekerjaan harian tanpa halamannya sendiri.
        href: "/accounting/pengeluaran",
        label: "Pengeluaran",
        icon: "M17 13l-5 5m0 0l-5-5m5 5V6m9 12a2 2 0 01-2 2H5a2 2 0 01-2-2",
      },
      {
        // Perolehan aset tetap dicatat lewat Pengeluaran (Jenis Pembelian "Aset
        // Tetap") — halaman ini adalah register + pemicu penyusutan bulanan.
        href: "/accounting/aset-tetap",
        label: "Aset Tetap",
        icon: "M3 21h18M5 21V7l8-4v18M19 21V11l-6-4M9 9h.01M9 12h.01M9 15h.01",
      },
      {
        // W-11: kewajiban kepada vendor. Bertetangga dengan Pengeluaran karena
        // keduanya menjawab "apa yang harus perusahaan bayar" — bedanya yang
        // satu sudah keluar uangnya, yang satu belum.
        href: "/accounting/hutang",
        label: "Hutang Usaha",
        icon: "M3 10h11M9 21V3m0 0l4 4m-4-4L5 7m11 7h5m-5 4h5m-5-8h5",
      },
      {
        href: "/accounting/vendor",
        label: "Master Vendor",
        icon: "M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4",
      },
      {
        href: "/accounting/jurnal",
        label: "Accounting",
        icon: "M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z",
      },
      {
        href: "/accounting/coa",
        label: "Daftar Akun",
        icon: "M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01",
      },
      {
        href: "/accounting/opening-balance",
        label: "Saldo Awal",
        icon: "M3 10h18M7 15h1m4 0h1m-7 4h12a3 3 0 003-3V8a3 3 0 00-3-3H6a3 3 0 00-3 3v8a3 3 0 003 3z",
      },
      {
        href: "/accounting/periods",
        label: "Periode Akuntansi",
        icon: "M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z",
      },
      {
        href: "/pajak",
        label: "Pajak",
        icon: "M9 14l-4-4 4-4M15 6l4 4-4 4",
      },
      {
        href: "/laporan",
        label: "Laporan",
        icon: "M18 20V10M12 20V4M6 20v-6",
      },
    ],
  },
  {
    label: "ADMIN",
    items: [
      {
        href: "/panduan",
        label: "Panduan Sistem",
        icon: "M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253",
      },
      {
        href: "/pengaturan",
        label: "Pengaturan",
        icon: "M12 15a3 3 0 100-6 3 3 0 000 6z M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z",
      },
    ],
  },
];

function NavIcon({ d }: { d: string }) {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" className="shrink-0">
      <path d={d} />
    </svg>
  );
}

// Label role datang dari lib/roles.ts — satu daftar untuk seluruh aplikasi.
// Salinan lokal di sini dulu berbunyi "Akuntan"/"Pengamat" sementara header
// berbunyi "Accounting"/"Viewer": satu pemakai, dua sebutan, di dua sudut layar
// yang terlihat bersamaan.

// W-12 — menu yang boleh dilihat role marketing. Daftar ini adalah cermin dari
// marketingAllow di backend (internal/platform/auth/scope.go); backend tetap
// yang menegakkan, di sini hanya supaya marketing tidak diberi tautan menuju
// halaman yang pasti membalas 403.
//
// Yang sengaja TIDAK ada: Dashboard (KPI keuangan), KPR (pencairan), Komisi,
// Pembatalan & Refund (uang keluar), seluruh grup KEUANGAN, dan Pengaturan.
// Panduan Sistem sengaja diikutkan — dokumentasi murni baca, aman untuk semua peran.
const MARKETING_NAV = new Set([
  "/proyek",
  "/penjualan",
  "/penjualan/sales",
  "/penjualan/booking",
  "/panduan",
]);

export function Sidebar({ drawer = false }: { drawer?: boolean }) {
  const pathname = usePathname();
  const user = useUserSafe();

  const navGroups = user?.role === "marketing"
    ? NAV_GROUPS
        .map((g) => ({ ...g, items: g.items.filter((i) => MARKETING_NAV.has(i.href)) }))
        .filter((g) => g.items.length > 0)
    : NAV_GROUPS;

  // Longest-match: "/penjualan/kpr" hanya menyalakan item KPR, bukan juga
  // "Unit & Penjualan" (prefix). Item aktif = href cocok TERPANJANG.
  const allHrefs = navGroups.flatMap((g) => g.items.filter((i) => !i.disabled).map((i) => i.href));
  const bestMatch = allHrefs
    .filter((h) => pathname === h || pathname.startsWith(h + "/"))
    .sort((a, b) => b.length - a.length)[0];

  // R7/U7 — desktop: sticky rail (disembunyikan di layar kecil, diganti drawer
  // dari Header); drawer: panel penuh di dalam slide-over mobile.
  // Lebar dari token --sidebar-width (256px, 2026-07 redesign).
  const asideClass = drawer
    ? "w-[256px] flex flex-col bg-surface h-full"
    : "w-[256px] shrink-0 hidden md:flex flex-col bg-surface border-r border-border h-screen sticky top-0";

  return (
    <aside className={asideClass}>
      {/* Logo brand — blok lebih lega + tagline (referensi klien) */}
      <div className="px-5 h-16 flex items-center gap-3 border-b border-border shrink-0">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/logo.png" alt={BRAND_NAME} className="h-10 w-10 object-contain shrink-0" />
        <div className="min-w-0">
          <span className="block text-[15px] font-extrabold track-tight text-text-primary leading-none">
            {BRAND_PREFIX} <span className="text-accent">{BRAND_ACCENT}</span>
          </span>
          <p className="text-[11px] text-text-tertiary leading-tight mt-1">
            {BRAND_TAGLINE}
          </p>
        </div>
      </div>

      {/* Nav */}
      <nav className="flex-1 py-4 px-3 space-y-5 overflow-y-auto scrollbar-thin">
        {navGroups.map((group) => (
          <div key={group.label}>
            <p className="eyebrow px-3 mb-1.5">{group.label}</p>
            <ul className="space-y-0.5">
              {group.items.map((item) => {
                const active = !item.disabled && item.href === bestMatch;

                if (item.disabled) {
                  return (
                    <li key={item.href}>
                      <span
                        className="flex items-center gap-3 px-3 py-2 rounded-md text-sm font-normal
                          text-text-tertiary cursor-not-allowed opacity-50"
                        title="Segera hadir"
                      >
                        <NavIcon d={item.icon} />
                        {item.label}
                      </span>
                    </li>
                  );
                }

                return (
                  <li key={item.href}>
                    <Link
                      href={item.href}
                      aria-current={active ? "page" : undefined}
                      className={`relative flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors
                        ${active
                          ? "bg-accent-light text-accent font-semibold"
                          : "text-text-primary font-medium hover:bg-border-subtle hover:text-accent"
                        }`}
                    >
                      {/* Rail aksen kiri utk item aktif — bukan blok solid berat */}
                      {active && (
                        <span className="absolute left-0 top-1.5 bottom-1.5 w-[3px] rounded-full bg-accent" />
                      )}
                      <span className={active ? "text-accent" : "text-text-secondary"}>
                        <NavIcon d={item.icon} />
                      </span>
                      {item.label}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      {/* Footer — kartu identitas user (referensi klien) */}
      {user && (
        <div className="px-3 py-3 border-t border-border shrink-0">
          <div className="flex items-center gap-3 px-2 py-1.5">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full
              bg-accent-light text-accent text-sm font-bold uppercase">
              {(user.display_name || user.email || "?").charAt(0)}
            </span>
            <div className="min-w-0 flex-1">
              {/* display_name sudah di-fallback backend ke email bila nama belum
                  diisi — tidak ada nama yang dikarang di sini. */}
              <p className="truncate text-sm font-semibold text-text-primary leading-tight">
                {user.display_name || user.email}
              </p>
              {/* Email hanya diulang bila baris atas memang nama — kalau nama
                  belum diisi, baris atas SUDAH email dan mengulangnya percuma. */}
              {user.name && (
                <p className="truncate text-[11px] text-text-tertiary leading-tight mt-0.5">
                  {user.email}
                </p>
              )}
              <p className="text-[11px] text-text-tertiary leading-tight mt-0.5">
                {ROLE_LABELS[user.role] ?? user.role}
              </p>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}
