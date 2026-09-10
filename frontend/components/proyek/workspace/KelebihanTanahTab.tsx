"use client";

// LT-3 (kelebihan-tanah-final-architecture §B.1) — admin CRUD minimal untuk
// pool inventory Kelebihan Tanah per proyek. reserved/sold murni counter,
// baru bergerak di LT-4 (reservasi) & LT-5 (penjualan) — di sini
// total_quantity_m2 dan DUA harga bisa dikoreksi admin: unit_price (Harga
// Jual — dipakai di reservasi/Akad/DPP, menentukan pendapatan) dan
// purchase_price (Harga Beli — tarif HPP per m² langsung dipakai saat Akad,
// koreksi klien 2026-08-31; lihat backend/internal/land/hpp_resolver.go).
//
// LT-4 (§B.2, §D) — reservasi: soft-lock kuantitas untuk customer. Keputusan
// klien 2026-08-20: TANPA booking fee — murni soft-lock, tidak ada transaksi
// finansial/jurnal apa pun.

import { useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { Rupiah } from "@/components/format/Rupiah";
import { Can } from "@/components/ui/Can";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { fetchLandStock, createLandStock, updateLandStock, type LandStock } from "@/lib/api/land";
import {
  fetchReservations,
  createReservation,
  cancelReservation,
  type LandStockReservation,
  type ReservationStatus,
} from "@/lib/api/land";
import {
  recordAkad,
  fetchLandSales,
  cancelLandSale,
  type LandSale,
  type LandSaleStatus,
} from "@/lib/api/land";
import {
  fetchCustomers,
  createCustomer,
  fetchSalesPersons,
  type Customer,
  type SalesPerson,
} from "@/lib/api/party";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
  projectId: number;
}

function availableM2(pool: LandStock): number {
  return Number(pool.total_quantity_m2) - Number(pool.reserved_quantity_m2) - Number(pool.sold_quantity_m2);
}

function StatCell({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs text-text-tertiary uppercase tracking-wide">{label}</p>
      <p className="text-lg font-semibold text-text-primary tabular">{value}</p>
    </div>
  );
}

function todayStr(): string {
  return todayLocalStr();
}

// toRFC3339Date mengubah tanggal kalender ("YYYY-MM-DD") jadi RFC3339 pada
// tengah malam UTC — bukan lewat `new Date(...).toISOString()`, yang membaca
// string itu sebagai tengah malam ZONA WAKTU BROWSER lalu mengonversi ke UTC.
// Untuk pengguna WIB/WITA/WIT (UTC+7/8/9), konversi itu mundur satu hari
// (mis. 27 Agustus terkirim sebagai 2026-08-26T17:00:00Z) — backend & kolom
// DATE (server UTC) lalu mencatat tanggal akuntansi yang salah.
function toRFC3339Date(dateStr: string): string {
  return `${dateStr}T00:00:00.000Z`;
}

function reservationStatusVariant(status: ReservationStatus): "default" | "success" | "warning" | "danger" {
  switch (status) {
    case "active":
      return "default";
    case "converted":
      return "success";
    case "expired":
    case "cancelled":
      return "danger";
    default:
      return "default";
  }
}

const RESERVATION_STATUS_LABEL: Record<ReservationStatus, string> = {
  active: "Aktif",
  converted: "Terkonversi",
  expired: "Kedaluwarsa",
  cancelled: "Dibatalkan",
};

function landSaleStatusVariant(status: LandSaleStatus): "default" | "success" | "warning" | "danger" {
  switch (status) {
    case "akad":
      return "success";
    case "cancelled":
      return "danger";
    default:
      return "default";
  }
}

const LAND_SALE_STATUS_LABEL: Record<LandSaleStatus, string> = {
  draft: "Draf",
  akad: "Akad",
  cancelled: "Dibatalkan",
};

