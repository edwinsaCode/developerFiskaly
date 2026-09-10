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

function plusDays(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return dateToLocalStr(d);
}

export function BookingFormModal({ open, onClose, token, unitId, unitCode, onSuccess }: Props) {
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
  // Client final note 2026-09-10: booking fee boleh Rp0 (murni reservasi unit,
  // tanpa uang diterima) — allowZero=true melonggarkan validasi dari >0 ke >=0.
  const feeErr = validateRupiah(fee, true);
  const feeZero = fee !== "" && parseInt(fee, 10) === 0;
  const [refundable, setRefundable] = useState(false);
  const [bank, setBank] = useState("");
  const [bookingDate, setBookingDate] = useState(todayLocalStr());
  const [expiryDate, setExpiryDate] = useState(plusDays(14));
  const [notes, setNotes] = useState("");

  useEffect(() => {
    if (!open) return;
    fetchCustomers(token).then(setCustomers).catch(() => {});
    fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
  }, [open, token]);

  const custErr = !newCustomer && !customerId ? "Pilih customer" : null;
  const newCustErr = newCustomer && newName.trim().length === 0 ? "Nama customer wajib" : null;
  const expiryErr = expiryDate <= bookingDate ? "Berlaku s/d harus setelah tanggal booking" : null;

  function isValid() {
    // Fee Rp0: tidak ada uang diterima → sumber dana (bank) tidak relevan dan
    // tidak wajib diisi (backend juga tidak memvalidasi/memakainya di jalur ini).
    return !custErr && !newCustErr && !feeErr && (feeZero || !!bank) && !expiryErr;
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
        // Fee Rp0: disposisi backend SELALU recognized (tidak ada apa pun
        // untuk dipegang/direfund) — kirim apa adanya, backend yang menentukan.
        refundable,
        bank_account_code: feeZero ? "" : bank,
        booking_date: bookingDate,
        expiry_date: expiryDate,
        notes: notes || undefined,
      });
      // Nomor kwitansi disebut apa adanya — "terbit otomatis" tanpa nomor
      // tidak bisa dicek siapa pun.
      if (feeZero) {
        toast(`Booking #${b.id} dibuat — Rp0, tanpa jurnal atau kwitansi`, "success");
      } else {
        const creditLabel = refundable ? "Titipan Booking (refundable)" : "Pendapatan Booking";
        toast(
          b.receipt_number
            ? `Booking #${b.id} dibuat — kwitansi ${b.receipt_number} terbit, fee dicatat sebagai ${creditLabel}`
            : `Booking #${b.id} dibuat — fee dicatat sebagai ${creditLabel}, kwitansi terbit otomatis`,
          "success",
        );
      }
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
          hint="Boleh Rp0 — murni reservasi unit tanpa uang diterima."
          placeholder="5.000.000"
        />

        {!feeZero && (
          <CashBankSelect token={token} value={bank} onChange={setBank} label="Diterima di" required />
        )}

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

        {/* Item 3 (2026-09), keputusan klien: refund hanya utk booking yang
            ditandai Refundable SAAT DIBUAT. Rule klien 2026-07-29 (fee =
            Pendapatan final, tanpa refund) tetap default/mayoritas —
            checkbox ini hanya membuka jalur legacy held→refund yang sudah
            ada, tidak mengubah default. Client final note 2026-09-10: fee
            Rp0 tidak ada apa pun untuk dipegang/direfund — checkbox & jalur
            held disembunyikan, disposisi backend SELALU recognized. */}
        {!feeZero && (
          <FormFull>
            <label className="flex items-start gap-2 text-sm text-text-primary">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={refundable}
                onChange={(e) => setRefundable(e.target.checked)}
              />
              <span>
                Refundable — fee bisa dikembalikan bila booking dibatalkan
              </span>
            </label>
          </FormFull>
        )}

        <FormFull>
          {feeZero ? (
            <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
              Booking fee <strong>Rp0</strong> — tidak ada uang diterima, jadi tidak ada
              jurnal kas maupun kwitansi yang dibuat. Ini murni reservasi unit; harga
              jual tetap nilai kontrak normal. Unit menjadi <strong>Dibooking</strong>{" "}
              sampai dikonversi ke kontrak, dibatalkan, atau lewat masa berlaku.
            </p>
          ) : refundable ? (
            <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
              Fee dicatat sebagai <strong>Titipan Booking</strong> (kewajiban) — belum
              diakui pendapatan. Bila customer batal, fee dapat diproses refund lewat
              menu Refund. Unit menjadi <strong>Dibooking</strong> sampai dikonversi
              ke kontrak, dibatalkan, atau lewat masa berlaku.
            </p>
          ) : (
            <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
              Booking fee langsung diakui sebagai <strong>Pendapatan Booking</strong> saat
              diterima — bila customer batal, pendapatan tetap dan tidak ada refund.
              Unit menjadi <strong>Dibooking</strong> sampai dikonversi ke kontrak,
              dibatalkan, atau lewat masa berlaku.
            </p>
          )}
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
