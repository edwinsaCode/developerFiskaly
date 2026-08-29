"use client";

import { useState } from "react";
import Link from "next/link";
import { Project, ProjectPhase, Unit, AllocationResult } from "@/lib/types/api";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/components/ui/Tabs";
import { StatusBadge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { UnitBoard } from "./UnitBoard";
import { AddUnitButton } from "./AddUnitButton";
import { BulkUnitWizard } from "./BulkUnitWizard";
import { ProjectCostTable } from "./CostBreakdownTable";
import { ClosingPanel } from "@/components/closing/ClosingPanel";
import { Tanggal } from "@/components/format/Tanggal";
import { AllocationConfig } from "@/components/allocation/AllocationConfig";
import { AllocationPreview } from "@/components/allocation/AllocationPreview";
import { AllocationHistory } from "@/components/allocation/AllocationHistory";

interface ProyekDetailTabsProps {
  project: Project;
  phases: ProjectPhase[];
  units: Unit[];
  allocationResults: AllocationResult[];
  allocationError: boolean;
  token: string;
}

export function ProyekDetailTabs({
  project,
  phases,
  units,
  allocationResults,
  allocationError,
  token,
}: ProyekDetailTabsProps) {
  const unitMap = Object.fromEntries(units.map(u => [u.id, u]));
  const [allocTrigger, setAllocTrigger]     = useState(0);
  const [historyTrigger, setHistoryTrigger] = useState(0);

  return (
    <Tabs defaultTab="board">
      <TabList className="mb-6">
        <TabTrigger id="board">Papan Unit</TabTrigger>
        <TabTrigger id="fase">Fase</TabTrigger>
        <TabTrigger id="biaya">Biaya</TabTrigger>
        <TabTrigger id="alokasi">Alokasi HPP</TabTrigger>
        <TabTrigger id="penyelesaian">Penyelesaian</TabTrigger>
      </TabList>

      {/* ── Papan Unit ────────────────────────────────────────────────────────── */}
      <TabPanel id="board">
        <div className="flex justify-end mb-3">
          <AddUnitButton token={token} projectId={project.id} phases={phases} />
          <BulkUnitWizard token={token} projectId={project.id} phases={phases} />
        </div>
        <UnitBoard units={units} phases={phases} projectId={project.id} />
      </TabPanel>

      {/* ── Fase ──────────────────────────────────────────────────────────────── */}
      <TabPanel id="fase">
        {phases.length === 0 ? (
          <EmptyState
            title="Belum ada fase"
            description="Fase proyek belum ditambahkan."
          />
        ) : (
          <Card padding="none">
            <Table>
              <TableHead>
                <TableRow>
                  <Th>Nama Fase</Th>
                  <Th>Status</Th>
                  <Th right>Target Unit</Th>
                  <Th right>Unit Aktual</Th>
                  <Th>Dibuat</Th>
                </TableRow>
              </TableHead>
              <TableBody>
                {phases.map(phase => {
                  const phaseUnits = units.filter(u => u.phase_id === phase.id);
                  return (
                    <TableRow key={phase.id}>
                      <Td>
                        <div>
                          <span className="font-medium">{phase.name}</span>
                          {phase.description && (
                            <p className="text-xs text-text-tertiary mt-0.5">{phase.description}</p>
                          )}
                        </div>
                      </Td>
                      <Td><StatusBadge status={phase.status} /></Td>
                      <Td right>{phase.target_units}</Td>
                      <Td right>{phaseUnits.length}</Td>
                      <Td><Tanggal value={phase.created_at} /></Td>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Card>
        )}
      </TabPanel>

      {/* ── Biaya ─────────────────────────────────────────────────────────────── */}
      <TabPanel id="biaya">
        {allocationError ? (
          <Card>
            <p className="text-sm text-text-secondary">
              Data alokasi tidak tersedia — konfigurasi alokasi mungkin belum diatur untuk proyek ini.
            </p>
          </Card>
        ) : (
          <ProjectCostTable results={allocationResults} unitMap={unitMap} />
        )}
      </TabPanel>

      {/* ── Alokasi HPP ───────────────────────────────────────────────────────── */}
      <TabPanel id="alokasi">
        <div className="space-y-6">
          <AllocationConfig
            token={token}
            projectId={project.id}
            onConfigSaved={() => setAllocTrigger((k) => k + 1)}
            onExecuted={() => setHistoryTrigger((k) => k + 1)}
          />
          <AllocationPreview
            token={token}
            projectId={project.id}
            units={units}
            triggerKey={allocTrigger}
          />
          <AllocationHistory
            token={token}
            projectId={project.id}
            refreshKey={historyTrigger}
          />
        </div>
      </TabPanel>

      {/* P0-4 — Closing Wizard (Completion & HPP True-Up) */}
      <TabPanel id="penyelesaian">
        <ClosingPanel token={token} projectId={project.id} />
      </TabPanel>
    </Tabs>
  );
}

// ── Header detail proyek (reusable) ───────────────────────────────────────────

interface ProyekHeaderProps {
  project: Project;
  unitCounts: { available: number; reserved: number; sold: number; total: number };
}

export function ProyekHeader({ project, unitCounts }: ProyekHeaderProps) {
  return (
    <div className="mb-6">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-1.5 text-xs text-text-tertiary mb-3">
        <Link href="/proyek" className="hover:text-text-secondary transition-colors">
          Proyek
        </Link>
        <span>/</span>
        <span className="text-text-primary">{project.name}</span>
      </nav>

      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <div className="flex items-center gap-3 mb-1">
            <h1 className="display-lg text-2xl text-text-primary">{project.name}</h1>
            <StatusBadge status={project.status} />
          </div>
          <div className="flex items-center gap-4 text-sm text-text-secondary flex-wrap">
            {project.land_area && project.land_area !== "0.0000" && (
              <span>Luas tanah: <strong className="text-text-primary">{project.land_area} m²</strong></span>
            )}
            {project.start_date && (
              <span>Mulai: <strong className="text-text-primary"><Tanggal value={project.start_date} /></strong></span>
            )}
            {project.notes && (
              <span className="text-text-tertiary">{project.notes}</span>
            )}
          </div>
        </div>

        {/* Ringkasan pipeline unit */}
        <div className="flex items-center gap-3">
          <PipelinePill color="bg-accent/20 text-accent"    label="Tersedia" count={unitCounts.available} />
          <PipelinePill color="bg-warning-bg text-warning"  label="Dipesan"  count={unitCounts.reserved} />
          <PipelinePill color="bg-success-bg text-success"  label="Terjual"  count={unitCounts.sold} />
          <PipelinePill color="bg-border-subtle text-text-secondary" label="Total" count={unitCounts.total} />
        </div>
      </div>
    </div>
  );
}

function PipelinePill({ color, label, count }: { color: string; label: string; count: number }) {
  return (
    <div className={`rounded-lg px-3 py-2 text-center min-w-[64px] ${color}`}>
      <p className="text-lg font-bold leading-none">{count}</p>
      <p className="text-xs mt-0.5">{label}</p>
    </div>
  );
}
