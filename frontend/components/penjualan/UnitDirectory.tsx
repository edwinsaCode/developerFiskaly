"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { Card } from "@/components/ui/Card";
import { AutoGrid } from "@/components/ui/AutoGrid";
import { StatusBadge } from "@/components/ui/Badge";
import { Rupiah } from "@/components/format/Rupiah";
import { TableSearch, TablePagination, useTableView } from "@/components/ui/TableView";
import { matchesQuery } from "@/lib/table-view";
import { buyerRoleLabel } from "@/lib/unit-label";
import type { Unit } from "@/lib/types/api";

export interface UnitWithProject extends Unit {
  project_name: string;
}

// ── Pengelompokan unit ────────────────────────────────────────────────────────
//
// Layar ini dulu hanya menyaring tiga status (available|reserved|sold), padahal
// siklus hidup unit punya sembilan. Akibatnya unit yang sudah DIBOOKING — dan
// juga PPJB, dihuni, hold, blokir, perbaikan — tidak muncul di mana pun: uang
// booking sudah diterima tapi unitnya lenyap dari daftar.
//
// Karena itu pengelompokan berbasis PEMETAAN, bukan penyaringan: setiap status
// dipetakan ke tepat satu kelompok, dan status yang tidak dikenali jatuh ke
// "Status Lain". Menambah status baru di backend tidak akan pernah lagi membuat
// unit menghilang diam-diam dari layar penjualan.
type GroupKey = "available" | "booked" | "process" | "sold" | "nonsale" | "other";

const GROUP_OF: Record<string, GroupKey> = {
  available:   "available",
  booked:      "booked",
  reserved:    "process",
  ppjb:        "process",
  sold:        "sold",
  occupied:    "sold",
  hold:        "nonsale",
  blocked:     "nonsale",
  maintenance: "nonsale",
};

const GROUP_META: Record<GroupKey, { title: string; hint: string }> = {
  available: { title: "Tersedia",       hint: "siap dipasarkan" },
  booked:    { title: "Dibooking",      hint: "booking fee diterima, belum kontrak" },
  process:   { title: "Dipesan / PPJB", hint: "sudah berkontrak, menuju serah terima" },
  sold:      { title: "Terjual",        hint: "BAST sudah diproses" },
  nonsale:   { title: "Tidak Dijual",   hint: "hold, blokir, atau perbaikan" },
  other:     { title: "Status Lain",    hint: "status belum dikenali layar ini" },
};

const GROUP_ORDER: GroupKey[] = ["available", "booked", "process", "sold", "nonsale", "other"];

/** Kolom yang ikut dicari. Nama pembeli ikut — itu justru cara orang mencari. */
function unitSearchFields(u: UnitWithProject): unknown[] {
  return [u.code, u.unit_type, u.type_label, u.project_name, u.buyer_name, u.status];
}

function groupUnits(units: UnitWithProject[]): Record<GroupKey, UnitWithProject[]> {
  const out: Record<GroupKey, UnitWithProject[]> = {
    available: [], booked: [], process: [], sold: [], nonsale: [], other: [],
  };
  for (const u of units) out[GROUP_OF[u.status] ?? "other"].push(u);
  return out;
}

/**
 * Direktori unit dengan pencarian lintas kelompok.
 *
 * Pencariannya SATU untuk semua kelompok, bukan satu per seksi: orang mencari
 * "Udin" tanpa tahu lebih dulu unitnya sedang di kelompok Dibooking atau PPJB —
 * kalau kotak carinya per seksi, ia harus menebak dulu jawabannya.
 */
