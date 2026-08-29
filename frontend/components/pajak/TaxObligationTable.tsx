"use client";

import { useState } from "react";
import { Card } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { Rupiah } from "@/components/format/Rupiah";
import { useToast } from "@/components/ui/Toast";
import { accrueTax } from "@/lib/api/tax";
import { TaxPaymentForm } from "./TaxPaymentForm";
import { ApiError } from "@/lib/api/client";
import type { TaxReportItem, Unit } from "@/lib/types/api";

import { useMayWrite } from "@/lib/hooks/usePermissions";
import { todayLocalStr } from "@/lib/date";
interface Props {
  items: TaxReportItem[];
  soldUnits: Unit[];
  token: string;
  onRefresh: () => void;
}

const RATE_CODES = [
  { code: "pph_final_pengalihan", label: "PPh Final Pengalihan" },
];

function fmtDate(s: string) {
  return new Date(s).toLocaleDateString("id-ID", {
    day: "2-digit", month: "short", year: "numeric",
  });
}

// ── AccrueTaxModal ────────────────────────────────────────────────────────────

function AccrueTaxModal({
  open,
  onClose,
  soldUnits,
  token,
  onSuccess,
}: {
  open: boolean;
  onClose: () => void;
  soldUnits: Unit[];
  token: string;
  onSuccess: () => void;
}) {
  const { toast }   = useToast();
  const [loading, setLoading]       = useState(false);
  const [unitId, setUnitId]         = useState<string>(soldUnits[0]?.id.toString() ?? "");
  const [transferVal, setTransferVal] = useState("");
  const [accrualDate, setAccrualDate] = useState(todayLocalStr());
  const [rateCode, setRateCode]     = useState("pph_final_pengalihan");

  const amtErr  = validateRupiah(transferVal);
  const isValid = unitId && !amtErr && transferVal && accrualDate;

  async function handleSubmit() {
    if (!isValid) return;
    setLoading(true);
    try {
      const obl = await accrueTax(token, parseInt(unitId), {
        transfer_value: transferVal,
        accrual_date: new Date(accrualDate).toISOString(),
        rate_code: rateCode,
      });
      toast(
        `Akrual PPh Final Rp ${new Intl.NumberFormat("id-ID").format(parseFloat(obl.tax_amount as string))} berhasil dicatat (Jurnal #${obl.journal_entry_id})`,
        "success",
      );
      onSuccess();
      onClose();
      setTransferVal("");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat akrual pajak", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Akrual Kewajiban Pajak (Event 5a)"
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!isValid}>
            Catat Akrual
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-xs text-text-tertiary bg-border-subtle rounded p-2">
          Jurnal Event 5a: Dr 5-2000 Beban PPh Final / Cr 2-4000 Hutang PPh Final.
          Tarif diambil dari konfigurasi aktif pada tanggal akrual.
        </p>

        <Select
          label="Unit"
          value={unitId}
          onChange={(e) => setUnitId(e.target.value)}
          required
        >
          {soldUnits.length === 0 && (
            <option value="">Tidak ada unit terjual</option>
          )}
          {soldUnits.map((u) => (
            <option key={u.id} value={u.id}>
              {u.code}{u.buyer_ref ? ` — ${u.buyer_ref}` : ""}
            </option>
          ))}
        </Select>

        <RupiahInput
          label="Nilai Pengalihan (Transfer Value)"
          value={transferVal}
          onChange={setTransferVal}
          required
          hint="Biasanya = harga jual (sale_price) dari BAST"
          error={transferVal && amtErr ? amtErr : undefined}
        />

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Input
            label="Tanggal Akrual"
            type="date"
            value={accrualDate}
            onChange={(e) => setAccrualDate(e.target.value)}
            required
          />
          <Select
            label="Kode Pajak"
            value={rateCode}
            onChange={(e) => setRateCode(e.target.value)}
          >
            {RATE_CODES.map((r) => (
              <option key={r.code} value={r.code}>{r.label}</option>
            ))}
          </Select>
        </div>
      </div>
    </Modal>
  );
}

