"use client";

import { useRouter, usePathname, useSearchParams } from "next/navigation";
import type { Project } from "@/lib/types/api";

const TABS = [
  { id: "neraca",       label: "Neraca" },
  { id: "laba-rugi",   label: "Laba Rugi" },
  { id: "laba-rugi-proyek", label: "L/R per Proyek" },
  { id: "arus-kas",    label: "Arus Kas" },
  { id: "rab-realisasi", label: "RAB vs Realisasi" },
  { id: "pipeline",    label: "Pipeline" },
  { id: "pajak",       label: "Pajak" },
  { id: "neraca-saldo", label: "Neraca Saldo" },
] as const;

type TabId = typeof TABS[number]["id"];

interface Props {
  activeTab: string;
  projects: Project[];
  asOf: string;
  startDate: string;
  periodFrom: string;
  periodTo: string;
  projectId: string;
}

export function LaporanNavClient({ activeTab, projects, asOf, startDate, periodFrom, periodTo, projectId }: Props) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  function navigate(updates: Record<string, string>) {
    const params = new URLSearchParams(searchParams.toString());
    // Clear account drill-down when switching tabs
    if (updates.tab && updates.tab !== searchParams.get("tab")) {
      params.delete("account_id");
      params.delete("account_name");
    }
    for (const [k, v] of Object.entries(updates)) {
      if (v) params.set(k, v);
      else params.delete(k);
    }
    router.push(`${pathname}?${params.toString()}`);
  }

  const needsDateRange = ["arus-kas", "pajak"].includes(activeTab);
  const needsAsOf = ["neraca", "laba-rugi", "laba-rugi-proyek", "neraca-saldo"].includes(activeTab);
  // Item 5: Start Date opsional — Neraca & L/R saja (Neraca Saldo tetap satu tanggal,
  // ia bukan bagian dari cakupan Item 5).
  const needsStartDate = ["neraca", "laba-rugi", "laba-rugi-proyek"].includes(activeTab);
  const needsProject = ["laba-rugi-proyek", "rab-realisasi"].includes(activeTab);

  return (
    <div className="space-y-4">
      {/* Tab navigation — 8 tab. Di desktop muat satu baris; di layar sempit
          digulir horizontal DI DALAM barisnya sendiri (sama seperti komponen
          Tabs bersama), bukan mematah jadi tiga baris. */}
      <div className="flex gap-1 overflow-x-auto scrollbar-none border-b border-border">
        {TABS.map(tab => (
          <button
            key={tab.id}
            onClick={() => navigate({ tab: tab.id })}
            className={`px-4 py-2.5 text-sm font-medium transition-colors border-b-2 -mb-px whitespace-nowrap shrink-0
              ${activeTab === tab.id
                ? "border-accent text-accent"
                : "border-transparent text-text-secondary hover:text-accent"
              }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Filters */}
      {(needsAsOf || needsDateRange || needsProject) && (
        <div className="flex flex-wrap items-center gap-3">
          {needsStartDate && (
            <div className="flex items-center gap-2 text-sm">
              <label className="text-text-secondary whitespace-nowrap">Dari tanggal:</label>
              <input
                type="date"
                value={startDate}
                onChange={e => navigate({ start_date: e.target.value })}
                className="border border-border rounded px-2 py-1 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40"
              />
            </div>
          )}

          {needsAsOf && (
            <div className="flex items-center gap-2 text-sm">
              <label className="text-text-secondary whitespace-nowrap">{needsStartDate ? "Sampai tanggal:" : "Per tanggal:"}</label>
              <input
                type="date"
                value={asOf}
                onChange={e => navigate({ as_of: e.target.value })}
                className="border border-border rounded px-2 py-1 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40"
              />
            </div>
          )}

          {needsDateRange && (
            <>
              <div className="flex items-center gap-2 text-sm">
                <label className="text-text-secondary whitespace-nowrap">Dari:</label>
                <input
                  type="date"
                  value={periodFrom}
                  onChange={e => navigate({ from: e.target.value })}
                  className="border border-border rounded px-2 py-1 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40"
                />
              </div>
              <div className="flex items-center gap-2 text-sm">
                <label className="text-text-secondary whitespace-nowrap">Sampai:</label>
                <input
                  type="date"
                  value={periodTo}
                  onChange={e => navigate({ to: e.target.value })}
                  className="border border-border rounded px-2 py-1 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40"
                />
              </div>
            </>
          )}

          {needsProject && (
            <div className="flex items-center gap-2 text-sm">
              <label className="text-text-secondary whitespace-nowrap">Proyek:</label>
              <select
                value={projectId}
                onChange={e => navigate({ project_id: e.target.value })}
                className="border border-border rounded px-2 py-1 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40"
              >
                <option value="">— Pilih Proyek —</option>
                {projects.map(p => (
                  <option key={p.id} value={String(p.id)}>{p.name}</option>
                ))}
              </select>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
