import { apiFetch } from "./client";

// ── W-6 — Snapshot Keuangan Historis ─────────────────────────────────────────
//
// Laporan tahun-tahun SEBELUM sistem ini dipakai. Data ini hidup di luar buku
// besar: tidak ada jurnal, tidak masuk Neraca/Laba Rugi berjalan, dan tidak
// dibaca perhitungan mana pun. Layarnya pun terpisah supaya tidak ada yang
// mengira angka historis ikut menyusun laporan tahun berjalan.
//
// Satu tahun = satu snapshot yang berdiri sendiri. 2024 tidak pernah diturunkan
// dari 2025.

export type SnapshotStatus = "draft" | "final";
export type SnapshotStatement = "balance_sheet" | "income_statement";

export interface SnapshotLine {
  id: number;
  snapshot_id: number;
  statement: SnapshotStatement;
  account_id: number;
  // Kode/nama/tipe adalah SALINAN master saat baris disimpan — laporan tahun
  // lalu tidak berubah kalau COA diedit sekarang.
  account_code: string;
  account_name: string;
  account_type: "asset" | "liability" | "equity" | "revenue" | "expense";
  amount: string;
  sort_order: number;
}

export interface SnapshotTotals {
  total_aset: string;
  total_kewajiban: string;
  total_ekuitas: string;
  total_pendapatan: string;
  total_beban: string;
  net_income: string;
  total_kewajiban_ekuitas: string;
  difference: string;
  is_balanced: boolean;
  line_count: number;
}

export interface Snapshot {
  id: number;
  fiscal_year: number;
  status: SnapshotStatus;
  revision: number;
  notes?: string;
  finalized_at?: string;
  finalized_by?: number;
  created_at: string;
  updated_at: string;
}

export interface SnapshotSummary extends Snapshot {
  totals: SnapshotTotals;
}

export interface SnapshotDetail extends Snapshot {
  lines: SnapshotLine[];
  totals: SnapshotTotals;
  // Akun ikhtisar laba rugi: dihitung server, jadi disembunyikan dari pemilih
  // akun. Datang dari API supaya kodenya tidak dihardcode di browser.
  computed_account_code: string;
}

export interface HistLine {
  code: string;
  name: string;
  amount: string;
}

export interface HistNeraca {
  fiscal_year: number;
  status: SnapshotStatus;
  revision: number;
  historical: boolean;
  aset: HistLine[];
  kewajiban: HistLine[];
  ekuitas: HistLine[];
  total_aset: string;
  total_kewajiban: string;
  total_ekuitas: string;
  laba_rugi_tahun_berjalan: string;
  total_ekuitas_efektif: string;
  total_kewajiban_ekuitas: string;
  difference: string;
  is_balanced: boolean;
  notes?: string;
}

export interface HistLabaRugi {
  fiscal_year: number;
  status: SnapshotStatus;
  revision: number;
  historical: boolean;
  pendapatan: HistLine[];
  beban: HistLine[];
  total_pendapatan: string;
  total_beban: string;
  laba_rugi_bersih: string;
  notes?: string;
}

export interface SnapshotAudit {
  id: number;
  snapshot_id: number;
  event: "created" | "saved" | "finalized" | "reopened";
  revision: number;
  reason?: string;
  total_assets: string;
  total_liab_equity: string;
  net_income: string;
  line_count: number;
  actor_id?: number;
  created_at: string;
}

const BASE = "/historical-financials";

// ── Readers ──────────────────────────────────────────────────────────────────

export async function fetchSnapshotYears(token?: string): Promise<SnapshotSummary[]> {
  const res = await apiFetch<SnapshotSummary[] | null>(`${BASE}/snapshots`, { token });
  return res ?? [];
}

export async function fetchSnapshot(
  token: string | undefined,
  year: number,
): Promise<SnapshotDetail> {
  return apiFetch(`${BASE}/snapshots/${year}`, { token });
}

export async function fetchHistNeraca(
  token: string | undefined,
  year: number,
): Promise<HistNeraca> {
  return apiFetch(`${BASE}/snapshots/${year}/neraca`, { token });
}

export async function fetchHistLabaRugi(
  token: string | undefined,
  year: number,
): Promise<HistLabaRugi> {
  return apiFetch(`${BASE}/snapshots/${year}/laba-rugi`, { token });
}

export async function fetchSnapshotAudits(
  token: string | undefined,
  year: number,
): Promise<SnapshotAudit[]> {
  const res = await apiFetch<SnapshotAudit[] | null>(`${BASE}/snapshots/${year}/audits`, { token });
  return res ?? [];
}

// ── Writers ──────────────────────────────────────────────────────────────────

export async function createSnapshot(
  token: string | undefined,
  fiscalYear: number,
  notes?: string,
): Promise<SnapshotDetail> {
  return apiFetch(`${BASE}/snapshots`, {
    token,
    method: "POST",
    body: { fiscal_year: fiscalYear, notes },
  });
}

// saveSnapshot mengganti SELURUH isi tahun tersebut. Baris yang tidak dikirim
// berarti dihapus — layarnya memang selalu mengirim keadaan penuh.
export async function saveSnapshot(
  token: string | undefined,
  year: number,
  data: {
    notes?: string;
    lines: { account_id: number; amount: string; sort_order?: number }[];
  },
): Promise<SnapshotDetail> {
  return apiFetch(`${BASE}/snapshots/${year}`, { token, method: "PUT", body: data });
}

export async function finalizeSnapshot(
  token: string | undefined,
  year: number,
): Promise<SnapshotDetail> {
  return apiFetch(`${BASE}/snapshots/${year}/finalize`, { token, method: "POST" });
}

// reopenSnapshot hanya untuk owner. Alasan wajib — itulah satu-satunya jejak
// mengapa angka yang pernah disahkan berubah.
export async function reopenSnapshot(
  token: string | undefined,
  year: number,
  reason: string,
): Promise<SnapshotDetail> {
  return apiFetch(`${BASE}/snapshots/${year}/reopen`, {
    token,
    method: "POST",
    body: { reason },
  });
}

export async function deleteSnapshot(
  token: string | undefined,
  year: number,
): Promise<void> {
  await apiFetch(`${BASE}/snapshots/${year}`, { token, method: "DELETE" });
}
