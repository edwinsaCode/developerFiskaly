"use client";

import { Tabs, TabList, TabTrigger, TabPanel } from "@/components/ui/Tabs";
import type { BudgetPlan, BudgetItem, ProjectPhase, RABvsRealisasiReport, ConstructionRealisasiTree } from "@/lib/types/api";
import { RABManager } from "./RABManager";
import { RABvsRealisasiSection } from "./RABvsRealisasiSection";

interface PlanWithItems { plan: BudgetPlan; items: BudgetItem[] }

interface RABPageClientProps {
  projectId: number;
  plansWithItems: PlanWithItems[];
  phases: ProjectPhase[];
  userEmail: string;
  canWrite: boolean;
  rabVsRealisasi: RABvsRealisasiReport | null;
  rabVsRealisasiError: boolean;
  constructionTree?: ConstructionRealisasiTree | null;
}

export function RABPageClient({
  projectId,
  plansWithItems,
  phases,
  userEmail,
  canWrite,
  rabVsRealisasi,
  rabVsRealisasiError,
  constructionTree,
}: RABPageClientProps) {
  return (
    <Tabs defaultTab="versi">
      <TabList className="mb-6">
        <TabTrigger id="versi">Versi RAB</TabTrigger>
        <TabTrigger id="realisasi">RAB vs Realisasi</TabTrigger>
      </TabList>
      <TabPanel id="versi">
        <RABManager
          projectId={projectId}
          plansWithItems={plansWithItems}
          phases={phases}
          userEmail={userEmail}
          canWrite={canWrite}
        />
      </TabPanel>
      <TabPanel id="realisasi">
        <RABvsRealisasiSection
          report={rabVsRealisasi}
          error={rabVsRealisasiError}
          constructionTree={constructionTree}
        />
      </TabPanel>
    </Tabs>
  );
}
