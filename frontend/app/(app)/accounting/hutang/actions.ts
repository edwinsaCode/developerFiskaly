"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import type {
  APInvoice,
  APInvoiceView,
  APPreviewResult,
  BudgetItem,
  Unit,
} from "@/lib/types/api";
import {
  createAPInvoice,
  fetchAPInvoice,
  postAPInvoice,
  previewAPInvoice,
  reverseAPInvoice,
  type APInvoiceBody,
} from "@/lib/api/ap";
import { fetchUnitsByProject } from "@/lib/api/projects";
import { fetchBudgetItems, fetchBudgetPlans } from "@/lib/api/budget";
import { errStatus, errText, type ApResult } from "@/components/ap/apRules";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

// previewInvoiceAction: dry-run. Angka dan baris jurnal yang muncul di layar
// pratinjau dihitung backend dengan jalur yang SAMA dengan create — bukan
// tiruan yang "seharusnya" setara. Pratinjau yang bisa berbeda dari hasilnya
// lebih buruk daripada tidak ada pratinjau sama sekali.
export async function previewInvoiceAction(
  body: APInvoiceBody,
): Promise<ApResult<APPreviewResult>> {
  try {
    return { ok: true, data: await previewAPInvoice(await token(), body) };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

// createInvoiceAction menyimpan sebagai DRAFT. Draft belum menjadi kewajiban
// siapa pun: tidak ada saldo hutang, tidak ada realisasi RAB, tidak ada baris
// umur hutang.
export async function createInvoiceAction(
  body: APInvoiceBody,
): Promise<ApResult<APInvoice>> {
  try {
    const inv = await createAPInvoice(await token(), body);
    revalidatePath("/accounting/hutang");
    return { ok: true, data: inv };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

// postInvoiceAction mengakui kewajiban. Sejak titik ini jurnalnya terposting
// dan biayanya terbaca sebagai realisasi RAB.
export async function postInvoiceAction(id: number): Promise<ApResult<APInvoice>> {
  try {
    const inv = await postAPInvoice(await token(), id);
    revalidatePath("/accounting/hutang");
    revalidatePath(`/accounting/hutang/${id}`);
    if (inv.project_id) revalidatePath(`/proyek/${inv.project_id}/biaya`);
    return { ok: true, data: inv };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

// reverseInvoiceAction membalik lewat jurnal pembalik — tagihan aslinya tidak
// dihapus dan tidak diedit (ledger append-only).
export async function reverseInvoiceAction(
  id: number,
  reverseDate?: string,
): Promise<ApResult<APInvoice>> {
  try {
    const inv = await reverseAPInvoice(await token(), id, reverseDate);
    revalidatePath("/accounting/hutang");
    revalidatePath(`/accounting/hutang/${id}`);
    if (inv.project_id) revalidatePath(`/proyek/${inv.project_id}/biaya`);
    return { ok: true, data: inv };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

export async function fetchInvoiceAction(id: number): Promise<ApResult<APInvoiceView>> {
  try {
    return { ok: true, data: await fetchAPInvoice(await token(), id) };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

// Konteks proyek dimuat saat proyek dipilih, bukan di muka: daftar unit dan
// item RAB milik satu proyek, dan memuat semuanya sekaligus akan sia-sia.
export async function fetchProjectContextAction(
  projectId: number,
): Promise<{ units: Unit[]; budgetItems: BudgetItem[]; budgetPlanLabel: string }> {
  const t = await token();

  const [units, plans] = await Promise.all([
    fetchUnitsByProject(t, projectId).catch(() => [] as Unit[]),
    fetchBudgetPlans(t, projectId).catch(() => []),
  ]);

  // Hanya RAB aktif yang boleh ditautkan — versi superseded adalah jejak audit,
  // bukan target realisasi.
  const active = plans.find((p) => p.status === "active");
  if (!active) return { units, budgetItems: [], budgetPlanLabel: "" };

  const budgetItems = await fetchBudgetItems(t, active.id).catch(() => [] as BudgetItem[]);
  return { units, budgetItems, budgetPlanLabel: active.label || `Versi ${active.version}` };
}
