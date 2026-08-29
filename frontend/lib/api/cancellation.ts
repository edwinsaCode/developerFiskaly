import { apiFetch } from "./client";

// Increment 8 — Cancellation & Refund (lihat docs/increment-8-frontend-spec.md).

export type CancellationStatus = "requested" | "approved" | "rejected" | "processed";
export type CancellationStage = "pre_bast" | "post_bast";
export type RefundStatus = "pending" | "paid" | "cancelled";

export interface Cancellation {
  id: number;
  project_id: number;
  unit_id: number;
  sale_contract_id?: number;
  sale_record_id?: number;
  stage: CancellationStage;
  reason: string;
  event_date: string;
  penalty: string;
  received_total: string;
  refund_amount: string;
  status: CancellationStatus;
  reject_reason?: string;
  approved_at?: string;
  processed_at?: string;
  revenue_reversal_journal_id?: number;
  cogs_reversal_journal_id?: number;
  tax_reversal_journal_id?: number;
  settlement_journal_id?: number;
  refund_id?: number;
  created_at: string;
}

export interface PreviewLine {
  account_code: string;
  account_name: string;
  debit: string;
  credit: string;
}

export interface PreviewJournal {
  purpose: string;
  label: string;
  lines: PreviewLine[];
}

export interface ProcessPreview {
  cancellation_id: number;
  stage: CancellationStage;
  received_total: string;
  penalty: string;
  refund_amount: string;
  journals: PreviewJournal[];
  revenue_reversed: string;
  cogs_reversed: string;
  tax_reversed: string;
  unit_next_status: string;
}

export interface Refund {
  id: number;
  source_type: "cancellation" | "booking";
  cancellation_id?: number;
  booking_id?: number;
  unit_id: number;
  payee: string;
  amount: string;
  status: RefundStatus;
  payment_journal_id?: number;
  bank_account_code?: string;
  paid_at?: string;
  created_at: string;
}

export async function requestCancellation(
  token: string,
  unitId: number,
  data: { reason: string; penalty?: string; event_date?: string },
): Promise<Cancellation> {
  return apiFetch<Cancellation>(`/units/${unitId}/cancellations`, {
    method: "POST",
    body: data,
    token,
  });
}

export async function fetchCancellations(
  token: string,
  status?: CancellationStatus | "",
): Promise<Cancellation[]> {
  const qs = status ? `?status=${status}` : "";
  const res = await apiFetch<Cancellation[]>(`/cancellations${qs}`, { token });
  return res ?? [];
}

export async function fetchCancellation(token: string, id: number): Promise<Cancellation> {
  return apiFetch<Cancellation>(`/cancellations/${id}`, { token });
}

export async function fetchCancellationPreview(
  token: string,
  id: number,
): Promise<ProcessPreview> {
  return apiFetch<ProcessPreview>(`/cancellations/${id}/preview`, { token });
}

export async function approveCancellation(token: string, id: number): Promise<Cancellation> {
  return apiFetch<Cancellation>(`/cancellations/${id}/approve`, {
    method: "POST", body: {}, token,
  });
}

export async function rejectCancellation(
  token: string,
  id: number,
  reason: string,
): Promise<Cancellation> {
  return apiFetch<Cancellation>(`/cancellations/${id}/reject`, {
    method: "POST", body: { reason }, token,
  });
}

export async function processCancellation(token: string, id: number): Promise<Cancellation> {
  return apiFetch<Cancellation>(`/cancellations/${id}/process`, {
    method: "POST", body: {}, token,
  });
}

// ── Refunds ───────────────────────────────────────────────────────────────────

export async function fetchRefunds(
  token: string,
  status?: RefundStatus | "",
): Promise<Refund[]> {
  const qs = status ? `?status=${status}` : "";
  const res = await apiFetch<Refund[]>(`/refunds${qs}`, { token });
  return res ?? [];
}

export async function payRefund(
  token: string,
  id: number,
  bankAccountCode: string,
  payDate?: string,
): Promise<Refund> {
  return apiFetch<Refund>(`/refunds/${id}/pay`, {
    method: "POST",
    body: { bank_account_code: bankAccountCode, pay_date: payDate },
    token,
  });
}

export async function createBookingRefund(token: string, bookingId: number): Promise<Refund> {
  return apiFetch<Refund>(`/refunds/from-booking/${bookingId}`, {
    method: "POST", body: {}, token,
  });
}
