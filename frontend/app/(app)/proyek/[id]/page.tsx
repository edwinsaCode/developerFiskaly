import { notFound } from "next/navigation";
import { fetchProject, fetchPhases, fetchUnitsByProject } from "@/lib/api/projects";
import { fetchAllocationCompute } from "@/lib/api/allocation";
import { fetchLandStock } from "@/lib/api/land";
import { ApiError } from "@/lib/api/client";
import { getTokenAndRole } from "@/lib/auth";
import { AllocationResult, Unit, UnitStatus } from "@/lib/types/api";
import { Card } from "@/components/ui/Card";
import { ErrorState } from "@/components/ui/ErrorState";
import { EmptyState } from "@/components/ui/EmptyState";
import { StatusBadge } from "@/components/ui/Badge";
import { Tanggal } from "@/components/format/Tanggal";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { ProyekHeader } from "@/components/proyek/ProyekDetailTabs";
import { WorkspaceNav } from "@/components/proyek/WorkspaceNav";
import { MARKETING_TABS, type WorkspaceTab } from "@/components/proyek/tabs";
import { OverviewTab } from "@/components/proyek/workspace/OverviewTab";
import { PenjualanTab } from "@/components/proyek/workspace/PenjualanTab";
import { TimelineTab } from "@/components/proyek/workspace/TimelineTab";
import { AlokasiTab } from "@/components/proyek/workspace/AlokasiTab";
import { KelebihanTanahTab } from "@/components/proyek/workspace/KelebihanTanahTab";
import { UnitBoard } from "@/components/proyek/UnitBoard";
import { ProdukTambahanCard } from "@/components/proyek/ProdukTambahanCard";
import { AddUnitButton } from "@/components/proyek/AddUnitButton";
import { BulkUnitWizard } from "@/components/proyek/BulkUnitWizard";
import { BookingBoard } from "@/components/booking/BookingBoard";
import { ClosingPanel } from "@/components/closing/ClosingPanel";

function countByStatus(units: Unit[]): Record<UnitStatus | "total", number> {
  const c: Record<UnitStatus | "total", number> = {
    available: 0, booked: 0, reserved: 0, ppjb: 0, sold: 0,
    occupied: 0, hold: 0, blocked: 0, maintenance: 0, total: units.length,
  };
  for (const u of units) c[u.status]++;
  return c;
}

// PS-2 — Workspace Project: semua area proyek dalam satu tempat, URL ?tab=.
const VALID_TABS = new Set([
  "overview", "unit", "penjualan", "booking", "alokasi", "closing", "timeline", "dokumen", "tanah",
]);

interface PageProps {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ tab?: string }>;
}

export default async function ProyekWorkspacePage({ params, searchParams }: PageProps) {
  const { id } = await params;
  const { tab: tabParam } = await searchParams;
  const projectId = parseInt(id, 10);
  if (isNaN(projectId)) notFound();

  const { token, role } = await getTokenAndRole();
  const isMarketing = role === "marketing";

  // Alias lama (PS-1): penyelesaian → closing.
  let tab = (tabParam === "penyelesaian" ? "closing" : tabParam) ?? "overview";
  if (!VALID_TABS.has(tab)) tab = "overview";
  // Marketing tidak punya tab Overview (KPI keuangan). Mendaratkannya di sana
  // berarti menyambutnya dengan panel kosong; Unit adalah pintu masuk yang benar.
  if (isMarketing && !MARKETING_TABS.includes(tab as WorkspaceTab)) tab = "unit";

  const [projectRes, phasesRes, unitsRes, allocRes, landStockRes] = await Promise.allSettled([
    fetchProject(token, projectId),
    fetchPhases(token, projectId),
    fetchUnitsByProject(token, projectId),
    // Alokasi HPP adalah data biaya: marketing dijawab 403 oleh backend.
    // Tidak perlu menembakkan request yang sudah pasti ditolak.
    isMarketing
      ? Promise.resolve({ data: [] as AllocationResult[] })
      : fetchAllocationCompute(token, projectId),
    fetchLandStock(token, projectId),
  ]);

  if (projectRes.status === "rejected") {
    const err = projectRes.reason;
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  }

  const project = projectRes.value;
  const phases = phasesRes.status === "fulfilled" ? phasesRes.value : [];
  const units  = unitsRes.status  === "fulfilled" ? unitsRes.value  : [];
  const allocationResults: AllocationResult[] =
    allocRes.status === "fulfilled" ? allocRes.value.data : [];
  const allocationError = allocRes.status === "rejected";
  const landStock = landStockRes.status === "fulfilled" ? landStockRes.value : null;

  const counts = countByStatus(units);

  return (
    <div>
      <ProyekHeader project={project} unitCounts={counts} />
      <WorkspaceNav projectId={projectId} active={tab as WorkspaceTab} />

      {unitsRes.status === "rejected" && (
        <Card className="mb-4">
          <ErrorState
            title="Gagal Memuat Unit"
            description="Data unit proyek tidak dapat dimuat."
            showRetry={false}
          />
        </Card>
      )}

      {tab === "overview" && <OverviewTab token={token} projectId={projectId} />}

      {tab === "unit" && (
        <div className="space-y-6">
          <div className="flex justify-end gap-2">
            <AddUnitButton token={token} projectId={projectId} phases={phases} />
            <BulkUnitWizard token={token} projectId={projectId} phases={phases} />
          </div>
          <UnitBoard units={units} phases={phases} projectId={projectId} />
          <ProdukTambahanCard pool={landStock} projectId={projectId} />
          {phases.length > 0 && (
            <Card padding="none">
              <div className="px-5 py-3 border-b border-border">
                <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary">Fase</p>
              </div>
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
                          <span className="font-medium">{phase.name}</span>
                          {phase.description && (
                            <p className="text-xs text-text-tertiary mt-0.5">{phase.description}</p>
                          )}
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
        </div>
      )}

      {tab === "penjualan" && <PenjualanTab units={units} />}

      {tab === "booking" && (
        <BookingBoard token={token} projectFilter={projectId} embedded />
      )}

      {tab === "alokasi" && (
        <AlokasiTab
          token={token}
          projectId={projectId}
          units={units}
          allocationResults={allocationResults}
          allocationError={allocationError}
        />
      )}

      {tab === "closing" && <ClosingPanel token={token} projectId={projectId} />}

      {tab === "tanah" && <KelebihanTanahTab token={token} projectId={projectId} />}

      {tab === "timeline" && (
        <TimelineTab token={token} projectId={projectId} units={units} />
      )}

      {tab === "dokumen" && (
        <EmptyState
          title="Manajemen Dokumen — segera hadir"
          description="PPJB, AJB, SHM, PBG/IMB, dan berita acara BAST akan tersimpan per unit/proyek di sini (roadmap blueprint: Document Management)."
        />
      )}
    </div>
  );
}
