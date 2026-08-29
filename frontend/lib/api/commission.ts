import { apiFetch } from "./client";

// Increment 9 — Commission Engine (lihat docs/increment-9-frontend-spec.md).

export type CommissionStatus =
  | "calculated"
  | "approved"
  | "payable"
  | "paid"
  | "cancelled"
  | "clawed_back";

export interface CommissionRule {
  id: number;
  name: string;
  basis: "percent_of_sale" | "flat_per_unit" | "tiered";
  rate: string;        // fraksi, mis. "0.025"
  flat_amount: string;
  trigger_event: string;
  sales_person_id?: number;
  project_id?: number;
  unit_type?: string;
  effective_from: string;
  effective_to?: string;
  is_active: boolean;
}

export interface Commission {
  id: number;
  rule_id: number;
  sale_record_id: number;
  project_id: number;
  unit_id: number;
  sales_person_id: number;
  basis_amount: string;
  rate_snapshot: string;
  amount: string;
  status: CommissionStatus;
  accrual_journal_id?: number;
  payment_journal_id?: number;
  clawback_journal_id?: number;
  paid_at?: string;
  cancel_reason?: string;
  created_at: string;
}

export interface CreateRuleInput {
  name: string;
  basis: "percent_of_sale" | "flat_per_unit";
  rate?: string;        // "0.025"
  flat_amount?: string; // rupiah bulat
  sales_person_id?: number;
  project_id?: number;
  unit_type?: string;
  effective_from: string; // YYYY-MM-DD
  effective_to?: string;
}

export async function fetchCommissionRules(token: string): Promise<CommissionRule[]> {
  const res = await apiFetch<CommissionRule[]>(`/commission-rules`, { token });
  return res ?? [];
}

export async function createCommissionRule(
  token: string,
  data: CreateRuleInput,
): Promise<CommissionRule> {
  return apiFetch<CommissionRule>(`/commission-rules`, {
    method: "POST", body: data, token,
  });
}

export async function setCommissionRuleActive(
  token: string,
  id: number,
  active: boolean,
): Promise<CommissionRule> {
  return apiFetch<CommissionRule>(`/commission-rules/${id}/active`, {
    method: "PUT", body: { active }, token,
  });
}

export async function fetchCommissions(
  token: string,
  status?: CommissionStatus | "",
  salesPersonId?: number,
): Promise<Commission[]> {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  if (salesPersonId) params.set("sales_person_id", String(salesPersonId));
  const qs = params.toString() ? `?${params.toString()}` : "";
  const res = await apiFetch<Commission[]>(`/commissions${qs}`, { token });
  return res ?? [];
}

export async function calculateCommissions(token: string): Promise<{ created: number }> {
  return apiFetch<{ created: number }>(`/commissions/calculate`, {
    method: "POST", body: {}, token,
  });
}

export async function syncCommissionCancellations(
  token: string,
): Promise<{ cancelled: number; clawed_back: number }> {
  return apiFetch<{ cancelled: number; clawed_back: number }>(
    `/commissions/sync-cancellations`,
    { method: "POST", body: {}, token },
  );
}

export async function approveCommission(token: string, id: number): Promise<Commission> {
  return apiFetch<Commission>(`/commissions/${id}/approve`, {
    method: "POST", body: {}, token,
  });
}

export async function makeCommissionPayable(
  token: string,
  id: number,
  accrualDate?: string,
): Promise<Commission> {
  return apiFetch<Commission>(`/commissions/${id}/make-payable`, {
    method: "POST", body: { accrual_date: accrualDate }, token,
  });
}

export async function payCommission(
  token: string,
  id: number,
  bankAccountCode: string,
  payDate?: string,
): Promise<Commission> {
  return apiFetch<Commission>(`/commissions/${id}/pay`, {
    method: "POST",
    body: { bank_account_code: bankAccountCode, pay_date: payDate },
    token,
  });
}

export async function cancelCommission(
  token: string,
  id: number,
  reason: string,
): Promise<Commission> {
  return apiFetch<Commission>(`/commissions/${id}/cancel`, {
    method: "POST", body: { reason }, token,
  });
}
