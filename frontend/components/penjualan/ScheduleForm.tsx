"use client";

import { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { useToast } from "@/components/ui/Toast";
import { createPaymentSchedule, fetchSchedulePlan } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import type { PaymentSchedule } from "@/lib/types/api";

interface Props {
  open: boolean;
  onClose: () => void;
  contractId: number;
  token: string;
  onSuccess?: (schedules: PaymentSchedule[]) => void;
}

interface ScheduleRow {
  installment_number: number;
  due_date: string;
  amount: string;
  type: "dp" | "installment" | "final";
}

function emptyRow(n: number): ScheduleRow {
  return { installment_number: n, due_date: "", amount: "", type: "installment" };
}

export function ScheduleForm({ open, onClose, contractId, token, onSuccess }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);
  const [planning, setPlanning] = useState(false);
  const [rows, setRows]       = useState<ScheduleRow[]>([emptyRow(1)]);

  // R6/U1 — isi otomatis dari skema pembayaran kontrak (DP%, tenor, jatuh tempo
  // dihitung engine). Staf tinggal koreksi tanggal bila perlu, bukan mengetik semua.
  async function fillFromScheme() {
    setPlanning(true);
    try {
      const items = await fetchSchedulePlan(token, contractId);
      if (items.length === 0) {
        toast("Skema kontrak ini tidak menghasilkan jadwal otomatis", "error");
        return;
      }
      setRows(items.map((it) => ({
        installment_number: it.installment_number,
        due_date: it.due_date.slice(0, 10),
        amount: it.amount,
        type: it.type,
      })));
      toast(`${items.length} baris terisi dari skema — periksa lalu simpan`, "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memuat rencana dari skema", "error");
    } finally {
      setPlanning(false);
    }
  }

  function updateRow(idx: number, field: keyof ScheduleRow, value: string) {
    setRows((prev) => prev.map((r, i) => i === idx ? { ...r, [field]: value } : r));
  }

  function addRow() {
    setRows((prev) => [...prev, emptyRow(prev.length + 1)]);
  }

  function removeRow(idx: number) {
    setRows((prev) =>
      prev
        .filter((_, i) => i !== idx)
        .map((r, i) => ({ ...r, installment_number: i + 1 })),
    );
  }

  const rowErrors = rows.map((r) => ({
    amount: validateRupiah(r.amount),
    date: !r.due_date ? "Tanggal wajib diisi" : null,
  }));
  const isValid = rowErrors.every((e) => !e.amount && !e.date);

  async function handleSubmit() {
    if (!isValid || rows.length === 0) return;
    setLoading(true);
    try {
      const schedules = await createPaymentSchedule(
        token,
        contractId,
        rows.map((r) => ({
          installment_number: r.installment_number,
          due_date: new Date(r.due_date).toISOString(),
          amount: r.amount,
          type: r.type,
        })),
      );
      toast(`${schedules.length} jadwal cicilan berhasil dibuat`, "success");
      onSuccess?.(schedules);
      onClose();
      setRows([emptyRow(1)]);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat jadwal", "error");
    } finally {
      setLoading(false);
    }
  }

  const typeLabels: Record<string, string> = {
    dp: "DP",
    installment: "Cicilan",
    final: "Pelunasan",
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Tambah Jadwal Cicilan"
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!isValid || rows.length === 0}>
            Simpan Jadwal
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5">
          <p className="text-xs text-text-secondary">
            Kontrak <strong className="text-text-primary">#{contractId}</strong> — susun termin
            pembayaran. Jadwal inilah dasar tagihan &amp; umur piutang.
          </p>
          <Button variant="secondary" size="sm" onClick={fillFromScheme} loading={planning}>
            ⚡ Isi Otomatis dari Skema
          </Button>
        </div>

        {rows.map((row, idx) => (
          <div key={idx} className="rounded-xl border border-border bg-surface p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold uppercase tracking-wide text-text-secondary">
                Cicilan #{row.installment_number}
              </span>
              {rows.length > 1 && (
                <button
                  onClick={() => removeRow(idx)}
                  className="text-xs text-danger hover:underline"
                >
                  Hapus
                </button>
              )}
            </div>
            <div className="grid grid-cols-1 gap-x-5 gap-y-4 sm:grid-cols-3">
              <Select
                label="Jenis"
                value={row.type}
                onChange={(e) => updateRow(idx, "type", e.target.value)}
              >
                {Object.entries(typeLabels).map(([v, l]) => (
                  <option key={v} value={v}>{l}</option>
                ))}
              </Select>
              {/* Tanggal kosong TIDAK dicat merah: wajib-nya sudah disampaikan
                  asterisk merah + tombol Simpan yang nonaktif. Merah disimpan
                  untuk isian yang benar-benar salah. */}
              <Input
                label="Jatuh Tempo"
                type="date"
                value={row.due_date}
                onChange={(e) => updateRow(idx, "due_date", e.target.value)}
                required
              />
              <RupiahInput
                label="Jumlah"
                value={row.amount}
                onChange={(v) => updateRow(idx, "amount", v)}
                required
                error={rowErrors[idx].amount ?? undefined}
              />
            </div>
          </div>
        ))}

        <Button variant="ghost" size="sm" onClick={addRow}>
          + Tambah Baris
        </Button>
      </div>
    </Modal>
  );
}
