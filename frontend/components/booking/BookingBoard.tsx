"use client";

// Increment 7 — Papan Booking (docs/increment-7-frontend-spec.md P1).

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { useUnitMap } from "@/lib/hooks/useUnitMap";
import { fetchCustomers, type Customer } from "@/lib/api/party";
import {
  fetchBookings,
  cancelBooking,
  markExpiredBookings,
  type Booking,
  type BookingStatus,
} from "@/lib/api/booking";
import { createBookingRefund } from "@/lib/api/cancellation";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { useUserSafe } from "@/lib/context/UserContext";
import { BookingStatusBadge, FeeDispositionBadge, isBookingExpired } from "./bookingUi";

const TABS: { key: BookingStatus | ""; label: string }[] = [
  { key: "", label: "Semua" },
  { key: "active", label: "Aktif" },
  { key: "converted", label: "Terkonversi" },
  { key: "expired", label: "Kedaluwarsa" },
  { key: "cancelled", label: "Dibatalkan" },
];

interface BookingBoardProps {
  token: string;
  /** PS-2 — mode embed di workspace proyek: filter per proyek + tanpa header. */
  projectFilter?: number;
  embedded?: boolean;
}

export function BookingBoard({ token, projectFilter, embedded = false }: BookingBoardProps) {
  const { toast } = useToast();
  const router = useRouter();
  const { unitMap } = useUnitMap(token);
  // W-12 — marketing membuat & membatalkan booking, tetapi kwitansi, refund,
  // dan sweep kedaluwarsa (yang menjurnal fee hangus) adalah pekerjaan
  // accounting. Backend menolaknya 403; kolomnya tidak ditawarkan di sini.
  const isMarketing = useUserSafe()?.role === "marketing";

  const [tab, setTab] = useState<BookingStatus | "">("");
  const [rows, setRows] = useState<Booking[]>([]);
  const [loading, setLoading] = useState(true);
  const [customers, setCustomers] = useState<Record<number, Customer>>({});
  const [sweeping, setSweeping] = useState(false);

  const [cancelTarget, setCancelTarget] = useState<Booking | null>(null);
  const [cancelReason, setCancelReason] = useState("");
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const bs = await fetchBookings(token, tab);
      setRows(bs);
    } catch {
      toast("Gagal memuat booking", "error");
    } finally {
      setLoading(false);
    }
  }, [token, tab, toast]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    fetchCustomers(token)
      .then((cs) => setCustomers(Object.fromEntries(cs.map((c) => [c.id, c]))))
      .catch(() => {});
  }, [token]);

  // Filter per proyek (embed workspace) — memakai peta unit.
  const visibleRows = useMemo(() => {
    if (!projectFilter) return rows;
    return rows.filter((b) => unitMap[b.unit_id]?.projectId === projectFilter);
  }, [rows, projectFilter, unitMap]);

  const expiredCount = useMemo(() => visibleRows.filter(isBookingExpired).length, [visibleRows]);

  async function handleSweep() {
    setSweeping(true);
    try {
      const r = await markExpiredBookings(token);
      toast(
        r.expired > 0
          ? `${r.expired} booking ditutup (kedaluwarsa) — Pendapatan Booking tetap diakui`
          : "Tidak ada booking kedaluwarsa",
        "success",
      );
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memproses", "error");
    } finally {
      setSweeping(false);
    }
  }

  async function handleCancel() {
    if (!cancelTarget) return;
    setBusy(true);
    try {
      const b = await cancelBooking(token, cancelTarget.id, cancelReason || "dibatalkan");
      toast(
        b.fee_disposition === "pending_refund"
          ? "Booking dibatalkan — fee menunggu proses refund"
          : "Booking dibatalkan — fee hangus (Pendapatan Lain-lain)",
        "success",
      );
      setCancelTarget(null);
      setCancelReason("");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membatalkan", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleRefund(b: Booking) {
    setBusy(true);
    try {
      await createBookingRefund(token, b.id);
      toast("Refund dibuat — bayar dari halaman Pembatalan → tab Refund", "success");
      router.push("/penjualan/pembatalan?tab=refund");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat refund", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      {!embedded && (
        <div className="flex items-start justify-between flex-wrap gap-3">
          <div>
            <h1 className="display-lg text-2xl text-text-primary">Booking</h1>
            <p className="text-sm text-text-secondary mt-0.5">
              Pemesanan unit dengan booking fee sebelum PPJB. Buat booking dari halaman unit.
            </p>
          </div>
          <Link href="/penjualan" className="text-sm text-accent hover:underline font-medium">
            Pilih Unit untuk Booking →
          </Link>
        </div>
      )}

      {expiredCount > 0 && !isMarketing && (
        <div className="flex items-center justify-between rounded border border-warning bg-warning-bg px-3 py-2 text-sm">
          <span>⚠ {expiredCount} booking lewat masa berlaku</span>
          <Button size="sm" variant="secondary" onClick={handleSweep} loading={sweeping}>
            Proses Kedaluwarsa
          </Button>
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border overflow-x-auto">
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`px-3 py-2 text-sm whitespace-nowrap border-b-2 -mb-px transition-colors
              ${tab === t.key
                ? "border-accent text-accent font-medium"
                : "border-transparent text-text-secondary hover:text-accent"}`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(3)].map((_, i) => (
              <div key={i} className="h-9 bg-border-subtle rounded" />
            ))}
          </div>
        </Card>
      ) : visibleRows.length === 0 ? (
        <EmptyState
          title="Belum ada booking"
          description="Booking mengunci unit dengan booking fee sebelum PPJB. Buka halaman unit yang Tersedia lalu klik Booking Unit."
          action={
            <Link href="/penjualan">
              <Button size="sm">Buka Unit &amp; Penjualan</Button>
            </Link>
          }
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-text-secondary border-b border-border">
                <th className="py-2 pr-3">Unit</th>
                <th className="py-2 pr-3">Customer</th>
                <th className="py-2 pr-3 text-right">Fee</th>
                {/* Kwitansi punya kolom sendiri: setiap penerimaan uang harus
                    bisa ditunjuk dokumennya dari daftar, tanpa membuka detail. */}
                {!isMarketing && <th className="py-2 pr-3">Kwitansi</th>}
                <th className="py-2 pr-3">Berlaku s/d</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2 pr-3">Dana</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {visibleRows.map((b) => {
                const u = unitMap[b.unit_id];
                const expired = isBookingExpired(b);
                return (
                  <tr key={b.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-3">
                      <Link href={`/penjualan/${b.unit_id}`} className="text-accent hover:underline font-medium">
                        {u?.code ?? `#${b.unit_id}`}
                      </Link>
                      {u && <span className="block text-xs text-text-secondary">{u.projectName}</span>}
                    </td>
                    <td className="py-2 pr-3">{customers[b.customer_id]?.name ?? `#${b.customer_id}`}</td>
                    <td className="py-2 pr-3 text-right font-medium">
                      <Rupiah value={b.booking_fee} colorSign={false} />
                    </td>
                    {!isMarketing && (
                      <td className="py-2 pr-3">
                        {b.termin_payment_id ? (
                          <div className="flex flex-col gap-0.5">
                            <span className="font-mono text-xs text-text-secondary">
                              {b.receipt_number ?? "—"}
                            </span>
                            <PrintReceiptButton
                              token={token}
                              terminId={b.termin_payment_id}
                              notes="Booking fee"
                              label="Cetak"
                            />
                          </div>
                        ) : (
                          <span className="text-xs text-text-tertiary">—</span>
                        )}
                      </td>
                    )}
                    <td className="py-2 pr-3">
                      <span className={expired ? "text-danger" : ""}>
                        <Tanggal value={b.expiry_date} />
                      </span>
                    </td>
                    <td className="py-2 pr-3"><BookingStatusBadge status={b.status} expired={expired} /></td>
                    <td className="py-2 pr-3"><FeeDispositionBadge d={b.fee_disposition} /></td>
                    <td className="py-2 text-right">
                      <div className="flex justify-end gap-2">
                        {b.status === "active" && (
                          <>
                            <Link href={`/penjualan/${b.unit_id}?booking=${b.id}`}>
                              <Button size="sm">Konversi →</Button>
                            </Link>
                            <Button size="sm" variant="secondary" onClick={() => setCancelTarget(b)}>
                              Batalkan
                            </Button>
                          </>
                        )}
                        {b.fee_disposition === "pending_refund" && !isMarketing && (
                          <Button size="sm" variant="secondary" loading={busy} onClick={() => handleRefund(b)}>
                            Proses Refund
                          </Button>
                        )}
                        {b.status === "converted" && b.converted_contract_id && (
                          <Link
                            href={`/penjualan/${b.unit_id}?contract=${b.converted_contract_id}`}
                            className="text-xs text-accent hover:underline self-center"
                          >
                            Kontrak #{b.converted_contract_id}
                          </Link>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      )}

      {/* Cancel modal */}
      <Modal
        open={!!cancelTarget}
        onClose={() => setCancelTarget(null)}
        title={`Batalkan Booking #${cancelTarget?.id ?? ""}`}
        size="sm"
        footer={
          <>
            <Button variant="secondary" onClick={() => setCancelTarget(null)} disabled={busy}>
              Tutup
            </Button>
            <Button variant="danger" onClick={handleCancel} loading={busy}>
              Batalkan Booking
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input
            label="Alasan"
            value={cancelReason}
            onChange={(e) => setCancelReason(e.target.value)}
            placeholder="cth: buyer mengundurkan diri"
          />
          <p className="text-xs text-text-secondary">
            {cancelTarget?.fee_disposition === "recognized"
              ? "Booking fee sudah diakui sebagai Pendapatan Booking — pembatalan TIDAK mengubah pendapatan (tidak ada refund/reversal)."
              : cancelTarget?.refundable
                ? "Baris legacy: fee refundable — akan menunggu proses refund (dana di Titipan)."
                : "Baris legacy: fee non-refundable — akan hangus menjadi Pendapatan Lain-lain."}{" "}
            Unit kembali <strong>Tersedia</strong>.
          </p>
        </div>
      </Modal>
    </div>
  );
}
