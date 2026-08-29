"use client";

import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { StatusBadge } from "@/components/ui/Badge";
import { Tanggal } from "@/components/format/Tanggal";
import { TableSearch, TablePagination, useTableView } from "@/components/ui/TableView";
import type { Project, Unit, UnitStatus } from "@/lib/types/api";

export interface ProjectWithUnits {
  project: Project;
  /** null = ringkasan unit gagal diambil; bukan "nol unit". */
  units: Unit[] | null;
}

// Hitung unit per status — counting (bukan money), diperbolehkan di FE.
function countByStatus(units: Unit[]): Record<UnitStatus | "total", number> {
  const counts: Record<UnitStatus | "total", number> = {
    available: 0, booked: 0, reserved: 0, ppjb: 0, sold: 0,
    occupied: 0, hold: 0, blocked: 0, maintenance: 0, total: units.length,
  };
  for (const u of units) counts[u.status]++;
  return counts;
}

/**
 * Daftar proyek dengan pencarian.
 *
 * Kotak cari selalu tampil, tidak muncul-hilang menurut jumlah proyek: kontrol
 * yang kadang ada kadang tidak membuat pemakai berhenti mencarinya.
 */
export function ProjectList({ rows }: { rows: ProjectWithUnits[] }) {
  const view = useTableView({
    rows,
    searchFields: ({ project }) => [project.name, project.status, project.notes],
    pageSize: 15,
  });

  return (
    <Card padding="none">
      <TableSearch
        value={view.query}
        onChange={view.setQuery}
        placeholder="Cari nama proyek…"
      >
        <span className="text-xs text-text-secondary tabular">
          {view.total} dari {rows.length} proyek
        </span>
      </TableSearch>

      {view.visible.length === 0 ? (
        <div className="px-4 py-10 text-center">
          <p className="text-sm text-text-secondary">
            Tidak ada proyek yang cocok dengan “{view.query}”.
          </p>
          <p className="text-xs text-text-tertiary mt-1">
            Coba potongan namanya saja, atau kosongkan kotak pencarian.
          </p>
        </div>
      ) : (
        <div className="divide-y divide-border">
          {view.visible.map(({ project, units }) => {
            const counts = countByStatus(units ?? []);
            return (
              <Link key={project.id} href={`/proyek/${project.id}`} className="block">
                <div className="p-5 flex items-start justify-between gap-4 flex-wrap
                  hover:bg-border-subtle/40 transition-colors">
                  {/* Info proyek */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-3 mb-1">
                      <h2 className="text-base font-semibold text-text-primary">
                        {project.name}
                      </h2>
                      <StatusBadge status={project.status} />
                    </div>
                    <div className="flex items-center gap-4 text-sm text-text-secondary flex-wrap">
                      {project.land_area && project.land_area !== "0.0000" && (
                        <span>{project.land_area} m²</span>
                      )}
                      {project.start_date && (
                        <span>Mulai <Tanggal value={project.start_date} /></span>
                      )}
                      {project.notes && (
                        <span className="text-text-tertiary truncate max-w-xs">
                          {project.notes}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Ringkasan unit */}
                  <div className="flex items-center gap-1 text-xs shrink-0">
                    {units === null ? (
                      <span className="text-text-tertiary">—</span>
                    ) : (
                      <>
                        <UnitPill color="text-accent"  label="T" count={counts.available} title="Tersedia" />
                        <span className="text-border mx-0.5">·</span>
                        <UnitPill color="text-warning" label="D" count={counts.reserved}  title="Dipesan" />
                        <span className="text-border mx-0.5">·</span>
                        <UnitPill color="text-success" label="J" count={counts.sold}      title="Terjual" />
                        <span className="text-text-tertiary ml-1">/ {counts.total} unit</span>
                      </>
                    )}
                  </div>
                </div>
              </Link>
            );
          })}
        </div>
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

function UnitPill({
  color, label, count, title,
}: { color: string; label: string; count: number; title: string }) {
  return (
    <span className={`font-bold ${color}`} title={title}>
      {label}{count}
    </span>
  );
}
