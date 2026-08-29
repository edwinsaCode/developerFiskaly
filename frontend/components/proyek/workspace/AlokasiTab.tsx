"use client";

// PS-2 — Tab Alokasi HPP workspace (pindahan dari ProyekDetailTabs).

import { useState } from "react";
import { Card } from "@/components/ui/Card";
import { AllocationConfig } from "@/components/allocation/AllocationConfig";
import { AllocationPreview } from "@/components/allocation/AllocationPreview";
import { AllocationHistory } from "@/components/allocation/AllocationHistory";
import { ProjectCostTable } from "@/components/proyek/CostBreakdownTable";
import type { AllocationResult, Unit } from "@/lib/types/api";

export function AlokasiTab({
  token, projectId, units, allocationResults, allocationError,
}: {
  token: string;
  projectId: number;
  units: Unit[];
  allocationResults: AllocationResult[];
  allocationError: boolean;
}) {
  const [allocTrigger, setAllocTrigger] = useState(0);
  const [historyTrigger, setHistoryTrigger] = useState(0);
  const unitMap = Object.fromEntries(units.map((u) => [u.id, u]));

  return (
    <div className="space-y-6">
      <AllocationConfig
        token={token}
        projectId={projectId}
        onConfigSaved={() => setAllocTrigger((k) => k + 1)}
        onExecuted={() => setHistoryTrigger((k) => k + 1)}
      />
      <AllocationPreview
        token={token}
        projectId={projectId}
        units={units}
        triggerKey={allocTrigger}
      />
      {allocationError ? (
        <Card>
          <p className="text-sm text-text-secondary">
            Hasil biaya per unit belum tersedia — konfigurasi basis alokasi terlebih dahulu.
          </p>
        </Card>
      ) : (
        <ProjectCostTable results={allocationResults} unitMap={unitMap} />
      )}
      <AllocationHistory token={token} projectId={projectId} refreshKey={historyTrigger} />
    </div>
  );
}
