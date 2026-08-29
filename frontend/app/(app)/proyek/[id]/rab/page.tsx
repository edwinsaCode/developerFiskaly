import { notFound } from "next/navigation";
import { getTokenAndRole } from "@/lib/auth";
import { fetchProject, fetchPhases } from "@/lib/api/projects";
import { fetchBudgetPlans, fetchBudgetItems, fetchRABvsRealisasi } from "@/lib/api/budget";
import { ApiError } from "@/lib/api/client";
import type { BudgetItem } from "@/lib/types/api";
import { Card } from "@/components/ui/Card";
import { RABPageClient } from "@/components/rab/RABPageClient";
import { WorkspaceNav } from "@/components/proyek/WorkspaceNav";

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function RABPage({ params }: PageProps) {
  const { id } = await params;
  const projectId = parseInt(id, 10);
  if (isNaN(projectId)) notFound();

  const { token, email, canWrite } = await getTokenAndRole();

  const [projectRes, phasesRes, plansRes] = await Promise.allSettled([
    fetchProject(token, projectId),
    fetchPhases(token, projectId),
    fetchBudgetPlans(token, projectId),
  ]);

  if (projectRes.status === "rejected") {
    const err = projectRes.reason;
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  }

  const project = projectRes.value;
  const phases  = phasesRes.status === "fulfilled" ? phasesRes.value : [];
  const plans   = plansRes.status  === "fulfilled" ? plansRes.value  : [];

  // Fetch items untuk setiap plan secara paralel
  const itemResults = await Promise.allSettled(
    plans.map(p => fetchBudgetItems(token, p.id))
  );

  const plansWithItems = plans.map((plan, i) => ({
    plan,
    items: itemResults[i].status === "fulfilled"
      ? (itemResults[i] as PromiseFulfilledResult<BudgetItem[]>).value
      : [],
  }));

  // RAB vs Realisasi — hanya jika ada plan aktif
  let rabVsRealisasi = null;
  let rabVsRealisasiError = false;
  if (plans.some(p => p.status === "active")) {
    try {
      rabVsRealisasi = await fetchRABvsRealisasi(token, projectId);
    } catch {
      rabVsRealisasiError = true;
    }
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb + workspace nav */}
      <div>
        <p className="text-xs text-text-tertiary">
          <a href="/proyek" className="hover:underline">Proyek</a>
          {" / "}
          <a href={`/proyek/${projectId}`} className="hover:underline">{project.name}</a>
          {" / "}
          <span className="text-text-primary">RAB</span>
        </p>
        <h1 className="display-lg text-2xl text-text-primary mt-1">
          Rencana Anggaran Biaya — {project.name}
        </h1>
        {!canWrite && (
          <p className="text-xs text-warning mt-1">
            Mode hanya-baca. Hubungi owner atau akuntan untuk membuat perubahan.
          </p>
        )}
      </div>

      <WorkspaceNav projectId={projectId} active="rab" />

      {plansRes.status === "rejected" && (
        <Card>
          <p className="text-sm text-danger">Gagal memuat daftar RAB.</p>
        </Card>
      )}

      <RABPageClient
        projectId={projectId}
        plansWithItems={plansWithItems}
        phases={phases}
        userEmail={email}
        canWrite={canWrite}
        rabVsRealisasi={rabVsRealisasi}
        rabVsRealisasiError={rabVsRealisasiError}
      />
    </div>
  );
}
