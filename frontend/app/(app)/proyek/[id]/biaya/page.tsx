import { notFound } from "next/navigation";
import { getTokenAndRole } from "@/lib/auth";
import { fetchProject, fetchPhases, fetchUnitsByProject, fetchProductTypes } from "@/lib/api/projects";
import { fetchCostEntries } from "@/lib/api/cost";
import { fetchExpenses } from "@/lib/api/expense";
import { fetchBudgetPlans, fetchBudgetItems } from "@/lib/api/budget";
import { ApiError } from "@/lib/api/client";
import type { BudgetItem } from "@/lib/types/api";
import { Card } from "@/components/ui/Card";
import { ErrorState } from "@/components/ui/ErrorState";
import { CostEntryForm } from "@/components/biaya/CostEntryForm";
import { CostEntryTable } from "@/components/biaya/CostEntryTable";
import { WorkspaceNav } from "@/components/proyek/WorkspaceNav";

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function BiayaPage({ params }: PageProps) {
  const { id } = await params;
  const projectId = parseInt(id, 10);
  if (isNaN(projectId)) notFound();

  const { token, canWrite } = await getTokenAndRole();

  const [projectRes, phasesRes, unitsRes, costRes, plansRes, productTypesRes, expenseRes] =
    await Promise.allSettled([
      fetchProject(token, projectId),
      fetchPhases(token, projectId),
      fetchUnitsByProject(token, projectId),
      fetchCostEntries(token, projectId),
      fetchBudgetPlans(token, projectId),
      fetchProductTypes(token),
      // W-10 §16: biaya yang dicatat lewat Transaksi Pengeluaran menulis ke tabel
      // yang SAMA, jadi sudah ikut di costEntries. Yang belum ikut hanya nama
      // jenis pengeluaran dan penanda realisasi RAB — diambil dari sini supaya
      // barisnya terbaca, bukan supaya datanya digandakan.
      fetchExpenses(token, { project_id: projectId }),
    ]);

  if (projectRes.status === "rejected") {
    const err = projectRes.reason;
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  }

  const project     = projectRes.value;
  const phases      = phasesRes.status  === "fulfilled" ? phasesRes.value  : [];
  const units       = unitsRes.status   === "fulfilled" ? unitsRes.value   : [];
  const costEntries = costRes.status    === "fulfilled" ? costRes.value    : [];
  const plans       = plansRes.status   === "fulfilled" ? plansRes.value   : [];

  const expenses = expenseRes.status === "fulfilled" ? expenseRes.value : [];

  // Katalog Produk: biaya bertingkat `direct` menempel ke unit dan baru lepas
  // menjadi HPP saat BAST. Produk NON-PROPERTI tidak pernah mengakui HPP, jadi
  // biayanya akan mengendap di persediaan selamanya — server menolaknya
  // (fail-closed) dan dropdown di bawah tidak menawarkannya sejak awal.
  // Bila katalog gagal dimuat, daftar unit tidak difilter — server tetap
  // menjadi penjaga terakhir, UI tidak menebak kategori.
  const productTypes = productTypesRes.status === "fulfilled" ? productTypesRes.value : [];
  const hppEligible  = new Set(productTypes.filter(p => p.category === "property").map(p => p.code));
  const costableUnits = productTypes.length > 0
    ? units.filter(u => hppEligible.has(u.unit_type))
    : units;

  // Fetch items untuk semua plan (digunakan di form dropdown)
  const activePlan = plans.find(p => p.status === "active" || p.status === "draft");
  let budgetItems: BudgetItem[] = [];
  if (activePlan) {
    try {
      budgetItems = await fetchBudgetItems(token, activePlan.id);
    } catch { /* tidak fatal */ }
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb + judul */}
      <div>
        <p className="text-xs text-text-tertiary">
          <a href="/proyek" className="hover:underline">Proyek</a>
          {" / "}
          <a href={`/proyek/${projectId}`} className="hover:underline">{project.name}</a>
          {" / "}
          <span className="text-text-primary">Biaya</span>
        </p>
        <h1 className="display-lg text-2xl text-text-primary mt-1">
          Input Biaya — {project.name}
        </h1>
        {!canWrite && (
          <p className="text-xs text-warning mt-1">
            Mode hanya-baca. Hubungi owner atau akuntan untuk mencatat biaya.
          </p>
        )}
      </div>

      <WorkspaceNav projectId={projectId} active="biaya" />

      {costRes.status === "rejected" && (
        <Card>
          <ErrorState
            title="Gagal Memuat Biaya"
            description="Daftar biaya tidak dapat dimuat dari server."
          />
        </Card>
      )}

      {/* Form input biaya — hanya untuk owner/accountant */}
      {canWrite && (
        <CostEntryForm
          token={token}
          projectId={projectId}
          phases={phases}
          units={costableUnits}
          budgetItems={budgetItems}
        />
      )}

      {/* Daftar biaya */}
      <CostEntryTable entries={costEntries} expenses={expenses} canWrite={canWrite} />
    </div>
  );
}
