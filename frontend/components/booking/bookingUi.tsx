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
    // Legacy (baris histori pra-rule 2026-07-29):
    held:           { v: "accent",  label: "Titipan (legacy)" },
    transferred:    { v: "success", label: "→ Saldo Kredit Buyer (legacy)" },
    forfeited:      { v: "neutral", label: "Hangus (legacy)" },
    pending_refund: { v: "warning", label: "Menunggu Refund (legacy)" },
    refunded:       { v: "success", label: "Sudah Direfund (legacy)" },
  };
  const m = map[d];
  return <Badge variant={m.v}>{m.label}</Badge>;
}

export function isBookingExpired(b: { status: BookingStatus; expiry_date: string }): boolean {
  return b.status === "active" && new Date(b.expiry_date) < new Date();
}
