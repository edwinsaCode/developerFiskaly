"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { Can } from "@/components/ui/Can";
import { useToast } from "@/components/ui/Toast";
import { createUnit, fetchProductTypes, isUnitProduct, type ProductType } from "@/lib/api/projects";
import { ApiError } from "@/lib/api/client";
import type { ProjectPhase } from "@/lib/types/api";

interface Props {
  token: string;
  projectId: number;
  phases: ProjectPhase[];
}

export function AddUnitButton({ token, projectId, phases }: Props) {
  const { toast } = useToast();
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [code, setCode] = useState("");
  const [unitType, setUnitType] = useState("");
  const [typeLabel, setTypeLabel] = useState("");
  const [area, setArea] = useState("");
  const [landArea, setLandArea] = useState("");
  const [listPrice, setListPrice] = useState("");
  const [phaseId, setPhaseId] = useState("");
  const [loading, setLoading] = useState(false);
  // UAT Batch 2 §2: tipe unit dari master Product Catalog (bukan teks bebas).
  const [productTypes, setProductTypes] = useState<ProductType[]>([]);
  useEffect(() => {
    if (!open) return;
    fetchProductTypes(token)
      // W-13: hanya produk properti yang boleh menjadi unit — kelebihan tanah
      // dijual sebagai produk tambahan pada penjualan unit, bukan sebagai unit.
      .then((pts) => setProductTypes(pts.filter(isUnitProduct)))
      .catch(() => setProductTypes([]));
  }, [open, token]);

  const selectedType = productTypes.find((p) => p.code === unitType);

  const codeErr = !code.trim() ? "Kode unit wajib diisi" : "";
  const typeErr = !unitType.trim() ? "Tipe unit wajib diisi" : "";
  const priceErr = listPrice ? validateRupiah(listPrice) : "";
  const areaErr = area && isNaN(Number(area)) ? "Luas harus angka" : "";
  const landAreaErr = landArea && isNaN(Number(landArea)) ? "Luas harus angka" : "";
  const canSubmit = !codeErr && !typeErr && !priceErr && !areaErr && !landAreaErr;

  function reset() {
    setCode(""); setUnitType(""); setTypeLabel(""); setArea(""); setLandArea(""); setListPrice(""); setPhaseId("");
  }

  async function handleSubmit() {
    if (!canSubmit) return;
    setLoading(true);
    try {
      const u = await createUnit(token, projectId, {
        code: code.trim(),
        unit_type: unitType.trim(),
        type_label: typeLabel.trim() || undefined,
        saleable_area: area || undefined,
        land_area: landArea || undefined,
        list_price: listPrice || undefined,
        phase_id: phaseId ? Number(phaseId) : undefined,
      });
      toast(`Unit "${u.code}" ditambahkan.`, "success");
      setOpen(false);
      reset();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menambah unit", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Can roles={["owner", "accountant"]}>
      <Button size="sm" onClick={() => setOpen(true)}>+ Tambah Unit</Button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Tambah Unit"
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={loading}>Batal</Button>
            <Button onClick={handleSubmit} loading={loading} disabled={!canSubmit || loading}>Simpan Unit</Button>
          </>
        }
      >
        <FormGrid>
          <Input label="Kode Unit" value={code} onChange={(e) => setCode(e.target.value)} placeholder="mis. A-01" required error={code && codeErr ? codeErr : undefined} />
          <Select label="Jenis Produk" value={unitType} onChange={(e) => setUnitType(e.target.value)} required>
            <option value="">— Pilih jenis produk —</option>
            {productTypes.map((p) => (
              <option key={p.id} value={p.code}>
                {p.name}{p.category === "non_property" ? " (non-properti)" : ""}
              </option>
            ))}
          </Select>
          {productTypes.length === 0 ? (
            <FormFull>
              <p className="-mt-1 text-[11px] text-text-tertiary">
                Belum ada jenis produk aktif. Tambahkan dulu di <strong>Pengaturan → Katalog Produk</strong>
                {" "}— jenis produk menentukan akun pendapatan dan apakah unit ikut perhitungan HPP.
              </p>
            </FormFull>
          ) : selectedType?.category === "non_property" ? (
            <FormFull>
              <p className="-mt-1 text-[11px] text-text-tertiary">
                Produk non-properti: unit ini <strong>tidak ikut alokasi biaya proyek (HPP)</strong> dan
                tidak boleh dibebani biaya langsung. Pendapatannya diakui ke akun{" "}
                <span className="font-mono">{selectedType.revenue_account_code}</span>.
              </p>
            </FormFull>
          ) : null}
          <Input label="Tipe (label)" value={typeLabel} onChange={(e) => setTypeLabel(e.target.value)} placeholder='mis. "36/72" (opsional)' />
          <Input label="Luas Bangunan (m²)" type="number" step="0.01" value={area} onChange={(e) => setArea(e.target.value)} placeholder="opsional" suffix="m²" error={areaErr || undefined} />
          <Input label="Luas Tanah (m²)" type="number" step="0.01" value={landArea} onChange={(e) => setLandArea(e.target.value)} placeholder="opsional, bisa diisi belakangan" suffix="m²" error={landAreaErr || undefined} />
          <RupiahInput label="Harga List" value={listPrice} onChange={setListPrice} error={listPrice && priceErr ? priceErr : undefined} hint="opsional" />
          {phases.length > 0 && (
            <Select label="Fase (opsional)" value={phaseId} onChange={(e) => setPhaseId(e.target.value)}>
              <option value="">— Tanpa fase —</option>
              {phases.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </Select>
          )}
        </FormGrid>
      </Modal>
    </Can>
  );
}
