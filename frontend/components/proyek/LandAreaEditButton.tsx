"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { Can } from "@/components/ui/Can";
import { useToast } from "@/components/ui/Toast";
import { updateUnitLandArea } from "@/lib/api/projects";
import { ApiError } from "@/lib/api/client";

interface Props {
  token: string;
  unitId: number;
  currentLandArea: string;
}

// LT-2 (kelebihan-tanah-final-architecture §B.5): koreksi eksplisit-admin luas
// tanah unit. Terpisah dari AddUnitButton — bisa dipakai kapan saja setelah
// unit dibuat, tidak hanya saat pembuatan.
export function LandAreaEditButton({ token, unitId, currentLandArea }: Props) {
  const { toast } = useToast();
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(currentLandArea);
  const [loading, setLoading] = useState(false);

  const err = value !== "" && isNaN(Number(value)) ? "Luas harus angka" : "";
  const negErr = value !== "" && !err && Number(value) < 0 ? "Luas tidak boleh negatif" : "";
  const canSubmit = !err && !negErr && value !== "";

  async function handleSubmit() {
    if (!canSubmit) return;
    setLoading(true);
    try {
      await updateUnitLandArea(token, unitId, value);
      toast("Luas tanah diperbarui.", "success");
      setOpen(false);
      router.refresh();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal memperbarui luas tanah", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Can roles={["owner", "accountant"]}>
      <button
        type="button"
        onClick={() => { setValue(currentLandArea); setOpen(true); }}
        className="text-[11px] text-text-tertiary hover:text-text-secondary underline decoration-dotted"
      >
        Ubah
      </button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Ubah Luas Tanah"
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={loading}>Batal</Button>
            <Button onClick={handleSubmit} loading={loading} disabled={!canSubmit || loading}>Simpan</Button>
          </>
        }
      >
        <FormGrid>
          <Input
            label="Luas Tanah (m²)"
            type="number"
            step="0.01"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            suffix="m²"
            error={err || negErr || undefined}
            autoFocus
          />
        </FormGrid>
      </Modal>
    </Can>
  );
}
