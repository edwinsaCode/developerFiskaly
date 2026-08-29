"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import { apiFetch } from "@/lib/api/client";
import type { BudgetPlan, BudgetItem, BudgetCategory } from "@/lib/types/api";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

export async function createBudgetPlanAction(
  projectId: number,
  body: { label: string; phase_id?: number | null; notes?: string },
): Promise<BudgetPlan> {
  const t = await token();
  const result = await apiFetch<BudgetPlan>(`/projects/${projectId}/budget-plans`, {
    method: "POST",
    token: t,
    body,
  });
  revalidatePath(`/proyek/${projectId}/rab`);
  return result;
}

export async function addBudgetItemAction(
  projectId: number,
  planId: number,
  body: { category: BudgetCategory; subcategory?: string; description?: string; budgeted_amount: string },
): Promise<BudgetItem> {
  const t = await token();
  const result = await apiFetch<BudgetItem>(`/budget-plans/${planId}/items`, {
    method: "POST",
    token: t,
    body,
  });
  revalidatePath(`/proyek/${projectId}/rab`);
  return result;
}

export async function deleteBudgetItemAction(
  projectId: number,
  planId: number,
  itemId: number,
): Promise<void> {
  const t = await token();
  await apiFetch<void>(`/budget-plans/${planId}/items/${itemId}`, {
    method: "DELETE",
    token: t,
  });
  revalidatePath(`/proyek/${projectId}/rab`);
}

export async function approveBudgetPlanAction(
  projectId: number,
  planId: number,
  approvedBy: string,
): Promise<BudgetPlan> {
  const t = await token();
  const result = await apiFetch<BudgetPlan>(`/budget-plans/${planId}/approve`, {
    method: "POST",
    token: t,
    body: { approved_by: approvedBy },
  });
  revalidatePath(`/proyek/${projectId}/rab`);
  return result;
}
