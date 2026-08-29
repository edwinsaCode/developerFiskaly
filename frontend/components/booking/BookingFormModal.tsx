"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { createBooking } from "@/lib/api/booking";
import { fetchLandStock, type LandStock } from "@/lib/api/land";
import { formatRupiah } from "@/components/format/Rupiah";
import { dateToLocalStr, todayLocalStr } from "@/lib/date";
import {
  fetchCustomers,
  createCustomer,
  fetchSalesPersons,
  type Customer,
  type SalesPerson,
} from "@/lib/api/party";

interface Props {
  open: boolean;
  onClose: () => void;
  token: string;
  unitId: number;
  unitCode: string;
  projectId: number;
  onSuccess?: () => void;
}

// availableM2 — kuantitas Kelebihan Tanah yang belum direservasi/terjual.
// Murni display; pool.reserved/sold_quantity_m2 sudah dijaga backend row-lock.
function availableM2(pool: LandStock): number {
  return Number(pool.total_quantity_m2) - Number(pool.reserved_quantity_m2) - Number(pool.sold_quantity_m2);
}

function plusDays(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return dateToLocalStr(d);
}

export function BookingFormModal({ open, onClose, token, unitId, unitCode, projectId, onSuccess }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  const [customers, setCustomers] = useState<Customer[]>([]);
  const [salesPersons, setSalesPersons] = useState<SalesPerson[]>([]);
  const [customerId, setCustomerId] = useState("");
  const [salesPersonId, setSalesPersonId] = useState("");

  // Quick-create customer inline (master belum tentu terisi).
  const [newCustomer, setNewCustomer] = useState(false);
  const [newName, setNewName] = useState("");

  const [fee, setFee] = useState("");
  const feeErr = validateRupiah(fee);
  const [bank, setBank] = useState("");
  const [bookingDate, setBookingDate] = useState(todayLocalStr());
  const [expiryDate, setExpiryDate] = useState(plusDays(14));
  const [notes, setNotes] = useState("");

  // Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08)
  // — opsional. Salesperson HANYA mengisi kuantitas; harga & total di bawah
  // murni PREVIEW (dari LandStock.unit_price, dibaca dari server, tidak pernah
  // diedit) — tidak pernah dikirim ke backend, yang menyelesaikan reservasi +
  // snapshot harga sendiri secara atomik (land.ReserveTx).
  const [landPool, setLandPool] = useState<LandStock | null>(null);
  const [wantsLand, setWantsLand] = useState(false);
  const [landQty, setLandQty] = useState("");

  useEffect(() => {
    if (!open) return;
    fetchCustomers(token).then(setCustomers).catch(() => {});
    fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
    fetchLandStock(token, projectId).then(setLandPool).catch(() => setLandPool(null));
  }, [open, token, projectId]);

  useEffect(() => {
    if (!open) {
      setWantsLand(false);
      setLandQty("");
    }
  }, [open]);

  const custErr = !newCustomer && !customerId ? "Pilih customer" : null;
  const newCustErr = newCustomer && newName.trim().length === 0 ? "Nama customer wajib" : null;
  const expiryErr = expiryDate <= bookingDate ? "Berlaku s/d harus setelah tanggal booking" : null;

  const landAvailable = landPool ? availableM2(landPool) : 0;
  const landQtyNum = Number(landQty.replace(",", "."));
  const landQtyErr = !wantsLand
    ? null
    : !landQty || isNaN(landQtyNum) || landQtyNum <= 0
      ? "Kuantitas harus angka positif"
      : landQtyNum > landAvailable
        ? `Melebihi tersedia (${landAvailable.toLocaleString("id-ID")} m²)`
        : null;
  const landTotalPreview =
    wantsLand && landPool && !landQtyErr ? landQtyNum * Number(landPool.unit_price) : null;

  function isValid() {
    return !custErr && !newCustErr && !feeErr && !!bank && !expiryErr && !landQtyErr;
  }

  async function handleSubmit() {
    if (!isValid()) return;
    setLoading(true);
    try {
      let cid = parseInt(customerId, 10);
      if (newCustomer) {
        const created = await createCustomer(token, {
          code: `CUST-${Date.now()}`,
          name: newName.trim(),
        });
        cid = created.id;
      }
      const b = await createBooking(token, unitId, {
        customer_id: cid,
        sales_person_id: salesPersonId ? parseInt(salesPersonId, 10) : undefined,
        booking_fee: fee,
        refundable: false, // rule klien: tidak ada refund (backend juga mengabaikan)
        bank_account_code: bank,
        booking_date: bookingDate,
        expiry_date: expiryDate,
        notes: notes || undefined,
        land_quantity_m2: wantsLand && landQty ? landQty : undefined,
      });
      // Nomor kwitansi disebut apa adanya — "terbit otomatis" tanpa nomor
      // tidak bisa dicek siapa pun.
      const landNote = b.land_quantity_m2
        ? ` + Kelebihan Tanah ${Number(b.land_quantity_m2).toLocaleString("id-ID")} m² direservasi`
        : "";
      toast(
        (b.receipt_number
          ? `Booking #${b.id} dibuat — kwitansi ${b.receipt_number} terbit, fee diakui sebagai Pendapatan Booking`
          : `Booking #${b.id} dibuat — fee diakui sebagai Pendapatan Booking, kwitansi terbit otomatis`) + landNote,
        "success",
      );
      onClose();
      onSuccess?.();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat booking", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Booking Unit ${unitCode}`}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!isValid()}>
            Simpan &amp; Terima Fee
          </Button>
        </>
      }
    >
      <FormGrid>
        {/* Customer */}
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

        <RupiahInput
          label="Booking Fee" required
          value={fee}
          onChange={setFee}
          error={feeErr ?? undefined}
          placeholder="5.000.000"
        />

        <CashBankSelect token={token} value={bank} onChange={setBank} label="Diterima di" required />

        <Input
          label="Tanggal Booking"
          type="date"
          value={bookingDate}
          onChange={(e) => setBookingDate(e.target.value)}
        />
        <Input
          label="Berlaku s/d" required
          type="date"
          value={expiryDate}
          onChange={(e) => setExpiryDate(e.target.value)}
          error={expiryErr ?? undefined}
        />

        <FormFull>
          <Input
            label="Catatan"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="opsional"
          />
        </FormFull>

        {/* Produk Tambahan: Kelebihan Tanah — hanya tampil bila proyek punya
            pool. Sales HANYA mengisi kuantitas; harga & total di bawah murni
            preview dari harga pool (tidak pernah bisa diedit di sini). */}
        {landPool && landAvailable > 0 && (
          <FormFull>
            {!wantsLand ? (
              <button
                type="button"
                className="text-xs text-accent hover:underline"
                onClick={() => setWantsLand(true)}
              >
                + Tambahkan Produk Tambahan: Kelebihan Tanah
              </button>
            ) : (
              <div className="rounded-lg border border-border-subtle p-3 space-y-2">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-text-secondary">
                    Produk Tambahan: Kelebihan Tanah
                  </p>
                  <button
                    type="button"
                    className="text-xs text-text-tertiary hover:underline"
                    onClick={() => { setWantsLand(false); setLandQty(""); }}
                  >
                    Hapus
                  </button>
                </div>
                <Input
                  label="Kuantitas (m²)" required
                  value={landQty}
                  onChange={(e) => setLandQty(e.target.value)}
                  suffix="m²"
                  error={landQtyErr ?? undefined}
                  hint={!landQtyErr ? `Tersedia ${landAvailable.toLocaleString("id-ID")} m² · Rp ${Number(landPool.unit_price).toLocaleString("id-ID")}/m²` : undefined}
                  placeholder="10"
                />
                {landTotalPreview !== null && (
                  <p className="text-xs text-text-secondary">
                    Total: <strong>{formatRupiah(landTotalPreview)}</strong> (otomatis, harga dari pool proyek)
                  </p>
                )}
              </div>
            )}
          </FormFull>
        )}

        {/* Rule klien 2026-07-29: booking fee = Pendapatan Booking saat
            diterima — final, tidak ada refund. Checkbox refundable dihapus.
            Catatan lama yang menyebut fee sebagai "Titipan Booking (kewajiban)"
            dihapus: ia bertentangan dengan aturan yang berlaku dan membuat
            admin membaca dua perlakuan akuntansi berbeda di satu layar. */}
        <FormFull>
          <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
            Booking fee langsung diakui sebagai <strong>Pendapatan Booking</strong> saat
            diterima — bila customer batal, pendapatan tetap dan tidak ada refund.
            Unit menjadi <strong>Dibooking</strong> sampai dikonversi ke kontrak,
            dibatalkan, atau lewat masa berlaku.
          </p>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