export function UnitDirectory({ units }: { units: UnitWithProject[] }) {
  const [query, setQuery] = useState("");

  const matched = useMemo(
    () => (query.trim() ? units.filter((u) => matchesQuery(unitSearchFields(u), query)) : units),
    [units, query],
  );
  const groups = useMemo(() => groupUnits(matched), [matched]);

  return (
    <div className="space-y-6">
      <Card padding="none">
        <TableSearch
          value={query}
          onChange={setQuery}
          placeholder="Cari unit, tipe, proyek, atau nama pembeli…"
        >
          <span className="text-xs text-text-secondary tabular">
            {matched.length} dari {units.length} unit
          </span>
        </TableSearch>
      </Card>

      {matched.length === 0 ? (
        <Card>
          <div className="py-8 text-center">
            <p className="text-sm text-text-secondary">
              Tidak ada unit yang cocok dengan “{query}”.
            </p>
            <p className="text-xs text-text-tertiary mt-1">
              Coba kode unit ({units[0]?.code ?? "mis. C-01"}), nama proyek, atau nama pembeli.
            </p>
          </div>
        </Card>
      ) : (
        <div className="space-y-8">
          {GROUP_ORDER.map((k) => (
            <UnitSection
              key={k}
              title={GROUP_META[k].title}
              hint={GROUP_META[k].hint}
              units={groups[k]}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// Satu seksi status. Kolom mengikuti jumlah unit (AutoGrid) — 2 unit tidak lagi
// menyisakan sel kosong di kanan. Halaman per seksi: proyek 200 unit membuat
// seksi "Tersedia" sendirian sepanjang lima layar dan menenggelamkan seksi di
// bawahnya.
const CARDS_PER_PAGE = 12;

function UnitSection({
  title, hint, units,
}: {
  title: string;
  hint?: string;
  units: UnitWithProject[];
}) {
  const view = useTableView({
    rows: units,
    searchFields: unitSearchFields,
    pageSize: CARDS_PER_PAGE,
  });
  if (units.length === 0) return null;
  return (
    <section>
      <div className="flex items-center gap-3 mb-3">
        <h2 className="eyebrow">{title}</h2>
        <span className="text-xs font-semibold text-text-tertiary tabular">{units.length}</span>
        {hint && <span className="text-xs text-text-tertiary hidden sm:inline">· {hint}</span>}
        <span className="h-px flex-1 bg-border" aria-hidden="true" />
      </div>
      <AutoGrid count={view.visible.length} max={3} gap="md">
        {view.visible.map((u) => (
          <UnitDirectoryCard key={u.id} unit={u} />
        ))}
      </AutoGrid>
      {view.pages > 1 && (
        <Card padding="none" className="mt-3">
          <TablePagination
            page={view.page}
            pages={view.pages}
            pageSize={view.pageSize}
            total={view.total}
            onPage={view.setPage}
          />
        </Card>
      )}
    </section>
  );
}

function UnitDirectoryCard({ unit }: { unit: UnitWithProject }) {
  return (
    <Link href={`/penjualan/${unit.id}`} className="block group h-full">
      <Card
        padding="sm"
        className="h-full flex flex-col hover:border-accent/50 hover:shadow-md
          transition-all cursor-pointer"
      >
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="min-w-0">
            <p className="font-semibold text-text-primary text-base track-tight truncate
              group-hover:text-accent transition-colors">
              {unit.code}
            </p>
            <p className="text-xs text-text-secondary truncate">{unit.project_name}</p>
          </div>
          <span className="shrink-0"><StatusBadge status={unit.status} /></span>
        </div>
        {/* Harga diberi baris sendiri: di kolom yang lebar, angka & meta yang
            dijejalkan satu baris membuat kartu terasa kosong di tengah. */}
        <div className="mt-auto pt-3 border-t border-border-subtle flex items-end
          justify-between gap-3">
          <span className="text-xs text-text-secondary truncate">
            {unit.unit_type} · {Number(unit.saleable_area).toLocaleString("id-ID")} m²
          </span>
          <span className="text-sm font-semibold text-text-primary tabular shrink-0">
            <Rupiah value={unit.list_price} colorSign={false} />
          </span>
        </div>
        {/* buyer_name, bukan buyer_ref — unit yang baru dipesan/PPJB juga
            punya nama, dan itu justru unit yang perlu dikenali. */}
        {unit.buyer_name && (
          <p className="text-xs text-text-tertiary mt-2 truncate">
            {buyerRoleLabel(unit.buyer_source)}: {unit.buyer_name}
          </p>
        )}
      </Card>
    </Link>
  );
}
