import { apiFetch } from "./client";

// P0-4 — Completion & HPP True-Up (lihat docs/p0-4-frontend-spec.md).

export type CompletionStatus = "draft" | "completed" | "finalized";
export type TrueupStatus = "draft" | "calculated" | "approved" | "posted" | "cancelled";

export interface CompletionEvent {
  id: number;
  project_id: number;
  status: CompletionStatus;
  completed_at?: string;
  actual_cost_finalized_at?: string;
  created_at: string;
}

export interface TrueupLine {
  id: number;
  run_id: number;
  unit_id: number;
  unit_name_snapshot: string;
  category: string; // land|hard|soft|operational
  is_sold: boolean;
  budgeted_amount: string;
  actual_amount: string;
  variance_amount: string;
  journal_entry_id?: number;
}

export interface TrueupRun {
  id: number;
  project_id: number;
  status: TrueupStatus;
  budget_hpp_total: string;
  actual_cost_total: string;
  variance_total: string;
  calculated_at?: string;
  approved_at?: string;
  posted_at?: string;
  journal_id?: number;
  lines?: TrueupLine[];
}

export interface VariancePreview {
  project_id: number;
  completion_status: CompletionStatus | "";
  basis: string;
  budget_hpp_total: string;
  actual_cost_total: string;
  variance_total: string;
  sold_units: number;
  unsold_units: number;
  lines: TrueupLine[];
}

export async function fetchCompletion(
  token: string,
  projectId: number,
): Promise<CompletionEvent | null> {
  try {
    return await apiFetch<CompletionEvent>(`/projects/${projectId}/completion`, { token });
  } catch {
    return null;
  }
}

export async function markCompleted(
  token: string,
  projectId: number,
  completedDate?: string,
): Promise<CompletionEvent> {
  return apiFetch<CompletionEvent>(`/projects/${projectId}/completion`, {
    method: "POST",
    body: { completed_date: completedDate },
    token,
  });
}

export async function finalizeCompletion(
  token: string,
  projectId: number,
): Promise<CompletionEvent> {
  return apiFetch<CompletionEvent>(`/projects/${projectId}/completion/finalize`, {
    method: "POST",
    body: {},
    token,
  });
}

export async function fetchVariancePreview(
  token: string,
  projectId: number,
): Promise<VariancePreview> {
  return apiFetch<VariancePreview>(`/projects/${projectId}/hpp-variance`, { token });
}

export async function calculateTrueup(
  token: string,
  projectId: number,
): Promise<TrueupRun> {
  return apiFetch<TrueupRun>(`/projects/${projectId}/hpp-trueup/calculate`, {
    method: "POST",
    body: {},
    token,
  });
}

export async function fetchTrueupRuns(
  token: string,
  projectId: number,
): Promise<TrueupRun[]> {
  const res = await apiFetch<TrueupRun[]>(`/projects/${projectId}/hpp-trueup`, { token });
  return res ?? [];
}

export async function fetchTrueupRun(token: string, runId: number): Promise<TrueupRun> {
  return apiFetch<TrueupRun>(`/hpp-trueup/${runId}`, { token });
}

export async function approveTrueup(token: string, runId: number): Promise<TrueupRun> {
  return apiFetch<TrueupRun>(`/hpp-trueup/${runId}/approve`, {
    method: "POST", body: {}, token,
  });
}

export async function postTrueup(
  token: string,
  runId: number,
  postDate?: string,
): Promise<TrueupRun> {
  return apiFetch<TrueupRun>(`/hpp-trueup/${runId}/post`, {
    method: "POST",
    body: { post_date: postDate },
    token,
  });
}

export async function cancelTrueup(token: string, runId: number): Promise<TrueupRun> {
  return apiFetch<TrueupRun>(`/hpp-trueup/${runId}/cancel`, {
    method: "POST", body: {}, token,
  });
}
