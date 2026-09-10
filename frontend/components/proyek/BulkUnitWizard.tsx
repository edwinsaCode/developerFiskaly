"use client";

// UAT Batch 2 §4 — Wizard pembuatan unit per BLOK (mis. Block A, unit 1–12,
// Type 36/72, harga sama) → backend membuat seluruh unit ATOMIK (duplikat =
// seluruh batch batal). Pembuatan satuan tetap via AddUnitButton.

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { Can } from "@/components/ui/Can";
import { useToast } from "@/components/ui/Toast";
import { bulkCreateUnits, fetchProductTypes, isUnitProduct, type ProductType } from "@/lib/api/projects";
import { ApiError } from "@/lib/api/client";
import type { ProjectPhase } from "@/lib/types/api";

interface Props {
  token: string;
  projectId: number;
  phases: ProjectPhase[];
}

export function BulkUnitWizard({ token, projectId, phases }: Props) {
  const { toast } = useToast();
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [block, setBlock] = useState("");
  const [unitStart, setUnitStart] = useState("1");
  const [unitEnd, setUnitEnd] = useState("");
  const [unitType, setUnitType] = useState("");
  const [typeLabel, setTypeLabel] = useState("");
  const [area, setArea] = useState("");
  const [landArea, setLandArea] = useState("");
  const [listPrice, setListPrice] = useState("");
  const [phaseId, setPhaseId] = useState("");
  const [loading, setLoading] = useState(false);
  const [productTypes, setProductTypes] = useState<ProductType[]>([]);

  useEffect(() => {
    if (!open) return;
    fetchProductTypes(token)
      // W-13: hanya produk properti yang boleh menjadi unit — kelebihan tanah
      // dijual sebagai produk tambahan pada penjualan unit, bukan sebagai unit.
      .then((pts) => setProductTypes(pts.filter(isUnitProduct)))
      .catch(() => setProductTypes([]));
  }, [open, token]);

  const start = parseInt(unitStart, 10);
  const end = parseInt(unitEnd, 10);
  const rangeOk = Number.isFinite(start) && Number.isFinite(end) && start >= 1 && end >= start && end - start + 1 <= 500;
  const priceErr = listPrice ? validateRupiah(listPrice) : "Harga wajib diisi";
  const canSubmit = block.trim() !== "" && unitType !== "" && rangeOk && !priceErr;

  // Pratinjau kode yang akan dibuat (display murni).
  const preview = useMemo(() => {
    if (!rangeOk || !block.trim()) return [];
    const codes: string[] = [];
    for (let n = start; n <= end && codes.length < 500; n++) {
      codes.push(`${block.trim()}-${String(n).padStart(2, "0")}`);
    }
    return codes;
  }, [block, start, end, rangeOk]);

  function reset() {
    setBlock(""); setUnitStart("1"); setUnitEnd(""); setUnitType("");
    setTypeLabel(""); setArea(""); setLandArea(""); setListPrice(""); setPhaseId("");
  }

  async function handleSubmit() {
    if (!canSubmit) return;
    setLoading(true);
    try {
      const res = await bulkCreateUnits(token, projectId, {
        block: block.trim(),
        unit_start: start,
        unit_end: end,
        unit_type: unitType,
        type_label: typeLabel.trim() || undefined,
        saleable_area: area || undefined,
        land_area: landArea || undefined,
        list_price: listPrice,
        phase_id: phaseId ? Number(phaseId) : undefined,
      });
      toast(`${res.created} unit blok ${block.trim()} berhasil dibuat.`, "success");
      setOpen(false);
      reset();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat unit per blok", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Can roles={["owner", "accountant"]}>
      <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>⊞ Buat per Blok</Button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Buat Unit per Blok"
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)} disabled={loading}>Batal</Button>
            <Button onClick={handleSubmit} loading={loading} disabled={!canSubmit || loading}>
              Buat {preview.length > 0 ? `${preview.length} Unit` : "Unit"}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            <Input label="Blok" value={block} onChange={(e) => setBlock(e.target.value)} placeholder="A" required />
            <Input label="Unit dari" type="number" min={1} value={unitStart} onChange={(e) => setUnitStart(e.target.value)} required />
            <Input label="Unit sampai" type="number" min={1} value={unitEnd} onChange={(e) => setUnitEnd(e.target.value)} placeholder="12" required />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Select label="Jenis Produk" value={unitType} onChange={(e) => setUnitType(e.target.value)} required>
              <option value="">— Pilih —</option>
              {productTypes.map((p) => (
                <option key={p.id} value={p.code}>
                  {p.name}{p.category === "non_property" ? " (non-properti)" : ""}
                </option>
              ))}
            </Select>
            <Input label="Tipe (label)" value={typeLabel} onChange={(e) => setTypeLabel(e.target.value)} placeholder='mis. "36/72"' />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="Luas Bangunan (m²)" type="number" step="0.01" value={area} onChange={(e) => setArea(e.target.value)} placeholder="72" />
            <Input label="Luas Tanah (m²)" type="number" step="0.01" value={landArea} onChange={(e) => setLandArea(e.target.value)} placeholder="mis. 90 (sama semua unit di blok)" />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <RupiahInput label="Harga List (sama semua)" value={listPrice} onChange={setListPrice} error={listPrice && priceErr ? priceErr : undefined} />
          </div>
          {phases.length > 0 && (
            <Select label="Fase (opsional)" value={phaseId} onChange={(e) => setPhaseId(e.target.value)}>
              <option value="">— Tanpa fase —</option>
              {phases.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </Select>
          )}
          {preview.length > 0 && (
            <div className="rounded-md border border-border bg-border-subtle/30 p-3">
              <p className="mb-1.5 text-xs font-semibold text-text-secondary">
                Pratinjau — {preview.length} unit akan dibuat:
              </p>
              <p className="text-xs text-text-tertiary break-words">
                {preview.slice(0, 24).join(", ")}{preview.length > 24 ? `, … ${preview[preview.length - 1]}` : ""}
              </p>
              <p className="mt-1.5 text-[11px] text-text-tertiary">
                Satu kode duplikat = seluruh batch dibatalkan (atomik).
              </p>
            </div>
          )}
        </div>
      </Modal>
    </Can>
  );
}
