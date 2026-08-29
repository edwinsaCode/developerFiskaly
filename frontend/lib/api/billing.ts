import { apiFetch, apiBase, ApiError } from "./client";
import type { Invoice, InvoiceSummary, Receipt } from "@/lib/types/api";

// ── Invoices ──────────────────────────────────────────────────────────────────

// S9/R-9: backend mengirim total_unpaid (dihitung decimal) — FE display saja.
export interface AllInvoicesResponse {
  invoices: InvoiceSummary[];
  total_unpaid: string;
}

export async function listAllInvoices(token: string): Promise<AllInvoicesResponse> {
  const res = await apiFetch<AllInvoicesResponse>("/invoices", { token });
  return res ?? { invoices: [], total_unpaid: "0" };
}

export async function listInvoices(token: string, contractId: number): Promise<Invoice[]> {
  const res = await apiFetch<Invoice[]>(`/sale-contracts/${contractId}/invoices`, { token });
  return res ?? [];
}

export async function getInvoice(token: string, invoiceId: number): Promise<Invoice> {
  return apiFetch<Invoice>(`/invoices/${invoiceId}`, { token });
}

export interface GenerateInvoiceInput {
  schedule_id: number;
  issue_date?: string;  // YYYY-MM-DD; optional
  notes?: string;
}

export async function generateInvoice(
  token: string,
  contractId: number,
  data: GenerateInvoiceInput,
): Promise<Invoice> {
  return apiFetch<Invoice>(`/sale-contracts/${contractId}/invoices`, {
    token,
    method: "POST",
    body: data,
  });
}

// ── Receipts (Kwitansi) ───────────────────────────────────────────────────────

// generateReceipt membuat (atau mengembalikan) kwitansi untuk satu transaksi
// termin. Idempoten di backend — aman dipanggil berkali-kali.
export async function generateReceipt(
  token: string,
  terminId: number,
  notes?: string,
): Promise<Receipt> {
  return apiFetch<Receipt>(`/termins/${terminId}/receipt`, {
    token,
    method: "POST",
    body: { notes: notes ?? "" },
  });
}

// fetchReceiptByTermin membaca kwitansi yang SUDAH terbit untuk satu termin —
// read-only, tanpa efek samping (berbeda dari generateReceipt yang POST).
// null = belum ada kwitansi; dipakai layar yang perlu MENAMPILKAN nomor
// dokumen tanpa menerbitkan apa pun.
export async function fetchReceiptByTermin(
  token: string,
  terminId: number,
): Promise<Receipt | null> {
  try {
    return await apiFetch<Receipt>(`/termins/${terminId}/receipt`, { token });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

// fetchReceiptPrintHTML fetches the printable A4 receipt HTML.
export async function fetchReceiptPrintHTML(token: string, receiptId: number): Promise<string> {
  const res = await fetch(`${apiBase()}/receipts/${receiptId}/print`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`Gagal mengambil kwitansi (${res.status})`);
  }
  return res.text();
}

// fetchInvoicePrintHTML fetches the printable HTML for an invoice.
// Returns raw HTML string so the caller can open it as a blob URL.
export async function fetchInvoicePrintHTML(token: string, invoiceId: number): Promise<string> {
  const res = await fetch(`${apiBase()}/invoices/${invoiceId}/print`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw new Error(`Gagal mengambil invoice print (${res.status})`);
  }
  return res.text();
}

// R1: invoice kekurangan pembayaran (pasca pencairan bank) — nominal SELALU
// dihitung backend dari ContractFinancialSummary (satu rumus).
export async function createShortfallInvoice(
  token: string,
  contractId: number,
  data: { due_date?: string; notes?: string } = {},
): Promise<{ id: number; invoice_number: string; amount: string }> {
  return apiFetch(`/sale-contracts/${contractId}/invoices/shortfall`, {
    method: "POST",
    body: data,
    token,
  });
}
