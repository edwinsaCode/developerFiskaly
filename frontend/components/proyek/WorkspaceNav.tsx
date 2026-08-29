"use client";

// PS-2 — Workspace Project: satu bar tab konsisten untuk seluruh halaman proyek.
// URL konsisten: /proyek/[id]?tab=... (+ /proyek/[id]/rab & /biaya sebagai rute
// server-page yang memakai bar yang sama).

import Link from "next/link";
import { useUserSafe } from "@/lib/context/UserContext";
import { MARKETING_TABS, type WorkspaceTab } from "@/components/proyek/tabs";

export type { WorkspaceTab };

const TABS: { key: WorkspaceTab; label: string; href: (id: number) => string }[] = [
  { key: "overview", label: "Overview", href: (id) => `/proyek/${id}` },
  { key: "unit", label: "Unit", href: (id) => `/proyek/${id}?tab=unit` },
  { key: "penjualan", label: "Penjualan", href: (id) => `/proyek/${id}?tab=penjualan` },
  { key: "booking", label: "Booking", href: (id) => `/proyek/${id}?tab=booking` },
  { key: "rab", label: "RAB", href: (id) => `/proyek/${id}/rab` },
  { key: "biaya", label: "Biaya", href: (id) => `/proyek/${id}/biaya` },
  { key: "alokasi", label: "Alokasi HPP", href: (id) => `/proyek/${id}?tab=alokasi` },
  { key: "tanah", label: "Kelebihan Tanah", href: (id) => `/proyek/${id}?tab=tanah` },
  { key: "closing", label: "Closing", href: (id) => `/proyek/${id}?tab=closing` },
  { key: "timeline", label: "Timeline", href: (id) => `/proyek/${id}?tab=timeline` },
  { key: "dokumen", label: "Dokumen", href: (id) => `/proyek/${id}?tab=dokumen` },
];

export function WorkspaceNav({ projectId, active }: { projectId: number; active: WorkspaceTab }) {
  const user = useUserSafe();
  const tabs = user?.role === "marketing"
    ? TABS.filter((t) => MARKETING_TABS.includes(t.key))
    : TABS;

  return (
    <div className="mb-5 flex gap-1 overflow-x-auto scrollbar-none border-b border-border" role="tablist">
      {tabs.map((t) => (
        <Link
          key={t.key}
          href={t.href(projectId)}
          role="tab"
          aria-selected={active === t.key}
          className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm transition-colors
            ${active === t.key
              ? "border-accent font-medium text-accent"
              : "border-transparent text-text-secondary hover:text-accent hover:border-accent/30"}`}
        >
          {t.label}
        </Link>
      ))}
    </div>
  );
}
