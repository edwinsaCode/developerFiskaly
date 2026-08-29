"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { Can } from "@/components/ui/Can";
import { useToast } from "@/components/ui/Toast";
import { createProject } from "@/lib/api/projects";
import { ApiError } from "@/lib/api/client";

interface Props {
  token: string;
}

export function NewProjectButton({ token }: Props) {
  const { toast } = useToast();
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [landArea, setLandArea] = useState("");
  const [startDate, setStartDate] = useState("");
  const [notes, setNotes] = useState("");
  const [loading, setLoading] = useState(false);

  const nameErr = !name.trim() ? "Nama proyek wajib diisi" : "";
  const areaErr = landArea && isNaN(Number(landArea)) ? "Luas harus angka" : "";
  const canSubmit = !nameErr && !areaErr;

  function reset() {
    setName("");
    setLandArea("");
    setStartDate("");
    setNotes("");
  }

  async function handleSubmit() {
    if (!canSubmit) return;
    setLoading(true);
    try {
      const p = await createProject(token, {
        name: name.trim(),
        land_area: landArea || undefined,
        notes: notes.trim() || undefined,
        start_date: startDate || undefined,
      });
      toast(`Proyek "${p.name}" dibuat.`, "success");
      setOpen(false);
      reset();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat proyek", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Can roles={["owner", "accountant"]}>
      <Button onClick={() => setOpen(true)}>+ Tambah Proyek</Button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Tambah Proyek Baru"
        description="Proyek adalah wadah biaya dan unit. Luas lahan dan tanggal mulai boleh diisi belakangan."
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={loading}>Batal</Button>
            <Button onClick={handleSubmit} loading={loading} disabled={!canSubmit || loading}>Simpan Proyek</Button>
          </>
        }
      >
        <FormGrid>
          <FormFull>
            <Input
              label="Nama Proyek"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="mis. LITHOS Villas"
              required
              error={name && nameErr ? nameErr : undefined}
            />
          </FormFull>
          <Input
            label="Luas Lahan (m²)"
            type="number"
            step="0.01"
            value={landArea}
            onChange={(e) => setLandArea(e.target.value)}
            placeholder="opsional"
            suffix="m²"
            error={areaErr || undefined}
          />
          <Input
            label="Tanggal Mulai"
            type="date"
            value={startDate}
            onChange={(e) => setStartDate(e.target.value)}
          />
          <FormFull>
            <Input
              label="Catatan"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder="opsional"
            />
          </FormFull>
        </FormGrid>
      </Modal>
    </Can>
  );
}
