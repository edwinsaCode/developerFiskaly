"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import type { BudgetItem, FixedAssetCategory, Unit } from "@/lib/types/api";
import {
  previewExpense,
  createExpense,
  isFixedAssetResult,
  type ExpenseBody,
  type ExpenseCreateResult,
  type ExpensePreviewResult,
} from "@/lib/api/expense";
import { fetchFixedAssetCategories } from "@/lib/api/fixedAsset";
import { fetchUnitsByProject } from "@/lib/api/projects";
import { fetchBudgetPlans, fetchBudgetItems } from "@/lib/api/budget";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

// previewExpenseAction: dry-run. Jurnal yang tampil di layar konfirmasi dihitung
// backend dengan jalur resolusi akun yang SAMA dengan create — bukan tebakan UI.
export async function previewExpenseAction(body: ExpenseBody): Promise<ExpensePreviewResult> {
  return previewExpense(await token(), body);
}

// createExpenseAction: catat + posting dalam SATU permintaan. Backend menjalankan
// keduanya dalam satu transaksi (§7) — tidak ada jendela di mana jurnal sudah
// terbit tapi transaksinya belum tercatat, atau sebaliknya. Jalur fixed_asset
// menerbitkan baris Register, bukan ExpenseListItem — revalidate mengikuti.
export async function createExpenseAction(body: ExpenseBody): Promise<ExpenseCreateResult> {
  const item = await createExpense(await token(), body);
  if (isFixedAssetResult(item)) {
    revalidatePath("/accounting/aset-tetap");
  } else {
    revalidatePath("/accounting/pengeluaran");
    if (item.project_id) revalidatePath(`/proyek/${item.project_id}/biaya`);
  }
  return item;
}

export async function fetchFixedAssetCategoriesAction(): Promise<FixedAssetCategory[]> {
  return fetchFixedAssetCategories(await token());
}

// Konteks proyek dimuat saat proyek dipilih, bukan di muka: daftar unit dan item
// RAB milik proyek tertentu, dan memuat semuanya sekaligus akan sia-sia.
export async function fetchProjectContextAction(
  projectId: number,
): Promise<{ units: Unit[]; budgetItems: BudgetItem[]; budgetPlanLabel: string }> {
  const t = await token();

  const [units, plans] = await Promise.all([
    fetchUnitsByProject(t, projectId).catch(() => [] as Unit[]),
    fetchBudgetPlans(t, projectId).catch(() => []),
  ]);

  // Hanya RAB yang aktif yang boleh ditautkan — versi superseded adalah jejak
  // audit, bukan target realisasi.
  const active = plans.find((p) => p.status === "active");
  if (!active) return { units, budgetItems: [], budgetPlanLabel: "" };

  const budgetItems = await fetchBudgetItems(t, active.id).catch(() => [] as BudgetItem[]);
  return { units, budgetItems, budgetPlanLabel: active.label || `Versi ${active.version}` };
}
