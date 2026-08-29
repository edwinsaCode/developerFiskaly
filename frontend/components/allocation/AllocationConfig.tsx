"use client";

import { useState, useEffect } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { useToast } from "@/components/ui/Toast";
import { fetchAllocationConfig, setAllocationConfig, executeAllocation } from "@/lib/api/allocation";
import { ApiError } from "@/lib/api/client";
import type { AllocationConfig as AllocationConfigType, AllocationBasis } from "@/lib/types/api";

interface Props {
  token: string;
  projectId: number;
  onConfigSaved: (cfg: AllocationConfigType) => void;
  onExecuted?: () => void;
}

const BASIS_OPTIONS: { value: AllocationBasis; label: string; desc: string }[] = [
  {
    value: "saleable_area",
    label: "Luas Area (m²)",
    desc: "Biaya proyek dialokasikan proporsional terhadap saleable area masing-masing unit.",
  },
  {
    value: "sales_value",
    label: "Nilai Jual (List Price)",
    desc: "Biaya proyek dialokasikan proporsional terhadap harga jual (list price) masing-masing unit.",
  },
];

const BASIS_LABEL: Record<AllocationBasis, string> = {
  saleable_area: "Luas Area",
  sales_value:   "Nilai Jual",
};

export function AllocationConfig({ token, projectId, onConfigSaved, onExecuted }: Props) {
  const { toast } = useToast();
  const [config, setConfig]           = useState<AllocationConfigType | null>(null);
  const [selectedBasis, setSelected]  = useState<AllocationBasis>("saleable_area");
  const [loading, setLoading]         = useState(true);
  const [saving, setSaving]           = useState(false);
  const [loadError, setLoadError]     = useState<string | null>(null);

  useEffect(() => {
    loadConfig();
  }, [projectId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function loadConfig() {
    setLoading(true);
    setLoadError(null);
    try {
      const cfg = await fetchAllocationConfig(token, projectId);
      setConfig(cfg);
      if (cfg) setSelected(cfg.basis);
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : "Gagal memuat konfigurasi alokasi");
    } finally {
      setLoading(false);
    }
  }

  async function runExecute() {
    try {
      await executeAllocation(token, projectId);
      onExecuted?.();
    } catch {
      // non-fatal: compute preview still works; history refresh is best-effort
    }
  }

  async function handleSave() {
    setSaving(true);
    try {
      const cfg = await setAllocationConfig(token, projectId, selectedBasis);
      setConfig(cfg);
      toast(`Basis alokasi "${BASIS_LABEL[selectedBasis]}" berhasil disimpan`, "success");
      onConfigSaved(cfg);
      await runExecute();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Gagal menyimpan konfigurasi", "error");
    } finally {
      setSaving(false);
    }
  }

  async function handleRunCurrent() {
    if (!config) return;
    setSaving(true);
    try {
      onConfigSaved(config);
      await runExecute();
    } finally {
      setSaving(false);
    }
  }

  const isDirty = config ? config.basis !== selectedBasis : true;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3 flex-wrap">
          <div>
            <CardTitle>Konfigurasi Basis Alokasi</CardTitle>
            <p className="text-xs text-text-secondary mt-0.5">
              Tentukan bagaimana biaya proyek-level didistribusikan ke setiap unit.
            </p>
          </div>
          {config && (
            <Badge variant="accent" className="shrink-0">
              Tersimpan: {BASIS_LABEL[config.basis]}
            </Badge>
          )}
        </div>
      </CardHeader>

      {loading ? (
        <p className="text-sm text-text-secondary animate-pulse">Memuat konfigurasi…</p>
      ) : loadError ? (
        <div className="space-y-3">
          <p className="text-sm text-danger">{loadError}</p>
          <Button variant="secondary" size="sm" onClick={loadConfig}>Coba Lagi</Button>
        </div>
      ) : (
        <div className="space-y-4">
          {!config && (
            <div className="text-xs bg-warning-bg border border-warning/30 rounded p-3 text-warning">
              Konfigurasi alokasi belum diatur untuk proyek ini. Pilih basis di bawah dan simpan.
            </div>
          )}

          <div className="space-y-2">
            {BASIS_OPTIONS.map((opt) => (
              <label
                key={opt.value}
                className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors
                  ${selectedBasis === opt.value
                    ? "border-accent bg-accent/5"
                    : "border-border hover:border-accent/40 hover:bg-bg"
                  }`}
              >
                <input
                  type="radio"
                  name={`basis-${projectId}`}
                  value={opt.value}
                  checked={selectedBasis === opt.value}
                  onChange={() => setSelected(opt.value)}
                  className="mt-0.5 accent-accent shrink-0"
                />
                <div>
                  <p className="text-sm font-medium text-text-primary">{opt.label}</p>
                  <p className="text-xs text-text-secondary mt-0.5">{opt.desc}</p>
                </div>
              </label>
            ))}
          </div>

          <div className="flex flex-wrap gap-2 pt-1">
            {isDirty ? (
              <Button onClick={handleSave} loading={saving}>
                Simpan & Jalankan Alokasi
              </Button>
            ) : (
              <>
                <Button variant="secondary" onClick={handleSave} loading={saving} disabled>
                  Tersimpan
                </Button>
                <Button onClick={handleRunCurrent} variant="secondary">
                  Jalankan Ulang Alokasi
                </Button>
              </>
            )}
          </div>
        </div>
      )}
    </Card>
  );
}