// ── TaxObligationTable ────────────────────────────────────────────────────────

export function TaxObligationTable({ items, soldUnits, token, onRefresh }: Props) {
  const mayWrite = useMayWrite();
  const [showAccrue,  setShowAccrue]  = useState(false);
  const [payTarget,   setPayTarget]   = useState<TaxReportItem | null>(null);

  const outstanding = items.filter((i) => i.status === "outstanding");
  const paid        = items.filter((i) => i.status === "paid");

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center justify-between">
        <p className="text-sm text-text-secondary">
          {items.length} kewajiban —{" "}
          <span className="text-danger font-medium">{outstanding.length} outstanding</span>
          {paid.length > 0 && (
            <>, <span className="text-success font-medium">{paid.length} lunas</span></>
          )}
        </p>
        {mayWrite && (
          <Button size="sm" onClick={() => setShowAccrue(true)}>
            + Akrual Baru
          </Button>
        )}
      </div>

      {/* Table */}
      {items.length === 0 ? (
        <Card padding="sm">
          <p className="text-sm text-text-secondary text-center py-6">
            Tidak ada kewajiban pajak dalam periode ini.
            Klik <strong>+ Akrual Baru</strong> untuk mencatat kewajiban PPh Final setelah BAST.
          </p>
        </Card>
      ) : (
        <Card padding="none">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-xs text-text-secondary uppercase tracking-wide">
                  <th className="text-left px-4 py-3 font-medium">Kewajiban</th>
                  <th className="text-left px-4 py-3 font-medium">Unit</th>
                  <th className="text-right px-4 py-3 font-medium">Transfer Value</th>
                  <th className="text-right px-4 py-3 font-medium">Tarif</th>
                  <th className="text-right px-4 py-3 font-medium">PPh Final</th>
                  <th className="text-left px-4 py-3 font-medium">Tgl Akrual</th>
                  <th className="text-left px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3" />
                </tr>
              </thead>
              <tbody>
                {items.map((item) => {
                  const ratePercent = (parseFloat(item.rate) * 100).toFixed(2);
                  const isOutstanding = item.status === "outstanding";
                  return (
                    <tr
                      key={item.obligation_id}
                      className="border-b border-border last:border-0 hover:bg-bg transition-colors"
                    >
                      <td className="px-4 py-3 font-mono text-xs text-text-secondary">
                        #{item.obligation_id}
                      </td>
                      <td className="px-4 py-3">
                        {item.unit_id ? (
                          <span className="font-medium">Unit #{item.unit_id}</span>
                        ) : (
                          <span className="text-text-tertiary">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-right">
                        <Rupiah value={item.transfer_value} colorSign={false} />
                      </td>
                      <td className="px-4 py-3 text-right text-text-secondary">
                        {ratePercent}%
                      </td>
                      <td className="px-4 py-3 text-right font-semibold">
                        <Rupiah value={item.tax_amount} colorSign={false} />
                      </td>
                      <td className="px-4 py-3 text-text-secondary text-xs">
                        {fmtDate(item.accrual_date)}
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant={isOutstanding ? "danger" : "success"}>
                          {isOutstanding ? "Outstanding" : "Lunas"}
                        </Badge>
                      </td>
                      <td className="px-4 py-3 text-right">
                        {isOutstanding && mayWrite && (
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => setPayTarget(item)}
                          >
                            Bayar
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {/* Modals */}
      <AccrueTaxModal
        open={showAccrue}
        onClose={() => setShowAccrue(false)}
        soldUnits={soldUnits}
        token={token}
        onSuccess={onRefresh}
      />
      <TaxPaymentForm
        open={payTarget !== null}
        onClose={() => setPayTarget(null)}
        obligation={payTarget}
        token={token}
        onSuccess={onRefresh}
      />
    </div>
  );
}
