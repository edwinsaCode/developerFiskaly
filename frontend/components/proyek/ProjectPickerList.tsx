"use client";

import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { StatusBadge } from "@/components/ui/Badge";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Tanggal } from "@/components/format/Tanggal";
import { TableSearch, TablePagination, useTableView } from "@/components/ui/TableView";
import type { Project } from "@/lib/types/api";

interface Props {
  title: string;
  description: string;
  /** Sub-route tujuan per proyek, mis. "rab" → /proyek/{id}/rab */
  feature: "rab" | "biaya";
  /** Teks CTA per kartu, mis. "Buka RAB" */
  cta: string;
  emptyDescription: string;
  projects: Project[];
  fetchError: boolean;
}

// ProjectPickerList: landing "pilih proyek" untuk fitur per-proyek (RAB / Biaya).
// Menggantikan redirect ke /proyek — user memilih proyek lalu masuk fitur.
export function ProjectPickerList({
  title,
  description,
  feature,
  cta,
  emptyDescription,
  projects,
  fetchError,
}: Props) {
  const view = useTableView({
    rows: projects,
    searchFields: (p) => [p.name, p.status],
    pageSize: 15,
  });

  return (
    <div>
      <div className="mb-6">
        <h1 className="display-lg text-2xl text-text-primary">{title}</h1>
        <p className="text-sm text-text-secondary mt-0.5">{description}</p>
      </div>

      {fetchError && (
        <Card className="mb-4">
          <ErrorState
            title="Gagal Memuat Proyek"
            description="Tidak dapat mengambil daftar proyek dari server. Periksa koneksi dan coba lagi."
          />
        </Card>
      )}

      {!fetchError && projects.length === 0 && (
        <Card>
          <EmptyState
            icon={
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z" />
              </svg>
            }
            title="Belum ada proyek"
            description={emptyDescription}
            action={
              <Link
                href="/proyek"
                className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
              >
                Ke Daftar Proyek →
              </Link>
            }
          />
        </Card>
      )}

      {!fetchError && projects.length > 0 && (
      <Card padding="none">
        <TableSearch value={view.query} onChange={view.setQuery} placeholder="Cari nama proyek…" />
        {view.visible.length === 0 ? (
          <div className="px-4 py-10 text-center">
            <p className="text-sm text-text-secondary">
              Tidak ada proyek yang cocok dengan “{view.query}”.
            </p>
          </div>
        ) : (
        <div className="divide-y divide-border">
          {view.visible.map((project) => (
            <Link key={project.id} href={`/proyek/${project.id}/${feature}`} className="block">
              <div className="p-5 flex items-center justify-between gap-4 flex-wrap
                hover:bg-border-subtle/40 transition-colors">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-1">
                    <h2 className="text-base font-semibold text-text-primary">{project.name}</h2>
                    <StatusBadge status={project.status} />
                  </div>
                  <div className="flex items-center gap-4 text-sm text-text-secondary flex-wrap">
                    {project.land_area && project.land_area !== "0.0000" && (
                      <span>{project.land_area} m²</span>
                    )}
                    {project.start_date && (
                      <span>Mulai <Tanggal value={project.start_date} /></span>
                    )}
                  </div>
                </div>
                <span className="text-sm text-accent font-medium shrink-0">{cta} →</span>
              </div>
            </Link>
          ))}
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
      )}
    </div>
  );
}
