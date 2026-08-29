import { apiFetch } from "./client";
import type {
  TaxObligation,
  TaxPayment,
  TaxReport,
  VATReport,
  CombinedTaxReport,
  BASTWithoutPPhItem,
} from "@/lib/types/api";

export async function fetchTaxReport(
  token: string,
  from: string,
  to: string,
): Promise<TaxReport> {
  return apiFetch<TaxReport>(
    `/tax/report?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    { token },
  );
}

export async function fetchVATReport(
  token: string,
  from: string,
  to: string,
): Promise<VATReport> {
  return apiFetch<VATReport>(
    `/tax/vat-report?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    { token },
  );
}

export async function fetchCombinedTaxReport(
  token: string,
  from: string,
  to: string,
): Promise<CombinedTaxReport> {
  return apiFetch<CombinedTaxReport>(
    `/tax/combined-report?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    { token },
  );
}

export async function fetchBASTWithoutPPh(
  token: string,
  from: string,
  to: string,
): Promise<BASTWithoutPPhItem[]> {
  return apiFetch<BASTWithoutPPhItem[]>(
    `/tax/bast-without-pph?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    { token },
  );
}

export interface AccrueTaxInput {
  transfer_value: string;
  accrual_date?: string;
  rate_code?: string;
  project_id?: number;
}

export async function accrueTax(
  token: string,
  unitId: number,
  data: AccrueTaxInput,
): Promise<TaxObligation> {
  return apiFetch<TaxObligation>(`/units/${unitId}/tax/accrue`, {
    method: "POST",
    body: data,
    token,
  });
}

export interface PayTaxInput {
  bank_account_code: string;
  payment_date?: string;
}

export async function payTax(
  token: string,
  obligationId: number,
  data: PayTaxInput,
): Promise<TaxPayment> {
  return apiFetch<TaxPayment>(`/tax/obligations/${obligationId}/pay`, {
    method: "POST",
    body: data,
    token,
  });
}

export interface SetTaxRateInput {
  rate_code: string;
  rate: string;          // desimal string: "0.025"
  effective_from: string; // YYYY-MM-DD
  description: string;
}

export async function setTaxRate(
  token: string,
  data: SetTaxRateInput,
): Promise<void> {
  await apiFetch<void>("/tax/tax-rates", {
    method: "POST",
    body: data,
    token,
  });
}
