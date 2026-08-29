"use client";

// P0-4 — Closing Wizard: Completion → Finalisasi → Hitung → Setujui → Posting
// (docs/p0-4-frontend-spec.md). State diambil dari backend; frontend tidak
// menghitung uang (U8).

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import {
  fetchCompletion,
  markCompleted,
  finalizeCompletion,
  fetchVariancePreview,
  calculateTrueup,
  fetchTrueupRuns,
  fetchTrueupRun,
  approveTrueup,
  postTrueup,
  cancelTrueup,
  type CompletionEvent,
  type TrueupRun,
  type TrueupLine,
  type VariancePreview,
} from "@/lib/api/closing";

const CATEGORY_LABELS: Record<string, string> = {
  land: "Tanah",
  hard: "Konstruksi",
  soft: "Soft Cost",
  financing: "Pembiayaan",
};

export function ClosingPanel({ token, projectId }: { token: string; projectId: number }) {
  const { toast } = useToast();
  const [completion, setCompletion] = useState<CompletionEvent | null>(null);
  const [run, setRun] = useState<TrueupRun | null>(null);
  const [preview, setPreview] = useState<VariancePreview | null>(null);
  const [previewErr, setPreviewErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState<"finalize" | "post" | "cancel" | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [ev, runs] = await Promise.all([
        fetchCompletion(token, projectId),
        fetchTrueupRuns(token, projectId).catch(() => [] as TrueupRun[]),
      ]);
      setCompletion(ev);
      const activeRun = runs.find((r) => r.status !== "cancelled") ?? null;
      if (activeRun) {
        const full = await fetchTrueupRun(token, activeRun.id).catch(() => activeRun);
        setRun(full);
      } else {
        setRun(null);
      }
      // Preview variance (read-only) — utk langkah 3 & ringkasan.
      try {
        const p = await fetchVariancePreview(token, projectId);
        setPreview(p);
        setPreviewErr(null);
      } catch (e) {
        setPreview(null);
        setPreviewErr(e instanceof ApiError ? e.message : null);
      }
    } finally {
      setLoading(false);
    }
  }, [token, projectId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const step = useMemo(() => {
    if (run?.status === "posted") return 6; // selesai
    if (run?.status === "approved") return 5;
    if (run?.status === "calculated") return 4;
    if (completion?.status === "finalized") return 3;
    if (completion?.status === "completed") return 2;
    return 1;
  }, [completion, run]);

  async function act(fn: () => Promise<unknown>, okMsg: string) {
    setBusy(true);
    try {
      await fn();
      toast(okMsg, "success");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memproses", "error");
    } finally {
      setBusy(false);
      setConfirm(null);
    }
  }

  if (loading) {
    return (
      <Card padding="sm">
        <div className="animate-pulse space-y-2">
          {[...Array(4)].map((_, i) => <div key={i} className="h-8 bg-border-subtle rounded" />)}
        </div>
      </Card>
    );
  }

  const lines: TrueupLine[] = run?.lines?.length ? run.lines : preview?.lines ?? [];
  const totals = run ?? preview;

  return (
    <div className="space-y-4">
      {/* Stepper */}
      <Card padding="sm">
        <div className="flex items-center gap-1 overflow-x-auto text-xs">
          {[
            "Tandai Selesai",
            "Finalisasi Biaya",
            "Hitung Variance",
            "Setujui",
            "Posting Jurnal",
          ].map((label, i) => {
            const n = i + 1;
            const done = step > n;
            const active = step === n;
            return (
              <div key={label} className="flex items-center gap-1 shrink-0">
                {i > 0 && <span className="w-6 h-px bg-border" />}
                <span
                  className={`flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-bold
                    ${done ? "bg-success text-white" : active ? "bg-accent text-white" : "bg-border-subtle text-text-secondary"}`}
                >
                  {done ? "✓" : n}
                </span>
                <span className={active ? "font-medium text-text-primary" : "text-text-secondary"}>
                  {label}
                </span>
              </div>
            );
          })}
        </div>
      </Card>

      {step === 6 && run && (
        <div className="rounded border border-success bg-success-bg px-3 py-2 text-sm">
          ✓ Closing selesai — HPP proyek kini <strong>aktual</strong>.
          {run.journal_id ? (
            <>
              {" "}Jurnal penyesuaian:{" "}
              <Link href={`/accounting/jurnal/${run.journal_id}`} className="text-accent hover:underline">
                #{run.journal_id}
              </Link>
            </>
          ) : (
            " Tidak ada variance yang perlu dijurnal."
          )}
        </div>
      )}

      {/* Langkah aktif + aksi */}
      {step === 1 && (
        <Card padding="sm">
          <p className="text-sm text-text-secondary mb-3">
            Tandai konstruksi selesai. Status proyek menjadi <strong>Selesai</strong>;
            biaya masih bisa dicatat sampai finalisasi.
          </p>
          {preview === null && previewErr && (
            <p className="text-xs text-warning mb-3">ℹ {previewErr}</p>
          )}
          <Button size="sm" loading={busy}
            onClick={() => act(() => markCompleted(token, projectId), "Proyek ditandai selesai")}>
            Tandai Konstruksi Selesai
          </Button>
        </Card>
      )}

      {step === 2 && (
        <Card padding="sm">
          <p className="text-sm text-text-secondary mb-3">
            Kunci biaya aktual (A<sub>c</sub>). Setelah final, penjualan unit baru
            menunggu true-up diposting.
          </p>
          <Button size="sm" variant="danger" loading={busy} onClick={() => setConfirm("finalize")}>
            Finalisasi Biaya Aktual
          </Button>
        </Card>
      )}

      {(step === 3 || step === 4 || step === 5) && totals && (
        <Card padding="sm">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm mb-4">
            <div>
              <p className="text-xs text-text-secondary">HPP Budgeted (terjual)</p>
              <p className="font-medium"><Rupiah value={totals.budget_hpp_total} colorSign={false} /></p>
            </div>
            <div>
              <p className="text-xs text-text-secondary">Biaya Aktual Proyek</p>
              <p className="font-medium"><Rupiah value={totals.actual_cost_total} colorSign={false} /></p>
            </div>
            <div>
              <p className="text-xs text-text-secondary">Penyesuaian HPP</p>
              <p className="font-semibold"><Rupiah value={totals.variance_total} /></p>
            </div>
            {preview && (
              <div>
                <p className="text-xs text-text-secondary">Unit</p>
                <p className="font-medium">
                  {preview.sold_units} terjual · {preview.unsold_units} stok
                </p>
              </div>
            )}
          </div>

          <div className="flex gap-2 flex-wrap">
            {step === 3 && (
              <Button size="sm" loading={busy}
                onClick={() => act(() => calculateTrueup(token, projectId), "Variance dihitung & disimpan")}>
                {run ? "Hitung Ulang" : "Hitung & Simpan"}
              </Button>
            )}
            {step === 4 && run && (
              <>
                <Button size="sm" variant="secondary" loading={busy}
                  onClick={() => act(() => calculateTrueup(token, projectId), "Dihitung ulang")}>
                  Hitung Ulang
                </Button>
                <Button size="sm" loading={busy}
                  onClick={() => act(() => approveTrueup(token, run.id), "True-up disetujui — siap posting")}>
                  Setujui
                </Button>
              </>
            )}
            {step === 5 && run && (
              <Button size="sm" variant="danger" loading={busy} onClick={() => setConfirm("post")}>
                Posting Jurnal Penyesuaian
              </Button>
            )}
            {run && run.status !== "posted" && (
              <Button size="sm" variant="secondary" loading={busy} onClick={() => setConfirm("cancel")}>
                Batalkan Run
              </Button>
            )}
          </div>
        </Card>
      )}

      {/* Tabel variance */}
      {lines.length > 0 ? (
        <Card padding="sm" className="overflow-x-auto">
          <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">
            Rincian per Unit × Kategori
          </p>
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-text-secondary border-b border-border">
                <th className="py-2 pr-3">Unit</th>
                <th className="py-2 pr-3">Kategori</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2 pr-3 text-right">Budgeted</th>
                <th className="py-2 pr-3 text-right">Aktual</th>
                <th className="py-2 pr-3 text-right">Selisih</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {lines.map((l, i) => (
                <tr key={l.id || i} className="border-b border-border last:border-0">
                  <td className="py-2 pr-3 font-medium">{l.unit_name_snapshot || `#${l.unit_id}`}</td>
                  <td className="py-2 pr-3">{CATEGORY_LABELS[l.category] ?? l.category}</td>
                  <td className="py-2 pr-3">
                    <Badge variant={l.is_sold ? "accent" : "neutral"}>
                      {l.is_sold ? "Terjual" : "Stok"}
                    </Badge>
                  </td>
                  <td className="py-2 pr-3 text-right">
                    {l.is_sold ? <Rupiah value={l.budgeted_amount} colorSign={false} /> : "—"}
                  </td>
                  <td className="py-2 pr-3 text-right"><Rupiah value={l.actual_amount} colorSign={false} /></td>
                  <td className="py-2 pr-3 text-right">
                    {l.is_sold
                      ? <Rupiah value={l.variance_amount} />
                      : <span className="text-xs text-text-secondary">tetap di stok</span>}
                  </td>
                  <td className="py-2 text-right">
                    {l.journal_entry_id && (
                      <Link href={`/accounting/jurnal/${l.journal_entry_id}`}
                        className="text-xs text-accent hover:underline">
                        jurnal
                      </Link>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      ) : step < 6 && !previewErr ? null : step < 6 && previewErr && step >= 3 ? (
        <EmptyState title="Belum bisa menghitung variance" description={previewErr} />
      ) : null}

      {step === 1 && !previewErr && preview === null && (
        <EmptyState
          title="Penyelesaian proyek"
          description="Dilakukan saat konstruksi selesai untuk merekonsiliasi HPP budgeted ke biaya aktual. Belum ada yang perlu dilakukan sekarang."
        />
      )}

      {/* Konfirmasi ireversibel */}
      <ConfirmModal
        open={confirm === "finalize"}
        title="Finalisasi Biaya Aktual?"
        confirmLabel="Finalisasi"
        variant="danger"
        loading={busy}
        onCancel={() => setConfirm(null)}
        onConfirm={() => act(() => finalizeCompletion(token, projectId), "Biaya aktual dikunci")}
      >
        <p className="text-sm">
          Biaya aktual dikunci per hari ini
          {preview && <> (<Rupiah value={preview.actual_cost_total} colorSign={false} />)</>}.
          Setelah ini <strong>BAST unit baru menunggu true-up diposting</strong>. Tidak bisa dibatalkan.
        </p>
      </ConfirmModal>
      <ConfirmModal
        open={confirm === "post"}
        title="Posting Jurnal True-up?"
        confirmLabel="Posting"
        variant="danger"
        loading={busy}
        onCancel={() => setConfirm(null)}
        onConfirm={async () => { if (run) await act(() => postTrueup(token, run.id), "Jurnal true-up diposting — closing selesai"); }}
      >
        <p className="text-sm">
          Jurnal penyesuaian
          {run && <> sebesar <strong><Rupiah value={run.variance_total} /></strong></>}{" "}
          diposting ke periode berjalan (<em>prior period HPP adjustment</em>). Final &amp; immutable.
        </p>
      </ConfirmModal>
      <ConfirmModal
        open={confirm === "cancel"}
        title="Batalkan Run True-up?"
        confirmLabel="Batalkan Run"
        variant="danger"
        loading={busy}
        onCancel={() => setConfirm(null)}
        onConfirm={async () => { if (run) await act(() => cancelTrueup(token, run.id), "Run dibatalkan — hitung ulang kapan saja"); }}
      >
        <p className="text-sm">Run dibatalkan (belum ada jurnal). Anda bisa menghitung ulang dari awal.</p>
      </ConfirmModal>
    </div>
  );
}
