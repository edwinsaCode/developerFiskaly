"use client";

import { useState, useEffect } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { AllocationResultTable } from "./AllocationResultTable";
import { fetchAllocationCompute } from "@/lib/api/allocation";
import { ApiError } from "@/lib/api/client";
import type { AllocationComputeResponse, Unit } from "@/lib/types/api";

interface Props {
  token: string;
  projectId: number;
  units: Unit[];
  // Incrementing this triggers an automatic re-compute (e.g. after config save)
  triggerKey: number;
}

export function AllocationPreview({ token, projectId, units, triggerKey }: Props) {
  const [results, setResults]   = useState<AllocationComputeResponse | null>(null);
  const [loading, setLoading]   = useState(false);
  const [error, setError]       = useState<string | null>(null);

  const unitMap = Object.fromEntries(units.map((u) => [u.id, u]));

  async function compute() {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchAllocationCompute(token, projectId);
      setResults(data);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Gagal menghitung alokasi");
      setResults(null);
    } finally {
      setLoading(false);
    }
  }

  // Auto-run when parent signals config was saved
  useEffect(() => {
    if (triggerKey > 0) compute();
  }, [triggerKey]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold text-text-primary">Hasil Alokasi HPP</h3>
          <p className="text-xs text-text-secondary mt-0.5">
            Distribusi biaya proyek ke setiap unit berdasarkan basis yang dikonfigurasi.
          </p>
        </div>
        <Button variant="secondary" size="sm" onClick={compute} loading={loading}>
          {results ? "Hitung Ulang" : "Hitung Sekarang"}
        </Button>
      </div>

      {error && (
        <Card padding="sm">
          <p className="text-sm text-danger">{error}</p>
          <p className="text-xs text-text-tertiary mt-1">
            Pastikan: (1) basis alokasi sudah disimpan, (2) biaya proyek sudah dicatat dan diposting,
            (3) setiap unit memiliki nilai luas area / harga jual.
          </p>
        </Card>
      )}

      {!error && !results && !loading && (
        <Card padding="sm">
          <p className="text-sm text-text-secondary text-center py-4">
            Klik <strong>Hitung Sekarang</strong> untuk menghitung distribusi HPP,
            atau simpan konfigurasi basis untuk menjalankan otomatis.
          </p>
        </Card>
      )}

      {loading && !results && (
        <Card padding="sm">
          <p className="text-sm text-text-secondary text-center py-4 animate-pulse">
            Menghitung alokasi…
          </p>
        </Card>
      )}

      {results !== null && results.data.length === 0 && (
        <Card padding="sm">
          <p className="text-sm text-text-secondary text-center py-4">
            Tidak ada unit di proyek ini. Tambah unit terlebih dahulu.
          </p>
        </Card>
      )}

      {results !== null && results.data.length > 0 && (
        <AllocationResultTable results={results.data} totals={results.totals} unitMap={unitMap} />
      )}
    </div>
  );
}
