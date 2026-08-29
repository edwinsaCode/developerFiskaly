import { apiFetch } from "./client";
import type { Role } from "@/lib/types/api";

// Master pihak (Increment 1): customer + sales person — dipakai Booking,
// Kontrak (wajib sejak Increment 3), dan Komisi.

export interface Customer {
  id: number;
  code: string;
  name: string;
  type?: "individual" | "company";
  id_number?: string;
  npwp?: string;
  phone?: string;
  email?: string;
  address?: string;
  is_active: boolean;
}

export interface SalesPerson {
  id: number;
  code: string;
  name: string;
  sales_team_id?: number;
  phone?: string;
  email?: string;
  is_active: boolean;
}

export interface SchemeParams {
  dp_percent?: string;
  installment_count?: number;
  tenor_months?: number;
  final_due_months?: number;
  bast_gate?: string; // full_payment | akad | dp_paid
  receivable_account?: string;
  financing_receivable_account?: string;
}

export interface PaymentScheme {
  id: number;
  code?: string;
  name: string;
  policy_type: string; // cash | cash_installment | kpr | inhouse
  params?: string | SchemeParams;
  is_active: boolean;
}

export interface FinancingSource {
  id: number;
  code?: string;
  name: string;
  type?: string; // bank_kpr_subsidi | bank_kpr_komersial | lainnya
  is_active: boolean;
}

export interface CreateCustomerInput {
  code: string;
  name: string;
  type?: "individual" | "company";
  id_number?: string;
  npwp?: string;
  phone?: string;
  email?: string;
  address?: string;
}

export async function fetchCustomers(token: string): Promise<Customer[]> {
  const res = await apiFetch<Customer[]>(`/customers`, { token });
  return res ?? [];
}

export async function createCustomer(
  token: string,
  data: CreateCustomerInput,
): Promise<Customer> {
  return apiFetch<Customer>(`/customers`, { method: "POST", body: data, token });
}

export async function fetchSalesPersons(token: string): Promise<SalesPerson[]> {
  const res = await apiFetch<SalesPerson[]>(`/sales-persons`, { token });
  return res ?? [];
}

export async function fetchPaymentSchemes(token: string): Promise<PaymentScheme[]> {
  const res = await apiFetch<PaymentScheme[]>(`/payment-schemes`, { token });
  return res ?? [];
}

export async function fetchFinancingSources(token: string): Promise<FinancingSource[]> {
  const res = await apiFetch<FinancingSource[]>(`/financing-sources`, { token });
  return res ?? [];
}

// ── PRODUCT HARDENING — master data management (Pengaturan) ───────────────────

export interface AppUser {
  id: number;
  email: string;
  // W-12: kosong untuk user yang dibuat sebelum migrasi 000074. Jangan
  // dikarang — tampilkan email sebagai gantinya.
  name?: string;
  role: Role;
  created_at: string;
}

export async function fetchUsers(token: string): Promise<AppUser[]> {
  const res = await apiFetch<AppUser[]>(`/users`, { token });
  return res ?? [];
}

export async function createUser(
  token: string,
  data: { email: string; password: string; role: string; name?: string },
): Promise<AppUser> {
  return apiFetch<AppUser>(`/users`, { method: "POST", body: data, token });
}

// PATCH /users/{id} — semua field opsional; yang tidak dikirim tidak diubah.
export async function updateUser(
  token: string,
  id: number,
  data: { email?: string; name?: string; password?: string; role?: string },
): Promise<AppUser> {
  return apiFetch<AppUser>(`/users/${id}`, { method: "PATCH", body: data, token });
}

export interface SalesTeam {
  id: number;
  code: string;
  name: string;
  leader_sales_person_id?: number;
}

export async function fetchSalesTeams(token: string): Promise<SalesTeam[]> {
  const res = await apiFetch<SalesTeam[]>(`/sales-teams`, { token });
  return res ?? [];
}

export async function createSalesTeam(
  token: string,
  data: { code: string; name: string },
): Promise<SalesTeam> {
  return apiFetch<SalesTeam>(`/sales-teams`, { method: "POST", body: data, token });
}

export async function createPaymentScheme(
  token: string,
  data: { code: string; name: string; policy_type: string; params?: SchemeParams },
): Promise<PaymentScheme> {
  return apiFetch<PaymentScheme>(`/payment-schemes`, { method: "POST", body: data, token });
}

export async function createSalesPerson(
  token: string,
  data: { code: string; name: string; sales_team_id?: number; phone?: string; email?: string },
): Promise<SalesPerson> {
  return apiFetch<SalesPerson>(`/sales-persons`, { method: "POST", body: data, token });
}

export async function updateSalesPerson(
  token: string,
  id: number,
  data: { name?: string; sales_team_id?: number; phone?: string; email?: string; is_active?: boolean },
): Promise<SalesPerson> {
  return apiFetch<SalesPerson>(`/sales-persons/${id}`, { method: "PUT", body: data, token });
}

export async function createFinancingSource(
  token: string,
  data: { code: string; name: string; type?: string },
): Promise<FinancingSource> {
  return apiFetch<FinancingSource>(`/financing-sources`, { method: "POST", body: data, token });
}

export async function updateFinancingSource(
  token: string,
  id: number,
  data: { name?: string; is_active?: boolean },
): Promise<FinancingSource> {
  return apiFetch<FinancingSource>(`/financing-sources/${id}`, { method: "PUT", body: data, token });
}

export async function updatePaymentScheme(
  token: string,
  id: number,
  data: { is_active?: boolean; name?: string },
): Promise<PaymentScheme> {
  return apiFetch<PaymentScheme>(`/payment-schemes/${id}`, { method: "PUT", body: data, token });
}

export async function updateCustomer(
  token: string,
  id: number,
  data: { name?: string; phone?: string; email?: string; address?: string; npwp?: string; is_active?: boolean },
): Promise<Customer> {
  return apiFetch<Customer>(`/customers/${id}`, { method: "PUT", body: data, token });
}
