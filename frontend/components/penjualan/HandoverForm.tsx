"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";
import { recordPhysicalHandover } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import { todayLocalStr } from "@/lib/date";

interface Props {
  open: boolean;
  onClose: () => void;
  unitId: number;
  token: string;
  onSuccess?: () => void;
}

// Temuan #7: serah terima fisik — murni pencatatan, tanpa gate finansial dan
// tanpa jurnal. Terpisah dari Akad (yang sudah mengakui pendapatan+HPP).
export function HandoverForm({ open, onClose, unitId, token, onSuccess }: Props) {
  const router = useRouter();
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);
  const [handoverDate, setHandoverDate] = useState(todayLocalStr());

  async function handleSubmit() {
    setLoading(true);
    try {
      await recordPhysicalHandover(token, unitId, {
        handover_date: new Date(handoverDate).toISOString(),
      });
      toast("Serah terima fisik dicatat.", "success");
      onSuccess?.();
      onClose();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mencatat serah terima fisik", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Catat Serah Terima Fisik"
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading}>
            Catat Serah Terima
          </Button>
        </>
      }
    >
      <FormGrid>
        <FormFull>
          <div className="bg-border-subtle/60 border border-border-subtle rounded-lg px-3 py-2.5 text-xs text-text-secondary">
            Tidak membuat jurnal — murni pencatatan fisik bahwa unit sudah diserahkan ke
            pembeli. Pendapatan dan HPP sudah diakui sebelumnya saat Akad.
          </div>
        </FormFull>

        <FormFull>
          <Input
            label="Tanggal Serah Terima"
            type="date"
            value={handoverDate}
            onChange={(e) => setHandoverDate(e.target.value)}
            required
          />
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
