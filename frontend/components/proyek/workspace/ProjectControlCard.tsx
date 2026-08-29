"use client";

// R3 — Kartu Kontrol Proyek: Fisik vs Biaya vs Collection dalam satu pandangan.
// Fisik = input manual append-only (bukan ledger); Biaya & Collection = derived
// dari accounting engine (dashboard). Warning bila biaya mendahului fisik >15pt.

import { useCallback, useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { useToast } from "@/components/ui/Toast";
import { Tanggal } from "@/components/format/Tanggal";
import { ApiError } from "@/lib/api/client";
import { useMayWrite } from "@/lib/hooks/usePermissions";
import { todayLocalStr } from "@/lib/date";
import {
  fetchProjectProgress, addProjectProgress, type ProjectProgressEntry,
} from "@/lib/api/projects";

interface Props {
  token: string;
  projectId: number;
  /** Dari dashboard row (derived ledger). */
  costPct: string;
  collectionPct: string;
  /** Refresh parent setelah input fisik (agar warning dashboard ikut segar). */
  onChanged?: () => void;
}

function pctNum(s: string | null | undefined): number {
  const n = parseFloat(s ?? "0");
  return isNaN(n) ? 0 : Math.max(0, Math.min(n, 100));
}

function ControlBar({ label, pct, cls, sub }: { label: string; pct: number; cls: string; sub?: string }) {
  return (
    <div>
      <div className="flex items-center justify-between text-sm">
        <span className="text-text-secondary">{label}</span>
        <span className="font-semibold tabular-nums">{pct.toFixed(0)}%</span>
      </div>
      <div className="mt-1.5 flex h-2 w-full overflow-hidden rounded-full bg-border-subtle">
        <div className={cls} style={{ width: `${pct}%` }} />
      </div>
      {sub && <p className="mt-1 text-[11px] text-text-tertiary">{sub}</p>}
    </div>
  );
}

export function ProjectControlCard({ token, projectId, costPct, collectionPct, onChanged }: Props) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [entries, setEntries] = useState<ProjectProgressEntry[]>([]);
  const [showInput, setShowInput] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const [busy, setBusy] = useState(false);

  const [pct, setPct] = useState("");
  const [asOf, setAsOf] = useState(todayLocalStr());
  const [notes, setNotes] = useState("");

  const load = useCallback(() => {
    fetchProjectProgress(token, projectId).then(setEntries).catch(() => {});
  }, [token, projectId]);

  useEffect(() => { load(); }, [load]);

  const latest = entries[0] ?? null;
  const fisik = latest ? pctNum(latest.progress_pct) : null;
  const biaya = pctNum(costPct);
  const collection = pctNum(collectionPct);
  const gap = fisik !== null ? biaya - fisik : 0;
  const warn = fisik !== null && gap > 15;

  async function submit() {
    setBusy(true);
    try {
      await addProjectProgress(token, projectId, {
        progress_pct: pct, as_of_date: asOf, notes: notes || undefined,
      });
      toast("Progress fisik tercatat", "success");
      setShowInput(false);
      setPct(""); setNotes("");
      load();
      onChanged?.();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mencatat progress", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card padding="sm">
      <div className="flex items-center justify-between mb-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary">
          Kontrol Proyek — Fisik vs Biaya vs Collection
        </p>
        {mayWrite && (
          <Button size="sm" variant="secondary" onClick={() => setShowInput(true)}>
            + Update Fisik
          </Button>
        )}
      </div>

      <div className="space-y-3">
        {fisik === null ? (
          <div className="rounded-md border border-dashed border-border p-3 text-sm text-text-secondary">
            Progress <strong>fisik</strong> belum pernah diinput. Catat progress lapangan
            (kurva-S) agar owner bisa membandingkan fisik vs biaya — indikator dini over budget.
          </div>
        ) : (
          <ControlBar
            label="Fisik (lapangan)"
            pct={fisik}
            cls="bg-accent"
            sub={latest ? `terakhir update ${daysAgo(latest.as_of_date)} — ${latest.notes || "tanpa catatan"}` : undefined}
          />
        )}
        <ControlBar label="Biaya (realisasi vs RAB)" pct={biaya} cls="bg-warning" />
        <ControlBar label="Collection (kas vs nilai kontrak)" pct={collection} cls="bg-success" />
      </div>

      {warn && (
        <div className="mt-3 rounded-md border border-danger/30 bg-danger-bg px-3 py-2 text-sm text-danger">
          ⚠ Biaya berjalan <strong>{gap.toFixed(0)}pt</strong> di depan fisik — indikasi over
          budget / prestasi tertinggal. Cek realisasi per kategori di tab RAB.
        </div>
      )}

      {entries.length > 0 && (
        <button
          className="mt-3 text-xs text-accent hover:underline"
          onClick={() => setShowHistory((v) => !v)}
        >
          {showHistory ? "Sembunyikan riwayat" : `Riwayat input (${entries.length}) →`}
        </button>
      )}
      {showHistory && (
        <ul className="mt-2 divide-y divide-border text-sm">
          {entries.map((e) => (
            <li key={e.id} className="flex items-center justify-between py-1.5">
              <span className="text-text-secondary">
                <Tanggal value={e.as_of_date} />
                {e.notes && <span className="text-text-tertiary text-xs ml-2">{e.notes}</span>}
              </span>
              <span className="font-semibold tabular-nums">{pctNum(e.progress_pct).toFixed(0)}%</span>
            </li>
          ))}
        </ul>
      )}

      <Modal
        open={showInput}
        onClose={() => setShowInput(false)}
        title="Update Progress Fisik"
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowInput(false)} disabled={busy}>Batal</Button>
            <Button onClick={submit} loading={busy} disabled={!pct}>Simpan</Button>
          </>
        }
      >
        <FormGrid>
          <Input
            label="Progress Fisik (%)" required type="number" min={0} max={100} value={pct}
            onChange={(e) => setPct(e.target.value)} placeholder="cth: 40" suffix="%"
          />
          <Input
            label="Per Tanggal" type="date" value={asOf}
            onChange={(e) => setAsOf(e.target.value)}
          />
          <FormFull>
            <Input
              label="Catatan" value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder="cth: struktur lantai 2 selesai"
            />
          </FormFull>
          <FormFull>
            <p className="text-xs text-text-tertiary">
              Riwayat bersifat <strong>append-only</strong> (tercatat siapa &amp; kapan). Menurunkan
              angka dari input terakhir wajib disertai catatan alasan.
            </p>
          </FormFull>
        </FormGrid>
      </Modal>
    </Card>
  );
}

function daysAgo(dateStr: string): string {
  const d = new Date(dateStr);
  const days = Math.floor((Date.now() - d.getTime()) / 86_400_000);
  if (days <= 0) return "hari ini";
  if (days === 1) return "kemarin";
  return `${days} hari lalu`;
}
