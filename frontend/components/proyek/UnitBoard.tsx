"use client";

import { useState } from "react";
import { Unit, ProjectPhase, UnitStatus } from "@/lib/types/api";
import { UnitCard } from "./UnitCard";
import { EmptyState } from "@/components/ui/EmptyState";
import { Select } from "@/components/ui/Input";
import { matchesQuery } from "@/lib/table-view";

interface UnitBoardProps {
  units: Unit[];
  phases: ProjectPhase[];
  projectId: number;
}

// Papan pipeline: SEMUA 9 status unit terpetakan ke kolom — tidak ada unit
// yang "hilang" dari papan (sebelumnya booked/ppjb/dll tak tampil sama sekali).
const COLUMNS: { key: string; statuses: UnitStatus[]; label: string; color: string }[] = [
  { key: "available", statuses: ["available"], label: "Tersedia", color: "border-t-accent" },
  { key: "pipeline", statuses: ["booked", "reserved"], label: "Booking / Dipesan", color: "border-t-warning" },
  { key: "ppjb", statuses: ["ppjb"], label: "PPJB", color: "border-t-chart-5" },
  { key: "sold", statuses: ["sold"], label: "Terjual", color: "border-t-success" },
  { key: "other", statuses: ["occupied", "hold", "blocked", "maintenance"], label: "Lainnya", color: "border-t-border" },
];

export function UnitBoard({ units, phases, projectId }: UnitBoardProps) {
  const [selectedPhaseId, setSelectedPhaseId] = useState<string>("all");
  const [query, setQuery] = useState("");

  const byPhase = selectedPhaseId === "all"
    ? units
    : selectedPhaseId === "none"
      ? units.filter(u => !u.phase_id)
      : units.filter(u => String(u.phase_id) === selectedPhaseId);

  // Pencarian menyempitkan papan, bukan mengganti tampilannya: unit yang cocok
  // tetap berdiri di kolom statusnya, sehingga "Udin punya unit apa saja, dan
  // sudah sampai tahap mana" terjawab dalam satu pandangan.
  const filtered = query.trim()
    ? byPhase.filter(u =>
        matchesQuery([u.code, u.unit_type, u.type_label, u.buyer_name, u.status], query),
      )
    : byPhase;

  return (
    <div>
      {/* Cari + filter fase */}
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <div className="min-w-0 flex-1 sm:max-w-xs">
          <label className="block text-xs font-medium text-text-secondary mb-1">
            Cari unit
          </label>
          <input
            type="search"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Kode unit, tipe, atau nama pembeli…"
            className="w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm
              text-text-primary placeholder:text-text-tertiary
              focus:outline-none focus:ring-2 focus:ring-accent/30 focus:border-accent/50"
          />
        </div>
        {phases.length > 0 && (
          <div className="min-w-0 flex-1 sm:max-w-xs">
          <Select
            label="Filter Fase"
            value={selectedPhaseId}
            onChange={e => setSelectedPhaseId(e.target.value)}
          >
            <option value="all">Semua Fase ({units.length} unit)</option>
            {phases.map(p => {
              const count = units.filter(u => String(u.phase_id) === String(p.id)).length;
              return (
                <option key={p.id} value={String(p.id)}>
                  {p.name} ({count} unit)
                </option>
              );
            })}
            {/* Unit tanpa fase */}
            {units.some(u => !u.phase_id) && (
              <option value="none">
                Tanpa Fase ({units.filter(u => !u.phase_id).length} unit)
              </option>
            )}
          </Select>
          </div>
        )}
        {query.trim() && (
          <span className="text-xs text-text-secondary pb-2">
            {filtered.length} dari {byPhase.length} unit
          </span>
        )}
      </div>

      {/* Papan 3 kolom */}
      {filtered.length === 0 ? (
        // Dua keadaan yang sama sekali berbeda: papan yang memang belum berisi,
        // dan pencarian yang tidak menemukan apa pun. Menyamakan keduanya
        // membuat pemakai mengira unitnya terhapus.
        query.trim() ? (
          <EmptyState
            title={`Tidak ada unit yang cocok dengan “${query}”`}
            description="Coba kode unit, tipe, atau nama pembeli — atau kosongkan kotak pencarian."
          />
        ) : (
          <EmptyState
            title="Belum ada unit"
            description="Unit akan muncul setelah ditambahkan ke proyek ini."
          />
        )
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
          {COLUMNS.filter(col =>
            col.key !== "other" || filtered.some(u => col.statuses.includes(u.status)),
          ).map(col => {
            const colUnits = filtered.filter(u => col.statuses.includes(u.status));
            return (
              <div key={col.key} className="flex flex-col">
                {/* Kolom header */}
                <div className={`bg-border-subtle/60 border border-border border-t-2 ${col.color}
                  rounded-t-lg px-3 py-2.5 flex items-center justify-between`}>
                  <span className="text-xs font-semibold text-text-secondary uppercase tracking-wide">
                    {col.label}
                  </span>
                  <span className="text-xs font-bold text-text-primary bg-surface
                    border border-border rounded-full px-2 py-0.5">
                    {colUnits.length}
                  </span>
                </div>

                {/* Kartu unit */}
                <div className="flex-1 border border-t-0 border-border rounded-b-lg
                  bg-border-subtle/20 p-2 space-y-2 min-h-[120px]">
                  {colUnits.length === 0 ? (
                    <p className="text-xs text-text-tertiary text-center py-6">—</p>
                  ) : (
                    colUnits.map(unit => (
                      <UnitCard key={unit.id} unit={unit} projectId={projectId} />
                    ))
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
