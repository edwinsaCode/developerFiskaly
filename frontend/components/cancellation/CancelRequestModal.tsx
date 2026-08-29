"use client";

// Increment 8 — Wizard langkah 1: pengajuan pembatalan (pra/pasca-BAST
// dideteksi otomatis oleh backend).

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { requestCancellation } from "@/lib/api/cancellation";
import { todayLocalStr } from "@/lib/date";

interface Props {
  open: boolean;
  onClose: () => void;
  token: string;
  unitId: number;
  unitCode: string;
  postBAST: boolean;
}

export function CancelRequestModal({ open, onClose, token, unitId, unitCode, postBAST }: Props) {
  const router = useRouter();
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  const [reason, setReason] = useState("");
  const [penalty, setPenalty] = useState("");
  const [eventDate, setEventDate] = useState(todayLocalStr());

  const reasonErr = reason.trim().length === 0 ? "Alasan wajib diisi" : null;

  async function handleSubmit() {
    if (reasonErr) return;
    setLoading(true);
    try {
      const c = await requestCancellation(token, unitId, {
        reason: reason.trim(),
        penalty: penalty || undefined,
        event_date: eventDate,
      });
      toast(`Pembatalan #${c.id} diajukan — tinjau dampak lalu setujui & proses`, "success");
      onClose();
      router.push(`/penjualan/pembatalan?open=${c.id}`);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengajukan pembatalan", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Batalkan ${postBAST ? "Penjualan" : "Pemesanan"} — ${unitCode}`}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Tutup</Button>
          <Button variant="danger" onClick={handleSubmit} loading={loading} disabled={!!reasonErr}>
            Ajukan Pembatalan
          </Button>
        </>
      }
    >
      <FormGrid>
        {postBAST && (
          <FormFull>
            <p className="rounded-lg border border-warning/30 bg-warning-bg px-3.5 py-2.5 text-sm text-text-primary">
              Unit ini sudah <strong>BAST</strong>. Pembatalan akan <strong>membalik
              pendapatan, HPP, dan PPh Final</strong> secara otomatis (jurnal pembalik),
              lalu unit kembali ke stok pada nilai biaya aktual.
            </p>
          </FormFull>
        )}

        <FormFull>
          <Input
            label="Alasan" required
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="cth: buyer wanprestasi / mengundurkan diri"
          />
        </FormFull>

        <RupiahInput
          label="Penalti (hangus)"
          value={penalty}
          onChange={setPenalty}
          hint="Dipotong dari dana buyer; diakui sebagai Pendapatan Lain-lain. Kosongkan bila tanpa penalti."
          placeholder="0"
        />
        <Input
          label="Tanggal Kejadian"
          type="date"
          value={eventDate}
          onChange={(e) => setEventDate(e.target.value)}
        />

        <FormFull>
          <p className="rounded-lg border border-border-subtle bg-border-subtle/60 px-3.5 py-2.5 text-xs text-text-secondary">
            Setelah diajukan Anda akan melihat <strong>pratinjau jurnal lengkap</strong>{" "}
            (dampak L/R, HPP, dana buyer) sebelum menyetujui dan memproses.
          </p>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
