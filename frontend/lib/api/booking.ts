import { apiFetch } from "./client";

// Increment 7 — Booking (lihat docs/increment-7-frontend-spec.md).

export type BookingStatus = "active" | "converted" | "expired" | "cancelled";
export type FeeDisposition =
  // Rule klien 2026-07-29: fee = Pendapatan Booking, diakui saat diterima
  // (final — batal/konversi tidak mengubahnya). Satu-satunya nilai booking baru.
  | "recognized"
  // Legacy (baris histori pra-rule):
  | "held"
  | "transferred"
  | "forfeited"
  | "pending_refund"
  | "refunded";

export interface Booking {
  id: number;
  project_id: number;
  unit_id: number;
  customer_id: number;
  sales_person_id?: number;
  booking_fee: string;
  refundable: boolean;
  booking_date: string;
  expiry_date: string;
  status: BookingStatus;
  fee_disposition: FeeDisposition;
  termin_payment_id: number;
  /** Kwitansi KWB penerimaan fee — selalu terbit bersama booking (INV-DOC-1). */
  receipt_id?: number;
  receipt_number?: string;
  converted_contract_id?: number;
  close_reason?: string;
  closed_at?: string;
  closed_event_date?: string;
  notes?: string;
  created_at: string;
  /** Produk Tambahan: Kelebihan Tanah — hadir hanya bila booking ini menyertakannya. */
  land_stock_id?: number;
  land_reservation_id?: number;
  land_quantity_m2?: string;
  land_unit_price_snapshot?: string;
}

export interface CreateBookingInput {
  customer_id: number;
  sales_person_id?: number;
  booking_fee: string; // string integer rupiah
  refundable: boolean;
  bank_account_code: string;
  booking_date?: string; // YYYY-MM-DD
  expiry_date: string;   // YYYY-MM-DD
  notes?: string;
  /**
   * Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08).
   * Salesperson mengisi HANYA kuantitas (m²) — harga & reservasi diselesaikan
   * server-side dari LandStock proyek unit ini. Kosong = booking tanpa tanah.
   */
  land_quantity_m2?: string;
}

export async function fetchBookings(
  token: string,
  status?: BookingStatus | "",
): Promise<Booking[]> {
  const qs = status ? `?status=${status}` : "";
  const res = await apiFetch<Booking[]>(`/bookings${qs}`, { token });
  return res ?? [];
}

export async function fetchBooking(token: string, id: number): Promise<Booking> {
  return apiFetch<Booking>(`/bookings/${id}`, { token });
}

export async function fetchActiveBookingByUnit(
  token: string,
  unitId: number,
): Promise<Booking | null> {
  try {
    return await apiFetch<Booking>(`/units/${unitId}/booking`, { token });
  } catch {
    return null;
  }
}

export async function createBooking(
  token: string,
  unitId: number,
  data: CreateBookingInput,
): Promise<Booking> {
  return apiFetch<Booking>(`/units/${unitId}/bookings`, {
    method: "POST",
    body: data,
    token,
  });
}

export async function cancelBooking(
  token: string,
  id: number,
  reason: string,
  eventDate?: string,
): Promise<Booking> {
  return apiFetch<Booking>(`/bookings/${id}/cancel`, {
    method: "POST",
    body: { reason, event_date: eventDate },
    token,
  });
}

export async function markExpiredBookings(
  token: string,
): Promise<{ expired: number }> {
  return apiFetch<{ expired: number }>(`/bookings/mark-expired`, {
    method: "POST",
    body: {},
    token,
  });
}
