"use client";

// PS-2 — Timeline workspace: transisi lifecycle terbaru lintas unit proyek
// (audit trail Increment 6, endpoint /projects/{id}/transitions).

import { useEffect, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { StatusBadge } from "@/components/ui/Badge";
import { Tanggal } from "@/components/format/Tanggal";
import { EmptyState } from "@/components/ui/EmptyState";
import { TableSearch, TablePagination, useTableView } from "@/components/ui/TableView";
import { fetchProjectTransitions, type UnitTransition } from "@/lib/api/projects";
import type { Unit } from "@/lib/types/api";

const EVENT_LABELS: Record<string, string> = {
  booking_created: "Booking dibuat",
  booking_expired: "Booking kedaluwarsa",
  booking_cancelled: "Booking dibatalkan",
  reservation_confirmed: "Reservasi dikonfirmasi",
  reservation_cancelled: "Reservasi dibatalkan",
  contract_signed: "Kontrak ditandatangani",
  contract_cancelled: "Kontrak dibatalkan",
  akad_executed: "Akad dilaksanakan",
  bast_executed: "BAST dilaksanakan", // riwayat lama sebelum Temuan #7 (event key berganti nama)
  cancelled_post_bast: "Penjualan dibatalkan (pasca-BAST)",
  physically_occupied: "Serah terima fisik",
  admin_hold: "Hold administratif",
  hold_released: "Hold dilepas",
  legal_block: "Blokir legal",
  unblocked: "Blokir dibuka",
  maintenance_started: "Perbaikan dimulai",
  maintenance_finished: "Perbaikan selesai",
  manual: "Perubahan manual",
  backfill: "Titik awal pencatatan",
};

export function TimelineTab({
  token, projectId, units,
}: {
  token: string;
  projectId: number;
  units: Unit[];
}) {
  const [rows, setRows] = useState<UnitTransition[]>([]);
  const [loading, setLoading] = useState(true);
  const unitCode = Object.fromEntries(units.map((u) => [u.id, u.code]));
  const unitBuyer = Object.fromEntries(units.map((u) => [u.id, u.buyer_name ?? ""]));

  useEffect(() => {
    fetchProjectTransitions(token, projectId)
      .then(setRows)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [token, projectId]);

  // Timeline tidak punya batas atas: proyek berumur dua tahun bisa ribuan baris,
  // dan yang dicari orang justru satu unit ("C-01 kenapa balik ke tersedia?").
  // Karena itu dicari lewat kata yang benar-benar dipakai orang — kode unit,
  // nama pembelinya, dan LABEL peristiwa (bukan kode `bast_executed`).
  const view = useTableView({
    rows,
    searchFields: (t) => [
      unitCode[t.unit_id],
      unitBuyer[t.unit_id],
      EVENT_LABELS[t.event] ?? t.event,
      t.from_status,
      t.to_status,
      t.notes,
      t.event_date,
    ],
    pageSize: 30,
  });

  if (loading) {
    return (
      <Card padding="sm">
        <div className="animate-pulse space-y-2">
          {[...Array(5)].map((_, i) => <div key={i} className="h-8 rounded bg-border-subtle" />)}
        </div>
      </Card>
    );
  }
  if (rows.length === 0) {
    return (
      <EmptyState
        title="Belum ada aktivitas"
        description="Setiap perubahan status unit (booking, reservasi, PPJB, BAST, pembatalan) tercatat otomatis di sini sebagai jejak audit."
      />
    );
  }

  return (
    <Card padding="none">
      <TableSearch
        value={view.query}
        onChange={view.setQuery}
        placeholder="Cari unit, pembeli, atau peristiwa…"
      />
      {view.visible.length === 0 ? (
        <div className="px-4 py-10 text-center">
          <p className="text-sm text-text-secondary">
            Tidak ada aktivitas yang cocok dengan “{view.query}”.
          </p>
          <p className="text-xs text-text-tertiary mt-1">
            Coba kode unit, nama pembeli, atau kata peristiwa seperti “BAST”.
          </p>
        </div>
      ) : (
      <ol className="space-y-3 p-4">
        {view.visible.map((t) => (
          <li key={t.id} className="flex items-start gap-3 text-sm">
            <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-accent" />
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Link
                  href={`/penjualan/${t.unit_id}`}
                  className="font-medium text-accent hover:underline"
                >
                  {unitCode[t.unit_id] ?? `Unit #${t.unit_id}`}
                </Link>
                <span className="text-text-primary">{EVENT_LABELS[t.event] ?? t.event}</span>
                {t.from_status && (
                  <span className="inline-flex items-center gap-1 text-xs text-text-secondary">
                    <StatusBadge status={t.from_status} /> → <StatusBadge status={t.to_status} />
                  </span>
                )}
              </div>
              <p className="text-xs text-text-secondary">
                <Tanggal value={t.event_date} />
                {t.reference_type !== "manual" && t.reference_type !== "backfill" && (
                  <> · ref: {t.reference_type}{t.reference_id ? ` #${t.reference_id}` : ""}</>
                )}
                {t.notes ? <> · {t.notes}</> : null}
              </p>
            </div>
          </li>
        ))}
      </ol>
      )}
      <TablePagination
        page={view.page}
        pages={view.pages}
        pageSize={view.pageSize}
        total={view.total}
        onPage={view.setPage}
      />
    </Card>
  );
}
