import { apiFetch, ApiError } from "./client";

// LT-3 (kelebihan-tanah-final-architecture §B.1) — Kelebihan Tanah: pool
// inventory per proyek. reserved/sold di sini murni counter (0 sampai LT-4/
// LT-5 hidup) — hanya total_quantity_m2 dan unit_price yang bisa diedit admin.

export interface LandStock {
  id: number;
  project_id: number;
  product_code: string;
  total_quantity_m2: string;
  reserved_quantity_m2: string;
  sold_quantity_m2: string;
  unit_price: string;
  created_at: string;
  updated_at: string;
}

export async function fetchLandStock(token: string, projectId: number): Promise<LandStock | null> {
  try {
    return await apiFetch<LandStock>(`/projects/${projectId}/land-stock`, { token });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

export async function createLandStock(
  token: string,
  projectId: number,
  input: { total_quantity_m2: string; unit_price: string },
): Promise<LandStock> {
  return apiFetch<LandStock>(`/projects/${projectId}/land-stock`, {
    token,
    method: "POST",
    body: input,
  });
}

export async function updateLandStock(
  token: string,
  projectId: number,
  input: { total_quantity_m2: string; unit_price: string },
): Promise<LandStock> {
  return apiFetch<LandStock>(`/projects/${projectId}/land-stock`, {
    token,
    method: "PATCH",
    body: input,
  });
}

// LT-4 (§B.2, §D) — reservasi: soft-lock kuantitas untuk customer. Keputusan
// klien 2026-08-20: TANPA booking fee — tidak ada transaksi finansial/jurnal.

export type ReservationStatus = "active" | "converted" | "expired" | "cancelled";

export interface LandStockReservation {
  id: number;
  land_stock_id: number;
  project_id: number;
  customer_id: number;
  sales_person_id?: number;
  quantity_m2: string;
  unit_price_snapshot: string;
  reserved_at: string;
  expiry_date?: string;
  status: ReservationStatus;
  converted_sale_id?: number;
  cancelled_reason?: string;
  created_at: string;
  updated_at: string;
}

export async function fetchReservations(token: string, projectId: number): Promise<LandStockReservation[]> {
  return apiFetch<LandStockReservation[]>(`/projects/${projectId}/land-stock/reservations`, { token });
}

export async function createReservation(
  token: string,
  projectId: number,
  input: {
    customer_id: number;
    sales_person_id?: number;
    quantity_m2: string;
    reserved_at: string;
    expiry_date?: string;
  },
): Promise<LandStockReservation> {
  return apiFetch<LandStockReservation>(`/projects/${projectId}/land-stock/reservations`, {
    token,
    method: "POST",
    body: input,
  });
}

export async function cancelReservation(
  token: string,
  projectId: number,
  reservationId: number,
  reason: string,
): Promise<LandStockReservation> {
  return apiFetch<LandStockReservation>(
    `/projects/${projectId}/land-stock/reservations/${reservationId}/cancel`,
    { token, method: "POST", body: { reason } },
  );
}

// LT-5 (§E, §F) — Akad: pengakuan pendapatan + HPP Kelebihan Tanah. Skema
// tunai/lunas saja di v1 — tidak ada termin_payments/ReceivePayment. PPN
// eksplisit opsional (is_pkp), tarif di-snapshot saat akad.

export type LandSaleStatus = "draft" | "akad" | "cancelled";

export interface LandSale {
  id: number;
  project_id: number;
  land_stock_id: number;
  reservation_id?: number;
  customer_id: number;
  sales_person_id?: number;
  quantity_m2: string;
  unit_price_snapshot: string;
  dpp_amount: string;
  is_pkp: boolean;
  vat_rate_snapshot: string;
  gross_amount: string;
  payment_account_code: string;
  recognition_date?: string;
  status: LandSaleStatus;
  cancelled_at?: string;
  cancel_reason?: string;
  cancelled_by?: number;
  revenue_journal_id?: number;
  cogs_journal_id?: number;
  revenue_reversal_journal_id?: number;
  cogs_reversal_journal_id?: number;
  created_by?: number;
  created_at: string;
  updated_at: string;
}

export async function recordAkad(
  token: string,
  projectId: number,
  input: {
    reservation_id?: number;
    customer_id: number;
    sales_person_id?: number;
    quantity_m2: string;
    unit_price_snapshot?: string;
    dpp_amount: string;
    is_pkp: boolean;
    vat_rate_snapshot?: string;
    payment_account_code: string;
    recognition_date: string;
  },
): Promise<LandSale> {
  return apiFetch<LandSale>(`/projects/${projectId}/land-stock/akad`, {
    token,
    method: "POST",
    body: input,
  });
}

export async function fetchLandSales(token: string, projectId: number): Promise<LandSale[]> {
  return apiFetch<LandSale[]>(`/projects/${projectId}/land-stock/land-sales`, { token });
}

export async function fetchLandSale(token: string, projectId: number, saleId: number): Promise<LandSale | null> {
  try {
    return await apiFetch<LandSale>(`/projects/${projectId}/land-stock/land-sales/${saleId}`, { token });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

// LT-6 (§F.3) — pembatalan pasca-Akad: membalik jurnal pendapatan+HPP,
// mengembalikan kuantitas ke AVAILABLE, land_sale -> cancelled.

export async function cancelLandSale(
  token: string,
  projectId: number,
  saleId: number,
  reason: string,
): Promise<LandSale> {
  return apiFetch<LandSale>(`/projects/${projectId}/land-stock/land-sales/${saleId}/cancel`, {
    token,
    method: "POST",
    body: { reason },
  });
}
