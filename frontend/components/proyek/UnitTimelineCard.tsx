"use client";

// Increment 6 — Riwayat lifecycle unit (audit trail unit_status_transitions).

import { useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { StatusBadge } from "@/components/ui/Badge";
import { Tanggal } from "@/components/format/Tanggal";
import { fetchUnitTransitions, type UnitTransition } from "@/lib/api/projects";

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

export function UnitTimelineCard({ token, unitId }: { token: string; unitId: number }) {
  const [rows, setRows] = useState<UnitTransition[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchUnitTransitions(token, unitId)
      .then(setRows)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [token, unitId]);

  if (loading || rows.length === 0) return null;

  return (
    <Card padding="sm">
      <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-3">
        Riwayat Status Unit
      </p>
      <ol className="space-y-2">
        {[...rows].reverse().map((t) => (
          <li key={t.id} className="flex items-start gap-3 text-sm">
            <span className="mt-1 h-2 w-2 rounded-full bg-accent shrink-0" />
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-text-primary">
                  {EVENT_LABELS[t.event] ?? t.event}
                </span>
                {t.from_status && (
                  <span className="text-xs text-text-secondary inline-flex items-center gap-1">
                    <StatusBadge status={t.from_status} /> → <StatusBadge status={t.to_status} />
                  </span>
                )}
                {!t.from_status && <StatusBadge status={t.to_status} />}
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
    </Card>
  );
}
