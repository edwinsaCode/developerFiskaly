import { Badge } from "@/components/ui/Badge";
import type { BookingStatus, FeeDisposition } from "@/lib/api/booking";

export function BookingStatusBadge({ status, expired }: { status: BookingStatus; expired?: boolean }) {
  if (status === "active" && expired) {
    return <Badge variant="danger">Lewat Masa Berlaku</Badge>;
  }
  const map: Record<BookingStatus, { v: "accent" | "success" | "neutral" | "danger"; label: string }> = {
    active:    { v: "accent",  label: "Aktif" },
    converted: { v: "success", label: "Terkonversi" },
    expired:   { v: "neutral", label: "Kedaluwarsa" },
    cancelled: { v: "danger",  label: "Dibatalkan" },
  };
  const m = map[status];
  return <Badge variant={m.v}>{m.label}</Badge>;
}

export function FeeDispositionBadge({ d }: { d: FeeDisposition }) {
  const map: Record<FeeDisposition, { v: "accent" | "success" | "neutral" | "warning"; label: string }> = {
    recognized:     { v: "success", label: "Pendapatan Booking" },
    // Item 3 (2026-09): "held"/"pending_refund"/"refunded" kini juga dipakai
    // booking BARU yang ditandai Refundable saat dibuat — bukan lagi
    // eksklusif baris legacy pra-rule 2026-07-29.
    held:           { v: "accent",  label: "Titipan Booking (refundable)" },
    pending_refund: { v: "warning", label: "Menunggu Refund" },
    refunded:       { v: "success", label: "Sudah Direfund" },
    // Masih legacy-only: booking baru tidak pernah mencapai state ini
    // (forfeited hanya utk held+non-refundable, yang tidak lagi bisa dibuat;
    // transferred hanya jalur konversi legacy CountsTowardPrice=true).
    transferred:    { v: "success", label: "→ Saldo Kredit Buyer (legacy)" },
    forfeited:      { v: "neutral", label: "Hangus (legacy)" },
  };
  const m = map[d];
  return <Badge variant={m.v}>{m.label}</Badge>;
}

export function isBookingExpired(b: { status: BookingStatus; expiry_date: string }): boolean {
  return b.status === "active" && new Date(b.expiry_date) < new Date();
}
