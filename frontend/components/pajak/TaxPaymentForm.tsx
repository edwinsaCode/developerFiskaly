"use client";

import { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { Rupiah } from "@/components/format/Rupiah";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import { payTax } from "@/lib/api/tax";
import { ApiError } from "@/lib/api/client";
import type { TaxReportItem } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

interface Props {
  open: boolean;
  onClose: () => void;
  obligation: TaxReportItem | null;
  token: string;
  onSuccess: () => void;
}

export function TaxPaymentForm({ open, onClose, obligation, token, onSuccess }: Props) {
  const { toast } = useToast();
  const [loading, setLoading]       = useState(false);
  const [bankCode, setBankCode]     = useState("");
  const [payDate, setPayDate]       = useState(todayLocalStr());

  if (!obligation) return null;

  async function handleSubmit() {
    if (!payDate || !bankCode) return;
    setLoading(true);
    try {
      const payment = await payTax(token, obligation!.obligation_id, {
        bank_account_code: bankCode,
        payment_date: new Date(payDate).toISOString(),
      });
      toast(
        `PPh Final Rp ${new Intl.NumberFormat("id-ID").format(parseFloat(obligation!.tax_amount))} berhasil dibayar (Journal #${payment.journal_entry_id})`,
        "success",
      );
      onSuccess();
      onClose();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membayar pajak", "error");
    } finally {
      setLoading(false);
    }
  }

  const ratePercent = (parseFloat(obligation.rate) * 100).toFixed(2);

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Bayar Kewajiban Pajak (Event 5b)"
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!payDate || !bankCode}>
            Bayar Pajak
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {/* Ringkasan obligation */}
        <div className="bg-bg rounded-lg border border-border p-4 space-y-1.5 text-sm">
          <div className="flex justify-between">
            <span className="text-text-secondary">Kewajiban #</span>
            <span className="font-medium">{obligation.obligation_id}</span>
          </div>
          {obligation.unit_id && (
            <div className="flex justify-between">
              <span className="text-text-secondary">Unit ID</span>
              <span className="font-medium">{obligation.unit_id}</span>
            </div>
          )}
          <div className="flex justify-between">
            <span className="text-text-secondary">Transfer Value</span>
            <span className="font-medium">
              <Rupiah value={obligation.transfer_value} colorSign={false} />
            </span>
          </div>
          <div className="flex justify-between">
            <span className="text-text-secondary">Tarif PPh Final</span>
            <span className="font-medium">{ratePercent}%</span>
          </div>
          <div className="flex justify-between border-t border-border pt-1.5 mt-1.5">
            <span className="text-text-secondary font-medium">Jumlah Dibayar</span>
            <span className="font-bold text-danger">
              <Rupiah value={obligation.tax_amount} colorSign={false} />
            </span>
          </div>
        </div>

        <FormGrid>
          <CashBankSelect token={token} value={bankCode} onChange={setBankCode} label="Rekening Bank Pembayaran" required />
          <Input
            label="Tanggal Pembayaran"
            type="date"
            value={payDate}
            onChange={(e) => setPayDate(e.target.value)}
            required
          />
        </FormGrid>

        <p className="text-xs text-text-secondary bg-border-subtle/60 border border-border-subtle rounded-lg px-3 py-2">
          Jurnal Event 5b: Dr 2-4000 Hutang PPh Final / Cr Bank terpilih.
          Setelah dibayar, status kewajiban berubah ke <strong>paid</strong> dan tidak dapat diubah.
        </p>
      </div>
    </Modal>
  );
}
