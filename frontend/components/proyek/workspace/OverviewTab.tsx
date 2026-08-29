"use client";

// PS-2 — Overview proyek: KPI dari /reports/dashboard (baris proyek ini).

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Rupiah } from "@/components/format/Rupiah";
import { fetchDashboard, type DashboardProject } from "@/lib/api/reports";
import { ProjectControlCard } from "./ProjectControlCard";

function KV({ label, value, sub }: { label: string; value: React.ReactNode; sub?: string }) {
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <p className="text-xs text-text-secondary">{label}</p>
      <p className="mt-1 text-lg font-semibold tabular-nums text-text-primary">{value}</p>
      {sub && <p className="mt-0.5 text-[11px] text-text-tertiary">{sub}</p>}
    </div>
  );
}

function Bar({ pct, cls = "bg-accent" }: { pct: string; cls?: string }) {
  const n = Math.min(parseFloat(pct) || 0, 100);
  return (
    <div className="mt-2 flex h-1.5 w-full overflow-hidden rounded-full bg-border-subtle">
      <div className={cls} style={{ width: `${n}%` }} />
    </div>
  );
}

export function OverviewTab({ token, projectId }: { token: string; projectId: number }) {
  const [p, setP] = useState<DashboardProject | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    fetchDashboard(token)
      .then((d) => setP(d.projects.find((x) => x.id === projectId) ?? null))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [token, projectId]);

  useEffect(() => { load(); }, [load]);

  if (loading) {
    return (
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {[...Array(8)].map((_, i) => (
          <div key={i} className="h-24 animate-pulse rounded-lg bg-border-subtle" />
        ))}
      </div>
    );
  }
  if (!p) {
    return (
      <Card padding="sm">
        <p className="text-sm text-text-secondary">Ringkasan belum tersedia untuk proyek ini.</p>
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <KV
          label="Progress Biaya vs RAB"
          value={`${Math.max(parseFloat(p.progress_pct) || 0, 0).toFixed(2)}%`}
          sub={undefined}
        />
        <KV label="Pendapatan Diakui" value={<Rupiah value={p.revenue} colorSign={false} />} />
        <KV label="HPP" value={<Rupiah value={p.hpp} colorSign={false} />} />
        <KV
          label="Margin"
          value={
            <span className={p.margin.startsWith("-") ? "text-danger" : "text-success"}>
              <Rupiah value={p.margin} colorSign={false} />
            </span>
          }
          sub={`${p.margin_pct}% dari pendapatan`}
        />
        <KV label="RAB Aktif" value={<Rupiah value={p.budget_total} colorSign={false} />} />
        <KV label="Biaya Aktual (kapitalisasi)" value={<Rupiah value={p.actual_cost} colorSign={false} />} />
        <KV label="Nilai Kontrak Aktif" value={<Rupiah value={p.contract_value} colorSign={false} />} />
        <KV
          label="Collection"
          value={`${p.collection_pct}%`}
          sub={undefined}
        />
      </div>

      {/* Funnel unit */}
      <Card padding="sm">
        <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary mb-2">
          Funnel Unit ({p.units_total})
        </p>
        <div className="grid grid-cols-3 sm:grid-cols-6 gap-3 text-center text-sm">
          {[
            { label: "Tersedia", n: p.units_available, tab: "unit" },
            { label: "Dibooking", n: p.units_booked, tab: "booking" },
            { label: "Dipesan", n: p.units_reserved, tab: "penjualan" },
            { label: "PPJB", n: p.units_ppjb, tab: "penjualan" },
            { label: "Terjual", n: p.units_sold, tab: "penjualan" },
            { label: "Lainnya", n: p.units_other, tab: "unit" },
          ].map((s) => (
            <Link
              key={s.label}
              href={`/proyek/${projectId}?tab=${s.tab}`}
              className="rounded-md border border-border p-2 hover:border-accent transition-colors"
            >
              <p className="text-lg font-semibold tabular-nums">{s.n}</p>
              <p className="text-[11px] text-text-secondary">{s.label}</p>
            </Link>
          ))}
        </div>
      </Card>

      {/* R3 — Kartu Kontrol Proyek: Fisik (manual) vs Biaya vs Collection */}
      <ProjectControlCard
        token={token}
        projectId={projectId}
        costPct={p.progress_pct}
        collectionPct={p.collection_pct}
        onChanged={load}
      />
    </div>
  );
}
