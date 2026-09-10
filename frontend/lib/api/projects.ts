import { apiFetch } from "./client";
import type { Project, ProjectPhase, Unit } from "@/lib/types/api";

export async function fetchProjects(token: string): Promise<Project[]> {
  return apiFetch<Project[]>("/projects", { token });
}

export interface CreateProjectInput {
  name: string;
  land_area?: string;  // m², string angka
  notes?: string;
  start_date?: string; // YYYY-MM-DD
}

export async function createProject(token: string, data: CreateProjectInput): Promise<Project> {
  return apiFetch<Project>("/projects", { token, method: "POST", body: data });
}

export interface CreateUnitInput {
  code: string;
  unit_type: string;      // kode product type (master Product Catalog)
  type_label?: string;    // label komersial, mis. "36/72"
  saleable_area?: string; // m²
  land_area?: string;     // m² — opsional saat create, bisa diisi belakangan
  list_price?: string;    // string angka rupiah
  phase_id?: number;
}

export async function createUnit(token: string, projectId: number, data: CreateUnitInput): Promise<Unit> {
  return apiFetch<Unit>(`/projects/${projectId}/units`, { token, method: "POST", body: data });
}

// LT-2 (kelebihan-tanah-final-architecture §B.5): koreksi eksplisit-admin luas
// tanah unit, terpisah dari lifecycle transition — bisa dipanggil kapan saja.
export async function updateUnitLandArea(token: string, unitId: number, landArea: string): Promise<Unit> {
  return apiFetch<Unit>(`/units/${unitId}/land-area`, { token, method: "PATCH", body: { land_area: landArea } });
}

// ── UAT Batch 2 §2 — Product Catalog ─────────────────────────────────────────

export interface ProductType {
  id: number;
  code: string;
  name: string;
  category: "property" | "land" | "non_property";
  revenue_account_code: string;
  is_active: boolean;
  // Rule klien UAT #3: subsidi|komersial, opsional. Kosong/absen = produk ini
  // ikut projects.tax_category (jalur legacy) — lihat backend product_type.go.
  tax_category?: "subsidi" | "komersial" | null;
}

// W-13 — satu produk, satu jalan jual.
//
// Produk PROPERTI (rumah, ruko) dijual sebagai UNIT: unitlah yang menerima
// alokasi biaya proyek dan melahirkan HPP. Produk NON-PROPERTI (PDAM, dll)
// dijual sebagai PRODUK TAMBAHAN yang menempel pada penjualan unit.
//
// LT-8 (kelebihan-tanah-final-architecture §G): produk `land` (Kelebihan Tanah)
// TIDAK LAGI dijual lewat produk tambahan — jalur itu dibekukan, dijual lewat
// halaman Kelebihan Tanah tersendiri (land_stock/land_sales). Backend menolak
// ketiganya secara fail-closed; filter di sini hanya supaya pengguna tidak
// dihadapkan pada pilihan yang pasti gagal.
export function isUnitProduct(p: ProductType): boolean {
  return p.is_active && p.category === "property";
}

export function isAddonProduct(p: ProductType): boolean {
  return p.is_active && p.category === "non_property";
}

export async function fetchProductTypes(token: string): Promise<ProductType[]> {
  const res = await apiFetch<ProductType[]>("/product-types", { token });
  return res ?? [];
}

export async function createProductType(
  token: string,
  data: { code: string; name: string; category: string; revenue_account_code?: string; tax_category?: string },
): Promise<ProductType> {
  return apiFetch<ProductType>("/product-types", { token, method: "POST", body: data });
}

export async function updateProductType(
  token: string,
  id: number,
  // tax_category: "" mengosongkan (kembali ke jalur legacy projects.tax_category).
  data: { name?: string; revenue_account_code?: string; is_active?: boolean; tax_category?: string },
): Promise<ProductType> {
  return apiFetch<ProductType>(`/product-types/${id}`, { token, method: "PATCH", body: data });
}

// ── UAT Batch 2 §4 — Bulk Unit Generator ─────────────────────────────────────

export interface BulkCreateUnitsInput {
  block: string;
  unit_start: number;
  unit_end: number;
  unit_type: string;
  type_label?: string;
  saleable_area?: string;
  land_area?: string; // m² — diterapkan sama ke seluruh unit dalam blok
  list_price: string;
  phase_id?: number;
}

export async function bulkCreateUnits(
  token: string,
  projectId: number,
  data: BulkCreateUnitsInput,
): Promise<{ created: number; units: Unit[] }> {
  return apiFetch(`/projects/${projectId}/units/bulk`, { token, method: "POST", body: data });
}

export async function fetchProject(token: string, id: number): Promise<Project> {
  return apiFetch<Project>(`/projects/${id}`, { token });
}

export async function fetchPhases(token: string, projectId: number): Promise<ProjectPhase[]> {
  return apiFetch<ProjectPhase[]>(`/projects/${projectId}/phases`, { token });
}

export async function fetchUnitsByProject(token: string, projectId: number): Promise<Unit[]> {
  return apiFetch<Unit[]>(`/projects/${projectId}/units`, { token });
}

export async function fetchUnit(token: string, unitId: number): Promise<Unit> {
  return apiFetch<Unit>(`/units/${unitId}`, { token });
}

// ── Increment 6 — Unit Lifecycle ──────────────────────────────────────────────

export interface UnitTransition {
  id: number;
  unit_id: number;
  from_status: string;
  to_status: string;
  event: string;
  event_date: string;
  reference_type: string;
  reference_id?: number;
  actor_id?: number;
  notes?: string;
  created_at: string;
}

export async function fetchUnitTransitions(
  token: string,
  unitId: number,
): Promise<UnitTransition[]> {
  const res = await apiFetch<UnitTransition[]>(`/units/${unitId}/transitions`, { token });
  return res ?? [];
}

export interface TransitionUnitInput {
  status: string;
  event?: string;
  event_date?: string; // YYYY-MM-DD
  notes?: string;
  buyer_ref?: string;
}

export async function transitionUnit(
  token: string,
  unitId: number,
  data: TransitionUnitInput,
): Promise<Unit> {
  return apiFetch<Unit>(`/units/${unitId}/transition`, {
    method: "POST",
    body: data,
    token,
  });
}

// PS-2 — Timeline workspace: transisi terbaru lintas unit satu proyek.
export async function fetchProjectTransitions(
  token: string,
  projectId: number,
  limit = 50,
): Promise<UnitTransition[]> {
  const res = await apiFetch<UnitTransition[]>(
    `/projects/${projectId}/transitions?limit=${limit}`,
    { token },
  );
  return res ?? [];
}

// ── R3 — Progress FISIK (append-only, non-ledger) ─────────────────────────────

export interface ProjectProgressEntry {
  id: number;
  project_id: number;
  phase_id?: number;
  progress_pct: string;
  as_of_date: string;
  notes?: string;
  created_by?: number;
  created_at: string;
}

export interface AddProgressInput {
  phase_id?: number;
  progress_pct: string;
  as_of_date?: string; // YYYY-MM-DD
  notes?: string;
}

export async function fetchProjectProgress(
  token: string,
  projectId: number,
): Promise<ProjectProgressEntry[]> {
  const res = await apiFetch<ProjectProgressEntry[] | null>(`/projects/${projectId}/progress`, { token });
  return res ?? [];
}

export async function addProjectProgress(
  token: string,
  projectId: number,
  data: AddProgressInput,
): Promise<ProjectProgressEntry> {
  return apiFetch(`/projects/${projectId}/progress`, { method: "POST", body: data, token });
}
