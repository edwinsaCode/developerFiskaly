"use client";

import { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import { recordTermin } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import type { TerminPayment, TerminKind } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

interface Props {
  open: boolean;
  onClose: () => void;
  unitId: number;
  token: string;
  onSuccess?: (termin: TerminPayment) => void;
  label?: string;
}

// W-13: opsi terbatas — tanpa kontrak, "Pencairan Dana Bank" tidak berlaku
// (pencairan KPR selalu butuh kontrak, lihat RecordPaymentButton).
const KIND_OPTIONS: { value: TerminKind; label: string }[] = [
  { value: "dp", label: "DP (Uang Muka)" },
  { value: "installment", label: "Cicilan" },
  { value: "final_payment", label: "Pelunasan" },
  { value: "other", label: "Lainnya" },
];

export function TerminForm({ open, onClose, unitId, token, onSuccess, label = "Catat Penerimaan" }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  const [bankCode, setBankCode]     = useState("");
  const [amount, setAmount]         = useState("");
  const [date, setDate]             = useState(todayLocalStr());
  const [description, setDescription] = useState("");
  const [kind, setKind]             = useState<TerminKind>("other");
  const [installmentNo, setInstallmentNo] = useState("");

  const amountErr = validateRupiah(amount);
  const isValid   = !amountErr && date && description.trim() && bankCode;

  async function handleSubmit() {
    if (!isValid) return;
    setLoading(true);
    try {
      const termin = await recordTermin(token, unitId, {
        bank_account_code: bankCode,
        amount,
        date: new Date(date).toISOString(),
        description: description.trim(),
        kind,
        installment_no: kind === "installment" && installmentNo ? Number(installmentNo) : undefined,
      });
      toast("Penerimaan berhasil dicatat. Jurnal sudah diposting.", "success");
      onSuccess?.(termin);
      onClose();
      setAmount("");
      setDescription("");
      setKind("other");
      setInstallmentNo("");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mencatat penerimaan", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={label}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!isValid}>
            Simpan
          </Button>
        </>
      }
    >
      <FormGrid>
        <Select
          label="Jenis Penerimaan"
          value={kind}
          onChange={(e) => setKind(e.target.value as TerminKind)}
          required
        >
          {KIND_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </Select>
        {kind === "installment" ? (
          <Input
            label="Cicilan ke-"
            type="number"
            min={1}
            value={installmentNo}
            onChange={(e) => setInstallmentNo(e.target.value)}
            placeholder="opsional"
          />
        ) : (
          <div />
        )}
        <FormFull>
          <CashBankSelect token={token} value={bankCode} onChange={setBankCode} label="Rekening / Kas Tujuan" required />
        </FormFull>
        <RupiahInput
          label="Jumlah Diterima"
          value={amount}
          onChange={setAmount}
          required
          error={amount && amountErr ? amountErr : undefined}
          hint="Harus rupiah bulat tanpa sen"
        />
        <Input
          label="Tanggal Terima"
          type="date"
          value={date}
          onChange={(e) => setDate(e.target.value)}
          required
        />
        <FormFull>
          <Input
            label="Keterangan"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="cth: DP pertama unit A01"
            required
          />
        </FormFull>
        <FormFull>
          <p className="text-xs text-text-secondary bg-border-subtle/60 border border-border-subtle rounded-lg px-3 py-2">
            Jurnal: Dr Bank / Cr Uang Muka Penjualan (kewajiban — Invariant #7)
          </p>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
