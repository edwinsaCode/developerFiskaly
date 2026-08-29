import { apiFetch } from "./client";
import type { CostEntry, CostCategory, PaymentMethod, CostPreviewResponse } from "@/lib/types/api";

export async function fetchCostEntries(token: string, projectId: number): Promise<CostEntry[]> {
  return apiFetch<CostEntry[]>(`/projects/${projectId}/cost-entries`, { token });
}

export interface CreateCostEntryBody {
  category: CostCategory;
  amount: string;
  payment_method: PaymentMethod;
  bank_account_code?: string;
  date: string;
  vendor: string;
  description: string;
  phase_id?: number | null;
  unit_id?: number | null;
  budget_item_id?: number | null;
}

export async function createCostEntry(
  token: string,
  projectId: number,
  body: CreateCostEntryBody,
): Promise<CostEntry> {
  return apiFetch<CostEntry>(`/projects/${projectId}/cost-entries`, {
    method: "POST",
    token,
    body,
  });
}

export async function postCostEntry(token: string, entryId: number): Promise<void> {
  await apiFetch<void>(`/cost-entries/${entryId}/post`, {
    method: "POST",
    token,
  });
}

// previewCostEntry is a dry-run: returns the journal lines that createCostEntry would post,
// without writing anything. Called by frontend before confirming submission.
export async function previewCostEntry(
  token: string,
  projectId: number,
  body: CreateCostEntryBody,
): Promise<CostPreviewResponse> {
  return apiFetch<CostPreviewResponse>(`/projects/${projectId}/cost-entries/preview`, {
    method: "POST",
    token,
    body,
  });
}
