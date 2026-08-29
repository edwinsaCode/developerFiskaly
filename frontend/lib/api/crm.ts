import { apiFetch } from "./client";
import type { Customer } from "./party";

// PS-3 — CRM ringan (Lead) + Sales Performance.

export type LeadStatus = "new" | "contacted" | "qualified" | "converted" | "lost";
export type LeadSource = "walk_in" | "referral" | "online" | "ads" | "expo" | "other";

export interface Lead {
  id: number;
  name: string;
  phone?: string;
  email?: string;
  source: LeadSource;
  status: LeadStatus;
  sales_person_id?: number;
  project_id?: number;
  customer_id?: number;
  notes?: string;
  lost_reason?: string;
  converted_at?: string;
  created_at: string;
}

export interface CreateLeadInput {
  name: string;
  phone?: string;
  email?: string;
  source?: LeadSource;
  sales_person_id?: number;
  project_id?: number;
  notes?: string;
}

export async function fetchLeads(
  token: string,
  status?: LeadStatus | "",
  salesPersonId?: number,
): Promise<Lead[]> {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  if (salesPersonId) params.set("sales_person_id", String(salesPersonId));
  const qs = params.toString() ? `?${params.toString()}` : "";
  const res = await apiFetch<Lead[]>(`/leads${qs}`, { token });
  return res ?? [];
}

export async function createLead(token: string, data: CreateLeadInput): Promise<Lead> {
  return apiFetch<Lead>(`/leads`, { method: "POST", body: data, token });
}

export async function updateLead(
  token: string,
  id: number,
  data: Partial<{
    status: LeadStatus;
    sales_person_id: number;
    project_id: number;
    notes: string;
    lost_reason: string;
  }>,
): Promise<Lead> {
  return apiFetch<Lead>(`/leads/${id}`, { method: "PUT", body: data, token });
}

export async function convertLead(
  token: string,
  id: number,
  existingCustomerId?: number,
): Promise<{ lead: Lead; customer: Customer }> {
  return apiFetch<{ lead: Lead; customer: Customer }>(`/leads/${id}/convert`, {
    method: "POST",
    body: { customer_id: existingCustomerId },
    token,
  });
}

// ── Sales performance ─────────────────────────────────────────────────────────

export interface SalesPerformanceRow {
  sales_person_id: number;
  name: string;
  is_active: boolean;
  leads: number;
  leads_converted: number;
  bookings_active: number;
  bookings_total: number;
  contracts: number;
  bast_count: number;
  contract_value: string;
  bast_value: string;
  collected: string;
  commission_earned: string;
  commission_paid: string;
}

export interface SalesPerformanceReport {
  rows: SalesPerformanceRow[] | null;
  total_leads: number;
  total_bookings: number;
  total_contracts: number;
  total_bast: number;
}

export async function fetchSalesPerformance(token: string): Promise<SalesPerformanceReport> {
  return apiFetch<SalesPerformanceReport>(`/reports/sales-performance`, { token });
}

// ── Admin Marketing performance (P2 — independen dari Sales; lihat P1) ────────

export interface AdminMarketingPerformanceRow {
  admin_marketing_person_id: number;
  name: string;
  is_active: boolean;
  contracts_handled: number;
  projects_handled: number;
  project_names: string;
  kpr_count: number;
  cash_count: number;
  bast_count: number;
  contract_value: string;
  collected: string;
}

export interface AdminMarketingPerformanceReport {
  rows: AdminMarketingPerformanceRow[] | null;
  total_contracts_assigned: number;
  total_contracts_unassigned: number;
}

export async function fetchAdminMarketingPerformance(token: string): Promise<AdminMarketingPerformanceReport> {
  return apiFetch<AdminMarketingPerformanceReport>(`/reports/admin-marketing-performance`, { token });
}

// ── Contract workload (P2b — drill-down per-kontrak untuk Sales & Admin Marketing) ──
// Satu daftar aktif per tenant; FE filter per person_id untuk expand baris
// "Kinerja Sales" / "Kinerja Admin Marketing" — menjawab "proyek apa, kontrak
// mana, sudah sampai tahap apa?" tanpa endpoint agregat baru per orang.

export interface ContractWorkloadRow {
  contract_id: number;
  unit_code: string;
  project_id: number;
  project_name: string;
  buyer_name: string;
  sales_person_id: number | null;
  admin_marketing_person_id: number | null;
  payment_type: string; // kpr|tunai
  scheme_state: string; // signed|dp_paid|submitted_to_bank|bank_approved|akad|disbursed|fully_paid|handed_over
  handed_over: boolean;
  contract_value: string;
  collected: string;
}

export interface ContractWorkloadReport {
  rows: ContractWorkloadRow[] | null;
}

export async function fetchContractWorkload(token: string): Promise<ContractWorkloadReport> {
  return apiFetch<ContractWorkloadReport>(`/reports/contract-workload`, { token });
}
