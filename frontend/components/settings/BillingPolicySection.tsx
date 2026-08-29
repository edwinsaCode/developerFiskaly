"use client";

// Kebijakan BAST (Serah Terima) — SEKARANG BACA SAJA.
//
// W-5 / keputusan klien D-3: syarat "Biaya Realisasi lunas sebelum BAST"
// DICABUT permanen. BAST tidak pernah lagi ditolak karena biaya realisasi belum
// lunas; sisanya menjadi Piutang Customer lewat invoice yang terbit otomatis
// saat BAST. Karena tidak ada lagi yang bisa disetel, toggle-nya dihapus —
// menyisakan setelan mati di layar hanya membuat admin mengira ia masih berlaku.
//
// R-A tetap: riwayat perubahan kebijakan bertahan walau kebijakannya sudah
// tidak ada. Pencabutan itu sendiri tercatat sebagai baris riwayat (migrasi
// 000067), sehingga pertanyaan "kenapa gate-nya hilang" bisa dijawab layar ini.

import { useEffect, useState } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { Tanggal } from "@/components/format/Tanggal";
import { fetchChargePolicyHistory, type PolicyChange } from "@/lib/api/charge";

const POLICY_LABEL: Record<string, string> = {
  require_realization_settled: "Biaya Realisasi lunas sebelum BAST",
};

function nilaiLabel(v: string): string {
  if (v === "true") return "Aktif";
  if (v === "false") return "Nonaktif";
  return v;
}

export function BillingPolicySection({ token }: { token: string }) {
  const [history, setHistory] = useState<PolicyChange[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchChargePolicyHistory(token, 20)
      .then((h) => { if (!cancelled) setHistory(h); })
      .catch(() => { if (!cancelled) setHistory([]); });
    return () => { cancelled = true; };
  }, [token]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Kebijakan BAST (Serah Terima)</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Syarat serah terima kunci. Setiap perubahan tercatat dalam jejak audit.
        </p>
      </CardHeader>
      <div className="space-y-3">
        <div className="flex items-center justify-between gap-3 px-4 py-3 rounded-lg border border-border">
          <div>
            <p className="text-sm font-medium text-text-primary">Harga rumah lunas sebelum BAST</p>
            <p className="text-xs text-text-secondary">Mengikuti gate skema pembayaran kontrak (Cash: lunas · KPR: akad/pencairan).</p>
          </div>
          <Badge variant="success">Aktif — via skema</Badge>
        </div>
        <div className="flex items-center justify-between gap-3 px-4 py-3 rounded-lg border border-border">
          <div>
            <p className="text-sm font-medium text-text-primary">Biaya Realisasi lunas sebelum BAST</p>
            <p className="text-xs text-text-secondary">
              Tidak lagi menjadi syarat. Saat BAST, sisa Biaya Realisasi yang belum ditagihkan
              otomatis diterbitkan invoice-nya dan menjadi <strong>Piutang Customer</strong> —
              serah terima kunci tidak pernah tertahan karenanya.
            </p>
          </div>
          <Badge variant="default">Dicabut</Badge>
        </div>

        {/* Jejak audit (R-A) */}
        <div className="px-4 py-3 rounded-lg border border-border">
          <p className="text-sm font-medium text-text-primary mb-1">Riwayat Perubahan Kebijakan</p>
          {history === null ? (
            <p className="text-xs text-text-tertiary">Memuat…</p>
          ) : history.length === 0 ? (
            <p className="text-xs text-text-secondary">
              Belum ada perubahan tercatat untuk perusahaan ini.
            </p>
          ) : (
            <div className="divide-y divide-border-subtle">
              {history.map((h) => (
                <div key={h.id} className="py-2 text-xs">
                  <div className="flex items-center gap-2 flex-wrap">
                    <Tanggal value={h.created_at} />
                    <span className="text-text-primary font-medium">
                      {POLICY_LABEL[h.policy_key] ?? h.policy_key}
                    </span>
                    <span className="text-text-secondary">
                      {nilaiLabel(h.old_value)} → <strong>{nilaiLabel(h.new_value)}</strong>
                    </span>
                    {h.changed_by && <span className="text-text-tertiary">oleh user #{h.changed_by}</span>}
                  </div>
                  {h.notes && <p className="text-text-secondary mt-0.5">{h.notes}</p>}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </Card>
  );
}
