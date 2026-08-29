import { apiFetch } from "./client";

// ── W-2 — Document Numbering Engine ──────────────────────────────────────────
//
// Satu mesin penomoran untuk SELURUH dokumen (kwitansi, invoice, memo transfer,
// dan jenis berikutnya). Yang membedakan antar jenis hanya konfigurasi di
// `document_types` — bukan cabang kode. Karena itu di file ini pun tidak ada
// daftar kode dokumen yang dikenali secara khusus.

export type ResetPolicy = "yearly" | "never";

export interface DocumentType {
  id: number;
  code: string;
  name: string;
  prefix: string;
  number_format: string;
  reset_policy: ResetPolicy;
  padding: number;
  is_active: boolean;
  // Jenis yang dipakai alur inti (kwitansi/invoice/memo). Prefix & formatnya
  // tetap milik admin; yang dilarang hanya menonaktifkannya.
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface DocumentPreview {
  code: string;
  name: string;
  fiscal_year: number;
  last_val: number;
  next_number: string;
  issued: number;
}

export interface DocumentRow {
  id: number;
  document_type_code: string;
  number: string;
  fiscal_year: number;
  sequence_no: number;
  issued_at: string;
  source_table: string;
  source_id: number;
  amount: string;
  created_by?: number;
  created_at: string;
}

export interface DocumentMasterChange {
  id: number;
  entity: string;
  entity_code: string;
  field: string;
  old_value: string;
  new_value: string;
  changed_by?: number;
  created_at: string;
}

// ── Readers ──────────────────────────────────────────────────────────────────

export async function fetchDocumentTypes(token?: string): Promise<DocumentType[]> {
  const res = await apiFetch<{ types: DocumentType[] }>(`/documents/types`, { token });
  return res?.types ?? [];
}

export async function fetchDocumentPreview(token?: string): Promise<DocumentPreview[]> {
  const res = await apiFetch<{ preview: DocumentPreview[] }>(`/documents/preview`, { token });
  return res?.preview ?? [];
}

export async function fetchDocuments(
  token?: string,
  filter: { type?: string; year?: number; limit?: number } = {},
): Promise<DocumentRow[]> {
  const q = new URLSearchParams();
  if (filter.type) q.set("type", filter.type);
  if (filter.year) q.set("year", String(filter.year));
  if (filter.limit) q.set("limit", String(filter.limit));
  const qs = q.toString();
  const res = await apiFetch<{ documents: DocumentRow[] }>(
    `/documents${qs ? `?${qs}` : ""}`,
    { token },
  );
  return res?.documents ?? [];
}

export async function fetchDocumentTypeHistory(
  token?: string,
  limit = 20,
): Promise<DocumentMasterChange[]> {
  const res = await apiFetch<{ history: DocumentMasterChange[] }>(
    `/documents/types/history?limit=${limit}`,
    { token },
  );
  return res?.history ?? [];
}

// ── Writers ──────────────────────────────────────────────────────────────────

export async function createDocumentType(
  token: string | undefined,
  data: {
    code: string;
    name: string;
    prefix: string;
    number_format?: string;
    reset_policy?: ResetPolicy;
    padding?: number;
  },
): Promise<DocumentType> {
  return apiFetch(`/documents/types`, { token, method: "POST", body: data });
}

export async function updateDocumentType(
  token: string | undefined,
  typeId: number,
  data: {
    name?: string;
    prefix?: string;
    number_format?: string;
    reset_policy?: ResetPolicy;
    padding?: number;
    is_active?: boolean;
  },
): Promise<DocumentType> {
  return apiFetch(`/documents/types/${typeId}`, { token, method: "PATCH", body: data });
}

// ── Pratinjau lokal ──────────────────────────────────────────────────────────

// renderNumber MENIRU document.formatNumber di backend supaya admin melihat
// akibat setiap ketikan sebelum menyimpan. Ini semata-mata alat bantu layar —
// nomor sungguhan SELALU dibentuk server. Kalau keduanya berbeda pendapat,
// server yang benar.
export function renderNumber(
  cfg: { prefix: string; number_format: string; padding: number },
  seq: number,
  when: Date = new Date(),
): string | null {
  const format = (cfg.number_format || "").trim() || "{prefix}/{year}/{seq}";
  if (!format.includes("{seq}")) return null;
  const pad = Math.min(12, Math.max(1, cfg.padding || 6));
  const out = format
    .replaceAll("{prefix}", (cfg.prefix || "").trim())
    .replaceAll("{year}", String(when.getFullYear()).padStart(4, "0"))
    .replaceAll("{month}", String(when.getMonth() + 1).padStart(2, "0"))
    .replaceAll("{seq}", String(seq).padStart(pad, "0"));
  return out.includes("{") ? null : out;
}

// ── W-3.4/W-3.6 — audit INV-DOC-1 ────────────────────────────────────────────
//
// Kesehatan bukti kas untuk tenant berjalan. Read-only, dan sengaja TIDAK
// mengikuti filter jenis/tahun di layar register: audit yang ikut terfilter
// akan terbaca "bersih" hanya karena penggunanya sedang menyaring.

export interface CashAuditFinding {
  journal_id: number;
  document_id: number;
  number: string;
  date: string;
  source: string;
  amount: string;
  detail: string;
}

export interface CashAuditCheck {
  code: string;
  title: string;
  count: number;
  truncated: boolean;
  findings?: CashAuditFinding[];
}

export interface CashAuditReport {
  tenant_id: number;
  checked_at: string;
  cash_journals_posted: number;
  cash_journals_documented: number;
  cash_journals_exempt: number;
  violations: number;
  clean: boolean;
  checks: CashAuditCheck[];
}

export async function fetchCashDocumentAudit(
  token?: string,
  limit = 20,
): Promise<CashAuditReport> {
  return apiFetch<CashAuditReport>(`/documents/audit?limit=${limit}`, { token });
}
