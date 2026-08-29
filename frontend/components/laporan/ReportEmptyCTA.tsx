import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";

// ReportEmptyCTA adalah empty state informatif untuk laporan keuangan yang
// belum punya data. Menjawab: kenapa kosong, apa penyebabnya, dan langkah
// berikutnya (dengan CTA). Dipakai bersama oleh seluruh seksi laporan.

type ReportKind = "neraca" | "laba-rugi" | "arus-kas" | "pipeline" | "neraca-saldo";

interface Copy {
  title: string;
  description: string;
  href: string;
  cta: string;
}

const COPY: Record<ReportKind, Copy> = {
  neraca: {
    title: "Neraca masih kosong",
    description:
      "Neraca terbentuk dari jurnal yang sudah diposting. Belum ada saldo karena belum ada transaksi. Mulai dengan menginput saldo awal akun, atau catat transaksi pertama (biaya, penjualan, termin).",
    href: "/accounting/opening-balance",
    cta: "Input Saldo Awal →",
  },
  "laba-rugi": {
    title: "Belum ada Laba Rugi",
    description:
      "Laba rugi muncul setelah ada pendapatan atau beban yang diposting. Belum ada karena belum ada penjualan (BAST) maupun beban operasional. Akui penjualan unit atau catat biaya untuk mengisinya.",
    href: "/penjualan",
    cta: "Ke Unit & Penjualan →",
  },
  "arus-kas": {
    title: "Belum ada Arus Kas",
    description:
      "Arus kas terbentuk dari jurnal yang menyentuh akun kas/bank dalam periode ini. Belum ada pergerakan kas tercatat. Catat penerimaan termin pembeli atau pembayaran biaya untuk melihatnya.",
    href: "/penjualan",
    cta: "Ke Unit & Penjualan →",
  },
  pipeline: {
    title: "Belum ada data Pipeline",
    description:
      "Pipeline membaca status unit (tersedia, dipesan, terjual) dari proyek Anda. Belum ada unit yang terdaftar. Tambahkan proyek beserta unitnya untuk mulai melacak pipeline penjualan.",
    href: "/proyek",
    cta: "Ke Proyek →",
  },
  "neraca-saldo": {
    title: "Neraca Saldo masih kosong",
    description:
      "Neraca saldo menampilkan saldo seluruh akun dari jurnal yang sudah diposting. Belum ada jurnal tercatat. Mulai dengan menginput saldo awal agar buku besar terisi.",
    href: "/accounting/opening-balance",
    cta: "Input Saldo Awal →",
  },
};

const ICON: Record<ReportKind, string> = {
  neraca: "M3 10h18M3 6h18M3 14h18M3 18h18",
  "laba-rugi": "M18 20V10M12 20V4M6 20v-6",
  "arus-kas": "M12 2v20M17 7l-5-5-5 5M7 17l5 5 5-5",
  pipeline: "M3 3v18h18M7 16l4-4 3 3 5-6",
  "neraca-saldo": "M3 6h18M3 12h18M3 18h18",
};

export function ReportEmptyCTA({ kind }: { kind: ReportKind }) {
  const c = COPY[kind];
  return (
    <Card>
      <EmptyState
        icon={
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"
            strokeLinecap="round" strokeLinejoin="round">
            <path d={ICON[kind]} />
          </svg>
        }
        title={c.title}
        description={c.description}
        action={
          <Link
            href={c.href}
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
          >
            {c.cta}
          </Link>
        }
      />
    </Card>
  );
}
