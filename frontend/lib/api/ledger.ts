import { apiFetch } from "./client";
import type { Account, JournalEntry, JournalSummary, AccountingPeriod, ClosingPreview, ClosingResult } from "@/lib/types/api";

// Ledger routes are mounted at /api/v1/ledger/* in the backend router.

// ── Accounts ──────────────────────────────────────────────────────────────────

// fetchCashBankAccounts mengambil akun tujuan pembayaran (aset aktif, kategori
// cash/bank) — sumber data dropdown Bank/Cash (COA-driven, bukan hardcode).
export async function fetchCashBankAccounts(token: string): Promise<Account[]> {
  const res = await apiFetch<Account[]>("/ledger/accounts/cash-bank", { token });
  return res ?? [];
}

export async function fetchAccounts(token: string): Promise<Account[]> {
  const res = await apiFetch<Account[]>("/ledger/accounts", { token });
  return res ?? [];
}

export async function fetchAccount(token: string, id: number): Promise<Account> {
  return apiFetch<Account>(`/ledger/accounts/${id}`, { token });
}

export async function createAccount(
  token: string,
  data: { code: string; name: string; type: string; description?: string },
): Promise<Account> {
  return apiFetch<Account>("/ledger/accounts", { token, method: "POST", body: data });
}

export async function updateAccount(
  token: string,
  id: number,
  data: { name: string; description?: string; is_active: boolean },
): Promise<Account> {
  return apiFetch<Account>(`/ledger/accounts/${id}`, { token, method: "PUT", body: data });
}

// ── Journals ──────────────────────────────────────────────────────────────────

export interface JournalListFilter {
  date_from?: string;
  date_to?: string;
  source?: string;
}

export async function fetchJournals(token: string, filter?: JournalListFilter): Promise<JournalSummary[]> {
  const params = new URLSearchParams();
  if (filter?.date_from) params.set("date_from", filter.date_from);
  if (filter?.date_to) params.set("date_to", filter.date_to);
  if (filter?.source) params.set("source", filter.source);
  const qs = params.toString();
  const res = await apiFetch<JournalSummary[]>(`/ledger/journals${qs ? `?${qs}` : ""}`, { token });
  return res ?? [];
}

export async function fetchJournal(token: string, id: number): Promise<JournalEntry> {
  return apiFetch<JournalEntry>(`/ledger/journals/${id}`, { token });
}

export interface CreateJournalLineInput {
  account_id: number;
  debit: string;
  credit: string;
  project_id?: number;
  description?: string;
}

export interface CreateJournalInput {
  date: string;
  description: string;
  reference?: string;
  source?: string; // "manual" (default) | "opening_balance"
  lines: CreateJournalLineInput[];
}

export async function createJournal(token: string, data: CreateJournalInput): Promise<JournalEntry> {
  return apiFetch<JournalEntry>("/ledger/journals", { token, method: "POST", body: data });
}

// ── W-3.6 — bukti kas saat posting (INV-DOC-1) ───────────────────────────────

// JournalDocumentChoices menjawab satu pertanyaan sebelum posting dimulai:
// "bukti apa yang akan terbit dari jurnal ini?". Tanpa ini frontend harus
// menebak aturan kas, dan tebakan yang meleset baru ketahuan sebagai error 400
// di tengah alur.
export interface JournalDocumentChoices {
  touches_cash: boolean;
  required: boolean; // true = ada lebih dari satu jenis yang sah → wajib dipilih
  default: string;
  choices: string[];
}

export async function fetchJournalDocumentChoices(
  token: string,
  id: number,
): Promise<JournalDocumentChoices> {
  return apiFetch<JournalDocumentChoices>(`/ledger/journals/${id}/document-choices`, { token });
}

// postJournal memposting draft. `documentType` HANYA bermakna untuk jurnal yang
// menyentuh kas; server yang memutuskan wajib atau tidak, bukan layar.
export async function postJournal(
  token: string,
  id: number,
  documentType?: string,
): Promise<JournalEntry> {
  return apiFetch<JournalEntry>(`/ledger/journals/${id}/post`, {
    token,
    method: "POST",
    body: documentType ? { document_type: documentType } : {},
  });
}

export async function deleteJournal(token: string, id: number): Promise<void> {
  return apiFetch<void>(`/ledger/journals/${id}`, { token, method: "DELETE" });
}

export async function reverseJournal(token: string, id: number, date: string): Promise<JournalEntry> {
  return apiFetch<JournalEntry>(`/ledger/journals/${id}/reverse`, { token, method: "POST", body: { date } });
}

// ── Periods ───────────────────────────────────────────────────────────────────

export async function fetchPeriods(token: string): Promise<AccountingPeriod[]> {
  const res = await apiFetch<AccountingPeriod[]>("/ledger/periods", { token });
  return res ?? [];
}

export async function closePeriod(token: string, year: number, month: number): Promise<AccountingPeriod> {
  return apiFetch<AccountingPeriod>(`/ledger/periods/${year}/${month}/close`, { token, method: "POST", body: {} });
}

export async function reopenPeriod(token: string, year: number, month: number): Promise<AccountingPeriod> {
  return apiFetch<AccountingPeriod>(`/ledger/periods/${year}/${month}/reopen`, { token, method: "POST", body: {} });
}

// ── Tutup Buku Tahunan (Year-End Closing) ─────────────────────────────────────

export async function fetchYearClosePreview(token: string, year: number): Promise<ClosingPreview> {
  return apiFetch<ClosingPreview>(`/ledger/fiscal-years/${year}/closing`, { token });
}

export async function closeFiscalYear(token: string, year: number): Promise<ClosingResult> {
  return apiFetch<ClosingResult>(`/ledger/fiscal-years/${year}/close`, { token, method: "POST", body: {} });
}
