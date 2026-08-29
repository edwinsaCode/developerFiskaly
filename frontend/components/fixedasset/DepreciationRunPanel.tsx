"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import type { DepreciationRunResult, FixedAsset } from "@/lib/types/api";
import { Select } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { useToast } from "@/components/ui/Toast";
import { runDepreciationAction } from "@/app/(app)/accounting/aset-tetap/actions";

const MONTHS = [
  "Januari", "Februari", "Maret", "April", "Mei", "Juni",
  "Juli", "Agustus", "September", "Oktober", "November", "Desember",
];

function rupiah(v: string): string {
  const n = parseInt(v, 10);
  return Number.isNaN(n) ? v : `Rp ${n.toLocaleString("id-ID")}`;
}

// DepreciationRunPanel: pemicu manual penyusutan bulanan. Aman ditekan
// berulang untuk periode yang sama — backend menolak duplikat lewat
// UNIQUE(tenant, aset, periode) dan melaporkannya sebagai "dilewati", bukan
// menjurnal ganda.
export function DepreciationRunPanel({ assets, canWrite }: { assets: FixedAsset[]; canWrite: boolean }) {
  const router = useRouter();
  const { toast } = useToast();
  const now = new Date();
  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth() + 1);
  const [result, setResult] = useState<DepreciationRunResult | null>(null);
  const [running, startRun] = useTransition();

  const activeAssets = assets.filter((a) => a.status === "active");

  function assetName(id: number): string {
    return activeAssets.find((a) => a.id === id)?.asset_name ?? `Aset #${id}`;
  }

  function handleRun() {
    startRun(async () => {
      try {
        const res = await runDepreciationAction(year, month);
        setResult(res);
        router.refresh();
      } catch (err) {
        toast(`Gagal menjalankan penyusutan: ${err instanceof Error ? err.message : "Kesalahan server"}`, "error");
      }
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Jalankan Penyusutan Bulanan</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Memposting jurnal penyusutan garis lurus untuk semua aset aktif pada satu
          periode. Aset yang belum mulai atau sudah habis masa manfaatnya dilewati
          otomatis.
        </p>
      </CardHeader>

      {!canWrite ? (
        <p className="text-sm text-text-secondary">
          Peran Anda hanya bisa melihat. Menjalankan penyusutan memerlukan peran
          Pemilik atau Accounting.
        </p>
      ) : (
        <>
          <div className="grid grid-cols-1 sm:grid-cols-[1fr_1fr_auto] gap-3 items-end">
            <Select label="Bulan" value={String(month)} onChange={(e) => setMonth(Number(e.target.value))}>
              {MONTHS.map((m, i) => (
                <option key={m} value={i + 1}>{m}</option>
              ))}
            </Select>
            <Select label="Tahun" value={String(year)} onChange={(e) => setYear(Number(e.target.value))}>
              {Array.from({ length: 6 }, (_, i) => now.getFullYear() - 3 + i).map((y) => (
                <option key={y} value={y}>{y}</option>
              ))}
            </Select>
            <Button onClick={handleRun} loading={running} disabled={activeAssets.length === 0}>
              Jalankan Penyusutan
            </Button>
          </div>

          {activeAssets.length === 0 && (
            <p className="mt-3 text-xs text-text-tertiary">
              Belum ada aset tetap aktif untuk disusutkan.
            </p>
          )}

          {result && (
            <div className="mt-4 space-y-3 text-sm">
              <div>
                <p className="text-xs font-semibold text-success uppercase tracking-wide mb-1">
                  Terposting ({result.posted?.length ?? 0})
                </p>
                {result.posted && result.posted.length > 0 ? (
                  <ul className="space-y-0.5">
                    {result.posted.map((p) => (
                      <li key={p.asset_id} className="flex justify-between text-text-primary">
                        <span>{assetName(p.asset_id)}</span>
                        <span className="tabular-nums font-medium">{p.amount ? rupiah(p.amount) : "—"}</span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-text-tertiary">Tidak ada.</p>
                )}
              </div>
              <div>
                <p className="text-xs font-semibold text-text-tertiary uppercase tracking-wide mb-1">
                  Dilewati ({result.skipped?.length ?? 0})
                </p>
                {result.skipped && result.skipped.length > 0 ? (
                  <ul className="space-y-0.5">
                    {result.skipped.map((s) => (
                      <li key={s.asset_id} className="flex justify-between text-text-secondary">
                        <span>{assetName(s.asset_id)}</span>
                        <span className="text-[11px] text-text-tertiary">{s.reason}</span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-text-tertiary">Tidak ada.</p>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </Card>
  );
}