export function KelebihanTanahTab({ token, projectId }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(true);
  const [pool, setPool] = useState<LandStock | null>(null);
  const [error, setError] = useState(false);

  const [open, setOpen] = useState(false);
  const [totalQty, setTotalQty] = useState("");
  const [unitPrice, setUnitPrice] = useState("");
  const [purchasePrice, setPurchasePrice] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const [reservations, setReservations] = useState<LandStockReservation[]>([]);
  const [reservationsLoading, setReservationsLoading] = useState(true);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [salesPersons, setSalesPersons] = useState<SalesPerson[]>([]);

  const [reserveOpen, setReserveOpen] = useState(false);
  const [reserveSubmitting, setReserveSubmitting] = useState(false);
  const [customerId, setCustomerId] = useState("");
  const [salesPersonId, setSalesPersonId] = useState("");
  const [newCustomer, setNewCustomer] = useState(false);
  const [newName, setNewName] = useState("");
  const [reserveQty, setReserveQty] = useState("");
  const [reservedAt, setReservedAt] = useState(todayStr());
  const [expiryDate, setExpiryDate] = useState("");

  const [cancelTarget, setCancelTarget] = useState<LandStockReservation | null>(null);
  const [cancelReason, setCancelReason] = useState("");
  const [cancelSubmitting, setCancelSubmitting] = useState(false);

  const [landSales, setLandSales] = useState<LandSale[]>([]);
  const [landSalesLoading, setLandSalesLoading] = useState(true);

  const [akadOpen, setAkadOpen] = useState(false);
  const [akadSubmitting, setAkadSubmitting] = useState(false);
  const [akadReservationId, setAkadReservationId] = useState("");
  const [akadCustomerId, setAkadCustomerId] = useState("");
  const [akadSalesPersonId, setAkadSalesPersonId] = useState("");
  const [akadNewCustomer, setAkadNewCustomer] = useState(false);
  const [akadNewName, setAkadNewName] = useState("");
  const [akadQty, setAkadQty] = useState("");
  const [akadUnitPrice, setAkadUnitPrice] = useState("");
  const [akadDpp, setAkadDpp] = useState("");
  const [akadIsPKP, setAkadIsPKP] = useState(false);
  const [akadVatRate, setAkadVatRate] = useState("0.11");
  const [akadPaymentAccount, setAkadPaymentAccount] = useState("");
  const [akadRecognitionDate, setAkadRecognitionDate] = useState(todayStr());

  const [cancelSaleTarget, setCancelSaleTarget] = useState<LandSale | null>(null);
  const [cancelSaleReason, setCancelSaleReason] = useState("");
  const [cancelSaleSubmitting, setCancelSaleSubmitting] = useState(false);

  async function load() {
    setLoading(true);
    setError(false);
    try {
      const result = await fetchLandStock(token, projectId);
      setPool(result);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  }

  async function loadReservations() {
    setReservationsLoading(true);
    try {
      const result = await fetchReservations(token, projectId);
      setReservations(result);
    } catch {
      // Daftar reservasi non-blocking — pool tetap tampil walau ini gagal.
    } finally {
      setReservationsLoading(false);
    }
  }

  async function loadLandSales() {
    setLandSalesLoading(true);
    try {
      const result = await fetchLandSales(token, projectId);
      setLandSales(result);
    } catch {
      // Daftar akad non-blocking — pool & reservasi tetap tampil walau ini gagal.
    } finally {
      setLandSalesLoading(false);
    }
  }

  useEffect(() => {
    load();
    loadReservations();
    loadLandSales();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  const qtyErr = totalQty !== "" && (isNaN(Number(totalQty)) || Number(totalQty) < 0)
    ? "Kuantitas harus angka, tidak boleh negatif" : "";
  const priceErr = unitPrice !== "" ? validateRupiah(unitPrice) : "";
  const purchasePriceErr = purchasePrice !== "" ? validateRupiah(purchasePrice) : "";
  const canSubmit = totalQty !== "" && !qtyErr && !priceErr && !purchasePriceErr;

  function openModal() {
    setTotalQty(pool?.total_quantity_m2 ?? "");
    setUnitPrice(pool?.unit_price ?? "");
    setPurchasePrice(pool?.purchase_price ?? "");
    setOpen(true);
  }

  async function handleSubmit() {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      const input = {
        total_quantity_m2: totalQty,
        unit_price: unitPrice || "0",
        purchase_price: purchasePrice || "0",
      };
      const saved = pool
        ? await updateLandStock(token, projectId, input)
        : await createLandStock(token, projectId, input);
      setPool(saved);
      toast(pool ? "Pool Kelebihan Tanah diperbarui." : "Pool Kelebihan Tanah dibuat.", "success");
      setOpen(false);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal menyimpan pool Kelebihan Tanah", "error");
    } finally {
      setSubmitting(false);
    }
  }

  function openReserveModal() {
    setCustomerId("");
    setSalesPersonId("");
    setNewCustomer(false);
    setNewName("");
    setReserveQty("");
    setReservedAt(todayStr());
    setExpiryDate("");
    fetchCustomers(token).then(setCustomers).catch(() => {});
    fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
    setReserveOpen(true);
  }

  const reserveQtyErr = reserveQty !== "" && (isNaN(Number(reserveQty)) || Number(reserveQty) <= 0)
    ? "Kuantitas harus angka positif" : "";
  const custErr = !newCustomer && !customerId ? "Pilih customer" : null;
  const newCustErr = newCustomer && newName.trim().length === 0 ? "Nama customer wajib" : null;
  const expiryErr = expiryDate && expiryDate <= reservedAt ? "Berlaku s/d harus setelah tanggal reservasi" : null;

  function reserveValid() {
    return reserveQty !== "" && !reserveQtyErr && !custErr && !newCustErr && !expiryErr;
  }

  async function handleReserveSubmit() {
    if (!reserveValid()) return;
    setReserveSubmitting(true);
    try {
      let cid = parseInt(customerId, 10);
      if (newCustomer) {
        const created = await createCustomer(token, {
          code: `CUST-${Date.now()}`,
          name: newName.trim(),
        });
        cid = created.id;
      }
      await createReservation(token, projectId, {
        customer_id: cid,
        sales_person_id: salesPersonId ? parseInt(salesPersonId, 10) : undefined,
        quantity_m2: reserveQty,
        reserved_at: toRFC3339Date(reservedAt),
        expiry_date: expiryDate ? toRFC3339Date(expiryDate) : undefined,
      });
      toast("Reservasi dibuat — soft-lock kuantitas, tidak ada transaksi finansial.", "success");
      setReserveOpen(false);
      await Promise.all([load(), loadReservations()]);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal membuat reservasi", "error");
    } finally {
      setReserveSubmitting(false);
    }
  }

  async function handleCancelSubmit() {
    if (!cancelTarget) return;
    setCancelSubmitting(true);
    try {
      await cancelReservation(token, projectId, cancelTarget.id, cancelReason || "Dibatalkan admin");
      toast("Reservasi dibatalkan — kuantitas kembali tersedia.", "success");
      setCancelTarget(null);
      setCancelReason("");
      await Promise.all([load(), loadReservations()]);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal membatalkan reservasi", "error");
    } finally {
      setCancelSubmitting(false);
    }
  }

  const activeReservations = reservations.filter((r) => r.status === "active");
  const akadSelectedReservation = akadReservationId
    ? activeReservations.find((r) => r.id === Number(akadReservationId)) ?? null
    : null;

  function openAkadModal() {
    setAkadReservationId("");
    setAkadCustomerId("");
    setAkadSalesPersonId("");
    setAkadNewCustomer(false);
    setAkadNewName("");
    setAkadQty("");
    setAkadUnitPrice(pool?.unit_price ?? "");
    setAkadDpp("");
    setAkadIsPKP(false);
    setAkadVatRate("0.11");
    setAkadPaymentAccount("");
    setAkadRecognitionDate(todayStr());
    if (customers.length === 0) fetchCustomers(token).then(setCustomers).catch(() => {});
    if (salesPersons.length === 0) fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
    setAkadOpen(true);
  }

  function handleAkadReservationChange(value: string) {
    setAkadReservationId(value);
    const r = value ? activeReservations.find((x) => x.id === Number(value)) : null;
    if (r) {
      setAkadCustomerId(String(r.customer_id));
      setAkadQty(r.quantity_m2);
      setAkadUnitPrice(r.unit_price_snapshot);
      setAkadNewCustomer(false);
    } else {
      setAkadQty("");
    }
  }

  const akadQtyNum = Number(akadQty);
  const akadQtyErr = !akadSelectedReservation && akadQty !== "" && (isNaN(akadQtyNum) || akadQtyNum <= 0)
    ? "Kuantitas harus angka positif"
    : !akadSelectedReservation && pool && akadQtyNum > availableM2(pool)
    ? `Melebihi tersedia (${availableM2(pool).toLocaleString("id-ID")} m²)`
    : "";
  const akadCustErr = !akadSelectedReservation && !akadNewCustomer && !akadCustomerId ? "Pilih customer" : null;
  const akadNewCustErr = !akadSelectedReservation && akadNewCustomer && akadNewName.trim().length === 0
    ? "Nama customer wajib" : null;
  const akadDppErr = akadDpp !== "" ? validateRupiah(akadDpp) : "DPP wajib diisi";
  const akadVatRateErr = akadIsPKP && (isNaN(parseFloat(akadVatRate)) || parseFloat(akadVatRate) <= 0)
    ? "Rate PPN tidak valid" : null;
  const akadPaymentErr = !akadPaymentAccount ? "Pilih rekening/kas tujuan" : null;

  function akadValid() {
    return (
      akadQty !== "" && !akadQtyErr &&
      !akadCustErr && !akadNewCustErr &&
      akadDpp !== "" && !akadDppErr &&
      !akadVatRateErr &&
      !akadPaymentErr
    );
  }

  const akadGrossPreview = akadDpp && !akadDppErr
    ? Number(akadDpp) + (akadIsPKP && !akadVatRateErr ? Math.round(Number(akadDpp) * parseFloat(akadVatRate)) : 0)
    : 0;

  async function handleAkadSubmit() {
    if (!akadValid()) return;
    setAkadSubmitting(true);
    try {
      let cid = akadSelectedReservation ? akadSelectedReservation.customer_id : parseInt(akadCustomerId, 10);
      if (!akadSelectedReservation && akadNewCustomer) {
        const created = await createCustomer(token, {
          code: `CUST-${Date.now()}`,
          name: akadNewName.trim(),
        });
        cid = created.id;
      }
      const record = await recordAkad(token, projectId, {
        reservation_id: akadSelectedReservation ? akadSelectedReservation.id : undefined,
        customer_id: cid,
        sales_person_id: akadSalesPersonId ? parseInt(akadSalesPersonId, 10) : undefined,
        quantity_m2: akadSelectedReservation ? akadSelectedReservation.quantity_m2 : akadQty,
        unit_price_snapshot: akadUnitPrice || undefined,
        dpp_amount: akadDpp,
        is_pkp: akadIsPKP,
        vat_rate_snapshot: akadIsPKP ? akadVatRate : undefined,
        payment_account_code: akadPaymentAccount,
        recognition_date: toRFC3339Date(akadRecognitionDate),
      });
      toast(`Akad tercatat — pendapatan & HPP diakui (Jurnal #${record.revenue_journal_id}).`, "success");
      setAkadOpen(false);
      await Promise.all([load(), loadReservations(), loadLandSales()]);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal mencatat Akad", "error");
    } finally {
      setAkadSubmitting(false);
    }
  }

  async function handleCancelSaleSubmit() {
    if (!cancelSaleTarget || !cancelSaleReason.trim()) return;
    setCancelSaleSubmitting(true);
    try {
      await cancelLandSale(token, projectId, cancelSaleTarget.id, cancelSaleReason.trim());
      toast("Akad dibatalkan — jurnal pendapatan & HPP dibalik, kuantitas kembali tersedia.", "success");
      setCancelSaleTarget(null);
      setCancelSaleReason("");
      await Promise.all([load(), loadLandSales()]);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal membatalkan Akad", "error");
    } finally {
      setCancelSaleSubmitting(false);
    }
  }

  if (loading) {
    return <Card><p className="text-sm text-text-secondary">Memuat…</p></Card>;
  }

  if (error) {
    return (
      <Card>
        <EmptyState
          title="Gagal Memuat Kelebihan Tanah"
          description="Data pool Kelebihan Tanah proyek ini tidak dapat dimuat."
        />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      {!pool ? (
        <Card>
          <EmptyState
            title="Belum Ada Pool Kelebihan Tanah"
            description="Proyek ini belum punya pool inventory Kelebihan Tanah. Buat pool untuk mulai mencatat kuantitas dan harga per m² yang tersedia untuk dijual sebagai produk tambahan."
            action={
              <Can roles={["owner", "accountant"]}>
                <Button onClick={openModal}>Buat Pool Kelebihan Tanah</Button>
              </Can>
            }
          />
        </Card>
      ) : (
        <>
          <Card>
            <div className="flex items-start justify-between mb-4">
              <p className="text-sm font-semibold text-text-primary">Pool Kelebihan Tanah</p>
              <Can roles={["owner", "accountant"]}>
                <Button variant="secondary" size="sm" onClick={openModal}>Ubah</Button>
              </Can>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
              <StatCell label="Total (m²)" value={Number(pool.total_quantity_m2).toLocaleString("id-ID")} />
              <StatCell label="Direservasi (m²)" value={Number(pool.reserved_quantity_m2).toLocaleString("id-ID")} />
              <StatCell label="Terjual (m²)" value={Number(pool.sold_quantity_m2).toLocaleString("id-ID")} />
              <StatCell label="Tersedia (m²)" value={availableM2(pool).toLocaleString("id-ID")} />
            </div>
            <div className="mt-4 pt-4 border-t border-border-subtle grid grid-cols-2 gap-4">
              <div>
                <p className="text-xs text-text-tertiary uppercase tracking-wide">Harga Jual per m²</p>
                <p className="text-lg font-semibold text-text-primary tabular"><Rupiah value={pool.unit_price} /></p>
              </div>
              <div>
                <p className="text-xs text-text-tertiary uppercase tracking-wide">Harga Beli per m² (basis HPP)</p>
                <p className="text-lg font-semibold text-text-secondary tabular"><Rupiah value={pool.purchase_price} /></p>
              </div>
            </div>
          </Card>

          <Card>
            <div className="flex items-start justify-between mb-4">
              <div>
                <p className="text-sm font-semibold text-text-primary">Reservasi</p>
                <p className="text-xs text-text-tertiary mt-0.5">
                  Soft-lock kuantitas untuk customer — tanpa booking fee, tanpa transaksi finansial/jurnal.
                </p>
              </div>
              <Can roles={["owner", "accountant"]}>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={openReserveModal}
                  disabled={availableM2(pool) <= 0}
                >
                  Buat Reservasi
                </Button>
              </Can>
            </div>

            {reservationsLoading ? (
              <p className="text-sm text-text-secondary">Memuat reservasi…</p>
            ) : reservations.length === 0 ? (
              <p className="text-sm text-text-tertiary">Belum ada reservasi untuk proyek ini.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-xs text-text-tertiary uppercase tracking-wide border-b border-border-subtle">
                      <th className="py-2 pr-3">Customer</th>
                      <th className="py-2 pr-3">Kuantitas (m²)</th>
                      <th className="py-2 pr-3">Direservasi</th>
                      <th className="py-2 pr-3">Berlaku s/d</th>
                      <th className="py-2 pr-3">Status</th>
                      <th className="py-2 pr-3" />
                    </tr>
                  </thead>
                  <tbody>
                    {reservations.map((r) => (
                      <tr key={r.id} className="border-b border-border-subtle last:border-0">
                        <td className="py-2 pr-3 text-text-primary">
                          {customers.find((c) => c.id === r.customer_id)?.name ?? `Customer #${r.customer_id}`}
                        </td>
                        <td className="py-2 pr-3 tabular">{Number(r.quantity_m2).toLocaleString("id-ID")}</td>
                        <td className="py-2 pr-3">{new Date(r.reserved_at).toLocaleDateString("id-ID")}</td>
                        <td className="py-2 pr-3">
                          {r.expiry_date ? new Date(r.expiry_date).toLocaleDateString("id-ID") : "—"}
                        </td>
                        <td className="py-2 pr-3">
                          <Badge variant={reservationStatusVariant(r.status)}>
                            {RESERVATION_STATUS_LABEL[r.status]}
                          </Badge>
                        </td>
                        <td className="py-2 pr-3 text-right">
                          {r.status === "active" && (
                            <Can roles={["owner", "accountant"]}>
                              <button
                                type="button"
                                className="text-xs text-danger hover:underline"
                                onClick={() => { setCancelTarget(r); setCancelReason(""); }}
                              >
                                Batalkan
                              </button>
                            </Can>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <Card>
            <div className="flex items-start justify-between mb-4">
              <div>
                <p className="text-sm font-semibold text-text-primary">Penjualan (Akad)</p>
                <p className="text-xs text-text-tertiary mt-0.5">
                  Akad bersifat permanen — pendapatan & HPP diakui saat ini, langsung ke jurnal.
                </p>
              </div>
              <Can roles={["owner", "accountant"]}>
                <Button variant="secondary" size="sm" onClick={openAkadModal}>
                  Catat Akad
                </Button>
              </Can>
            </div>

            {landSalesLoading ? (
              <p className="text-sm text-text-secondary">Memuat penjualan…</p>
            ) : landSales.length === 0 ? (
              <p className="text-sm text-text-tertiary">Belum ada Akad Kelebihan Tanah untuk proyek ini.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-xs text-text-tertiary uppercase tracking-wide border-b border-border-subtle">
                      <th className="py-2 pr-3">Customer</th>
                      <th className="py-2 pr-3">Kuantitas (m²)</th>
                      <th className="py-2 pr-3">DPP</th>
                      <th className="py-2 pr-3">PPN</th>
                      <th className="py-2 pr-3">Bruto</th>
                      <th className="py-2 pr-3">Tanggal Akad</th>
                      <th className="py-2 pr-3">Status</th>
                      <th className="py-2 pr-3" />
                    </tr>
                  </thead>
                  <tbody>
                    {landSales.map((s) => (
                      <tr key={s.id} className="border-b border-border-subtle last:border-0">
                        <td className="py-2 pr-3 text-text-primary">
                          {customers.find((c) => c.id === s.customer_id)?.name ?? `Customer #${s.customer_id}`}
                        </td>
                        <td className="py-2 pr-3 tabular">{Number(s.quantity_m2).toLocaleString("id-ID")}</td>
                        <td className="py-2 pr-3 tabular"><Rupiah value={s.dpp_amount} /></td>
                        <td className="py-2 pr-3">
                          {s.is_pkp ? <Badge variant="default">PKP</Badge> : <span className="text-text-tertiary">—</span>}
                        </td>
                        <td className="py-2 pr-3 tabular"><Rupiah value={s.gross_amount} /></td>
                        <td className="py-2 pr-3">
                          {s.recognition_date ? new Date(s.recognition_date).toLocaleDateString("id-ID") : "—"}
                        </td>
                        <td className="py-2 pr-3">
                          <Badge variant={landSaleStatusVariant(s.status)}>
                            {LAND_SALE_STATUS_LABEL[s.status]}
                          </Badge>
                        </td>
                        <td className="py-2 pr-3 text-right">
                          {s.status === "akad" && (
                            <Can roles={["owner", "accountant"]}>
                              <button
                                type="button"
                                className="text-xs text-danger hover:underline"
                                onClick={() => { setCancelSaleTarget(s); setCancelSaleReason(""); }}
                              >
                                Batalkan
                              </button>
                            </Can>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}

      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title={pool ? "Ubah Pool Kelebihan Tanah" : "Buat Pool Kelebihan Tanah"}
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={submitting}>Batal</Button>
            <Button onClick={handleSubmit} loading={submitting} disabled={!canSubmit || submitting}>Simpan</Button>
          </>
        }
      >
        <FormGrid>
          <Input
            label="Total Kuantitas (m²)"
            type="number"
            step="0.01"
            value={totalQty}
            onChange={(e) => setTotalQty(e.target.value)}
            suffix="m²"
            error={qtyErr || undefined}
            autoFocus
          />
          <RupiahInput
            label="Harga Jual per m²"
            value={unitPrice}
            onChange={setUnitPrice}
            error={priceErr || undefined}
            hint="dipakai di reservasi, Akad, dan DPP — menentukan pendapatan"
          />
          <RupiahInput
            label="Harga Beli per m²"
            value={purchasePrice}
            onChange={setPurchasePrice}
            error={purchasePriceErr || undefined}
            hint="dipakai langsung sebagai tarif HPP per m² saat Akad"
          />
        </FormGrid>
      </Modal>

      <Modal
        open={reserveOpen}
        onClose={() => setReserveOpen(false)}
        title="Buat Reservasi Kelebihan Tanah"
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setReserveOpen(false)} disabled={reserveSubmitting}>Batal</Button>
            <Button onClick={handleReserveSubmit} loading={reserveSubmitting} disabled={!reserveValid()}>
              Simpan Reservasi
            </Button>
          </>
        }
      >
        <FormGrid>
          {!newCustomer ? (
            <div>
              <Select
                label="Customer" required
                value={customerId}
                onChange={(e) => setCustomerId(e.target.value)}
              >
                <option value="">— pilih customer —</option>
                {customers.map((c) => (
                  <option key={c.id} value={c.id}>{c.name} ({c.code})</option>
                ))}
              </Select>
              <button
                type="button"
                className="text-xs text-accent hover:underline mt-1"
                onClick={() => setNewCustomer(true)}
              >
                + Customer baru
              </button>
            </div>
          ) : (
            <div>
              <Input
                label="Nama Customer Baru" required
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="cth: Budi Santoso"
              />
              <button
                type="button"
                className="text-xs text-accent hover:underline mt-1"
                onClick={() => setNewCustomer(false)}
              >
                ← pilih dari daftar
              </button>
            </div>
          )}

          <Select
            label="Sales Person"
            value={salesPersonId}
            onChange={(e) => setSalesPersonId(e.target.value)}
          >
            <option value="">— tanpa atribusi —</option>
            {salesPersons.filter((s) => s.is_active).map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </Select>

          <Input
            label="Kuantitas (m²)" required
            type="number"
            step="0.01"
            value={reserveQty}
            onChange={(e) => setReserveQty(e.target.value)}
            suffix="m²"
            error={reserveQtyErr || undefined}
            hint={pool ? `Tersedia ${availableM2(pool).toLocaleString("id-ID")} m²` : undefined}
          />

          <Input
            label="Tanggal Reservasi"
            type="date"
            value={reservedAt}
            onChange={(e) => setReservedAt(e.target.value)}
          />
          <Input
            label="Berlaku s/d"
            type="date"
            value={expiryDate}
            onChange={(e) => setExpiryDate(e.target.value)}
            error={expiryErr ?? undefined}
            hint="opsional — kosongkan bila tanpa batas waktu"
          />

          <FormFull>
            <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
              Reservasi adalah <strong>soft-lock kuantitas</strong> saja — tidak ada booking fee,
              tidak ada transaksi finansial, dan tidak ada jurnal yang tercatat. Kuantitas kembali
              tersedia otomatis bila reservasi dibatalkan atau lewat masa berlaku.
            </p>
          </FormFull>
        </FormGrid>
      </Modal>

      <Modal
        open={!!cancelTarget}
        onClose={() => setCancelTarget(null)}
        title="Batalkan Reservasi"
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setCancelTarget(null)} disabled={cancelSubmitting}>Tutup</Button>
            <Button variant="danger" onClick={handleCancelSubmit} loading={cancelSubmitting}>
              Batalkan Reservasi
            </Button>
          </>
        }
      >
        <FormGrid>
          <FormFull>
            <p className="text-sm text-text-secondary">
              Kuantitas {cancelTarget ? Number(cancelTarget.quantity_m2).toLocaleString("id-ID") : ""} m² akan
              kembali tersedia untuk direservasi ulang.
            </p>
          </FormFull>
          <FormFull>
            <Input
              label="Alasan Pembatalan"
              value={cancelReason}
              onChange={(e) => setCancelReason(e.target.value)}
              placeholder="opsional"
            />
          </FormFull>
        </FormGrid>
      </Modal>

      <Modal
        open={akadOpen}
        onClose={() => setAkadOpen(false)}
        title="Catat Akad Kelebihan Tanah"
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setAkadOpen(false)} disabled={akadSubmitting}>Batal</Button>
            <Button onClick={handleAkadSubmit} loading={akadSubmitting} disabled={!akadValid()}>
              Catat Akad
            </Button>
          </>
        }
      >
        <FormGrid>
          <FormFull>
            <div className="bg-warning-bg border border-warning/30 rounded-lg px-3 py-2.5 text-xs text-warning">
              Akad bersifat permanen — pendapatan dan HPP diakui saat ini, langsung ke jurnal akuntansi.
            </div>
          </FormFull>

          {activeReservations.length > 0 && (
            <FormFull>
              <Select
                label="Dari Reservasi"
                value={akadReservationId}
                onChange={(e) => handleAkadReservationChange(e.target.value)}
              >
                <option value="">— tanpa reservasi (penjualan langsung) —</option>
                {activeReservations.map((r) => (
                  <option key={r.id} value={r.id}>
                    {customers.find((c) => c.id === r.customer_id)?.name ?? `Customer #${r.customer_id}`}
                    {" · "}{Number(r.quantity_m2).toLocaleString("id-ID")} m²
                  </option>
                ))}
              </Select>
            </FormFull>
          )}

          {akadSelectedReservation ? (
            <FormFull>
              <p className="text-xs text-text-secondary bg-border-subtle/60 border border-border-subtle rounded-lg px-3 py-2">
                Reservasi dipilih — customer & kuantitas ({Number(akadSelectedReservation.quantity_m2).toLocaleString("id-ID")} m²)
                mengikuti reservasi dan tidak bisa diubah di sini.
              </p>
            </FormFull>
          ) : (
            <>
              {!akadNewCustomer ? (
                <div>
                  <Select
                    label="Customer" required
                    value={akadCustomerId}
                    onChange={(e) => setAkadCustomerId(e.target.value)}
                  >
                    <option value="">— pilih customer —</option>
                    {customers.map((c) => (
                      <option key={c.id} value={c.id}>{c.name} ({c.code})</option>
                    ))}
                  </Select>
                  <button
                    type="button"
                    className="text-xs text-accent hover:underline mt-1"
                    onClick={() => setAkadNewCustomer(true)}
                  >
                    + Customer baru
                  </button>
                </div>
              ) : (
                <div>
                  <Input
                    label="Nama Customer Baru" required
                    value={akadNewName}
                    onChange={(e) => setAkadNewName(e.target.value)}
                    placeholder="cth: Budi Santoso"
                  />
                  <button
                    type="button"
                    className="text-xs text-accent hover:underline mt-1"
                    onClick={() => setAkadNewCustomer(false)}
                  >
                    ← pilih dari daftar
                  </button>
                </div>
              )}

              <Input
                label="Kuantitas (m²)" required
                type="number"
                step="0.01"
                value={akadQty}
                onChange={(e) => setAkadQty(e.target.value)}
                suffix="m²"
                error={akadQtyErr || undefined}
                hint={pool ? `Tersedia ${availableM2(pool).toLocaleString("id-ID")} m²` : undefined}
              />
            </>
          )}

          <Select
            label="Sales Person"
            value={akadSalesPersonId}
            onChange={(e) => setAkadSalesPersonId(e.target.value)}
          >
            <option value="">— tanpa atribusi —</option>
            {salesPersons.filter((s) => s.is_active).map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </Select>

          <RupiahInput
            label="DPP (Harga Jual sebelum PPN)"
            value={akadDpp}
            onChange={setAkadDpp}
            required
            error={akadDpp && akadDppErr ? akadDppErr : undefined}
          />

          <Input
            label="Tanggal Akad"
            type="date"
            value={akadRecognitionDate}
            onChange={(e) => setAkadRecognitionDate(e.target.value)}
            required
          />

          <label
            htmlFor="land-akad-pkp"
            className="flex cursor-pointer items-center gap-3 rounded-lg border border-border px-3.5 py-2.5
              transition-colors hover:bg-border-subtle/40"
          >
            <input
              id="land-akad-pkp"
              type="checkbox"
              checked={akadIsPKP}
              onChange={(e) => setAkadIsPKP(e.target.checked)}
              className="h-4 w-4 accent-accent cursor-pointer"
            />
            <span className="text-sm text-text-primary">Penjual PKP (kenakan PPN)</span>
          </label>

          {akadIsPKP && (
            <Input
              label="Rate PPN"
              value={akadVatRate}
              onChange={(e) => setAkadVatRate(e.target.value)}
              placeholder="0.11"
              hint="Desimal: 0.11 = 11%"
              error={akadVatRateErr ?? undefined}
            />
          )}

          <FormFull>
            <CashBankSelect
              token={token}
              value={akadPaymentAccount}
              onChange={setAkadPaymentAccount}
              label="Rekening / Kas Penerimaan"
              required
            />
          </FormFull>

          <FormFull>
            <p className="text-xs text-text-secondary bg-border-subtle/60 border border-border-subtle rounded-lg px-3 py-2">
              Perkiraan bruto: <strong>{akadGrossPreview.toLocaleString("id-ID")}</strong>. Skema tunai/lunas
              saja — pembayaran diterima langsung saat Akad, tidak ada termin/cicilan untuk Kelebihan Tanah.
            </p>
          </FormFull>
        </FormGrid>
      </Modal>

      <Modal
        open={!!cancelSaleTarget}
        onClose={() => setCancelSaleTarget(null)}
        title="Batalkan Akad Kelebihan Tanah"
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setCancelSaleTarget(null)} disabled={cancelSaleSubmitting}>Tutup</Button>
            <Button
              variant="danger"
              onClick={handleCancelSaleSubmit}
              loading={cancelSaleSubmitting}
              disabled={!cancelSaleReason.trim()}
            >
              Batalkan Akad
            </Button>
          </>
        }
      >
        <FormGrid>
          <FormFull>
            <div className="bg-warning-bg border border-warning/30 rounded-lg px-3 py-2.5 text-xs text-warning">
              Jurnal pendapatan{cancelSaleTarget?.cogs_journal_id ? " & HPP" : ""} akan dibalik (jurnal pembalik,
              histori tidak dihapus), dan kuantitas {cancelSaleTarget ? Number(cancelSaleTarget.quantity_m2).toLocaleString("id-ID") : ""} m²
              kembali tersedia untuk dijual ulang.
            </div>
          </FormFull>
          <FormFull>
            <Input
              label="Alasan Pembatalan" required
              value={cancelSaleReason}
              onChange={(e) => setCancelSaleReason(e.target.value)}
              placeholder="cth: pembeli wanprestasi"
            />
          </FormFull>
        </FormGrid>
      </Modal>
    </div>
  );
}
