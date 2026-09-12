import { apiFetch } from "./client";
import type { BudgetPlan, BudgetItem, BudgetCategory, RABvsRealisasiReport, ConstructionRealisasiTree } from "@/lib/types/api";

export async function fetchBudgetPlans(token: string, projectId: number): Promise<BudgetPlan[]> {
  return apiFetch<BudgetPlan[]>(`/projects/${projectId}/budget-plans`, { token });
}

export async function createBudgetPlan(
  token: string,
  projectId: number,
  body: { label: string; phase_id?: number | null; notes?: string },
): Promise<BudgetPlan> {
  return apiFetch<BudgetPlan>(`/projects/${projectId}/budget-plans`, {
    method: "POST",
    token,
    body,
  });
}

export async function fetchBudgetItems(token: string, planId: number): Promise<BudgetItem[]> {
  return apiFetch<BudgetItem[]>(`/budget-plans/${planId}/items`, { token });
}

export async function addBudgetItem(
  token: string,
  planId: number,
  body: { category: BudgetCategory; subcategory?: string; description?: string; budgeted_amount: string },
): Promise<BudgetItem> {
  return apiFetch<BudgetItem>(`/budget-plans/${planId}/items`, {
    method: "POST",
    token,
    body,
  });
}

export async function deleteBudgetItem(
  token: string,
  planId: number,
  itemId: number,
): Promise<void> {
  await apiFetch<void>(`/budget-plans/${planId}/items/${itemId}`, {
    method: "DELETE",
    token,
  });
}

export async function approveBudgetPlan(
  token: string,
  planId: number,
  approvedBy: string,
): Promise<BudgetPlan> {
  return apiFetch<BudgetPlan>(`/budget-plans/${planId}/approve`, {
    method: "POST",
    token,
    body: { approved_by: approvedBy },
  });
}

export async function fetchRABvsRealisasi(
  token: string,
  projectId: number,
): Promise<RABvsRealisasiReport> {
  return apiFetch<RABvsRealisasiReport>(
    `/projects/${projectId}/budget/rab-vs-realisasi`,
    { token },
  );
}

// Detail RAB Konstruksi (Produksi Subsidi/Komersial, Sarana & Prasarana,
// Perizinan) sampai ke item RAB individual — dipakai untuk pecah baris
// "Konstruksi" pada RAB vs Realisasi menjadi hierarki penuh.
export async function fetchConstructionRealisasi(
  token: string,
  projectId: number,
): Promise<ConstructionRealisasiTree> {
  return apiFetch<ConstructionRealisasiTree>(
    `/projects/${projectId}/budget/realisasi-konstruksi`,
    { token },
  );
}
