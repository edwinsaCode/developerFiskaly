import { ReactNode } from "react";

// AutoGrid — grid kartu yang jumlah kolomnya MENGIKUTI jumlah data.
//
// Masalah yang dipecahkan: grid 3 kolom statis terlihat "bolong" ketika datanya
// hanya 2 — satu sel kosong menganga di kanan dan halaman terasa rata kiri.
// Di sini kolom = min(max, jumlah item): 2 item → 2 kolom penuh, 1 item → 1
// kolom penuh, 3+ item → grid penuh sesuai `max`.
//
// Kelas Tailwind ditulis LITERAL (bukan `grid-cols-${n}`) karena Tailwind
// memindai kode statis; kelas yang dirangkai saat runtime tidak akan ter-generate.

type MaxCols = 2 | 3 | 4;

/** Peta [max][jumlah kolom efektif] → kelas kolom responsif. */
const COLS: Record<MaxCols, Record<number, string>> = {
  2: {
    1: "grid-cols-1",
    2: "grid-cols-1 sm:grid-cols-2",
  },
  3: {
    1: "grid-cols-1",
    2: "grid-cols-1 sm:grid-cols-2",
    3: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3",
  },
  4: {
    1: "grid-cols-1",
    2: "grid-cols-1 sm:grid-cols-2",
    3: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3",
    4: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4",
  },
};

interface AutoGridProps {
  /** Jumlah item yang dirender di dalam grid. */
  count: number;
  /** Batas atas kolom di layar lebar. Default 3. */
  max?: MaxCols;
  /** Jarak antar kartu. Default "md" (gap-4). */
  gap?: "sm" | "md" | "lg";
  className?: string;
  children: ReactNode;
}

const GAP = { sm: "gap-3", md: "gap-4", lg: "gap-5" } as const;

export function AutoGrid({
  count,
  max = 3,
  gap = "md",
  className = "",
  children,
}: AutoGridProps) {
  const effective = Math.min(max, Math.max(1, count));
  return (
    <div className={`grid ${COLS[max][effective]} ${GAP[gap]} ${className}`}>
      {children}
    </div>
  );
}
