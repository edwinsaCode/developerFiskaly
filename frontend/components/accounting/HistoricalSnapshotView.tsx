"use client";

// W-6 — Laporan Historis (snapshot keuangan tahun sebelum sistem dipakai).
//
// Layar ini SENGAJA berdiri sendiri, tidak menumpang halaman Laporan. Angka di
// sini tidak berasal dari buku besar dan tidak ikut menyusun laporan berjalan;
// menaruhnya di tab yang sama dengan Neraca berjalan akan mengundang orang
// membandingkan dua angka yang tidak sebanding.
//
// Alurnya satu arah dan pendek: pilih tahun → isi angka → simpan → finalkan.

import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { EmptyState } from "@/components/ui/EmptyState";
import { Input, Textarea } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { useToast } from "@/components/ui/Toast";
import { Rupiah, formatRupiah } from "@/components/format/Rupiah";
import {
  createSnapshot,
  deleteSnapshot,
  fetchHistLabaRugi,
  fetchHistNeraca,
  fetchSnapshot,
  fetchSnapshotAudits,
  fetchSnapshotYears,
  finalizeSnapshot,
  reopenSnapshot,
  saveSnapshot,
  type HistLabaRugi,
  type HistNeraca,
  type SnapshotAudit,
  type SnapshotDetail,
  type SnapshotSummary,
} from "@/lib/api/histfin";
import type { Account } from "@/lib/types/api";

interface Props {
  token: string;
  accounts: Account[];
  role: string;
}

type Tab = "isi" | "neraca" | "laba-rugi" | "riwayat";

const SECTIONS: { type: Account["type"]; label: string }[] = [
  { type: "asset", label: "Aset" },
  { type: "liability", label: "Kewajiban" },
  { type: "equity", label: "Ekuitas" },
  { type: "revenue", label: "Pendapatan" },
  { type: "expense", label: "Beban" },
];

// toNumber hanya untuk PRATINJAU di layar isian. Angka yang berlaku selalu
// datang dari server (detail.totals) — lihat catatan di strip keseimbangan.
function toNumber(raw: string): number {
  if (!raw || raw === "-") return 0;
  const n = Number(raw);
  return Number.isFinite(n) ? n : 0;
}

// sanitize menerima digit dan SATU tanda minus di depan: defisit akumulasi dan
// akumulasi penyusutan memang bernilai negatif, jadi input tidak boleh menolak
// minus seperti RupiahInput biasa.
function sanitize(input: string): string {
  const neg = input.trim().startsWith("-");
  const digits = input.replace(/[^0-9]/g, "");
  if (!digits) return neg ? "-" : "";
  return (neg ? "-" : "") + digits.replace(/^0+(?=\d)/, "");
}

function displayAmount(raw: string): string {
  if (!raw || raw === "-") return raw;
  const n = Number(raw);
  if (!Number.isFinite(n)) return raw;
  return n.toLocaleString("id-ID");
}

export function HistoricalSnapshotView({ token, accounts, role }: Props) {
  const { toast } = useToast();
  const canWrite = role === "owner" || role === "accountant";
  const isOwner = role === "owner";

  const [years, setYears] = useState<SnapshotSummary[]>([]);
  const [selected, setSelected] = useState<number | null>(null);
  const [detail, setDetail] = useState<SnapshotDetail | null>(null);
  const [tab, setTab] = useState<Tab>("isi");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  // Isian lokal: accountId → nominal mentah. Dipisah dari `detail` supaya
  // "belum disimpan" selalu kelihatan bedanya dengan "sudah tersimpan".
  const [draft, setDraft] = useState<Record<number, string>>({});
  const [notes, setNotes] = useState("");
  const [dirty, setDirty] = useState(false);
  const [filter, setFilter] = useState("");

  const [neraca, setNeraca] = useState<HistNeraca | null>(null);
  const [labaRugi, setLabaRugi] = useState<HistLabaRugi | null>(null);
  const [audits, setAudits] = useState<SnapshotAudit[]>([]);

  const [addOpen, setAddOpen] = useState(false);
  const [newYear, setNewYear] = useState(String(new Date().getFullYear() - 1));
  const [finalOpen, setFinalOpen] = useState(false);
  const [reopenOpen, setReopenOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);

  const readOnly = !canWrite || detail?.status === "final";

  // ── Muat daftar tahun ──────────────────────────────────────────────────────
  const loadYears = useCallback(
    async (prefer?: number) => {
      const list = await fetchSnapshotYears(token).catch(() => []);
      setYears(list);
      setSelected((cur) => {
        const want = prefer ?? cur;
        if (want && list.some((y) => y.fiscal_year === want)) return want;
        return list[0]?.fiscal_year ?? null;
      });
      setLoading(false);
    },
    [token],
  );

  useEffect(() => {
    void loadYears();
  }, [loadYears]);

  // ── Muat satu tahun ────────────────────────────────────────────────────────
  const loadDetail = useCallback(
    async (year: number) => {
      const d = await fetchSnapshot(token, year);
      setDetail(d);
      const next: Record<number, string> = {};
      for (const l of d.lines) next[l.account_id] = String(l.amount).replace(/\.0+$/, "");
      setDraft(next);
      setNotes(d.notes ?? "");
      setDirty(false);
      setNeraca(null);
      setLabaRugi(null);
      setAudits([]);
    },
    [token],
  );

  useEffect(() => {
    if (selected == null) {
      setDetail(null);
      return;
    }
    void loadDetail(selected).catch((e: unknown) =>
      toast(e instanceof Error ? e.message : "Gagal memuat snapshot", "error"),
    );
    // toast identitasnya stabil dari provider; tidak perlu jadi dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, loadDetail]);

  // ── Muat tab laporan sesuai kebutuhan ──────────────────────────────────────
  useEffect(() => {
    if (selected == null || dirty) return;
    if (tab === "neraca" && !neraca) {
      void fetchHistNeraca(token, selected).then(setNeraca).catch(() => setNeraca(null));
    }
    if (tab === "laba-rugi" && !labaRugi) {
      void fetchHistLabaRugi(token, selected).then(setLabaRugi).catch(() => setLabaRugi(null));
    }
    if (tab === "riwayat" && audits.length === 0) {
      void fetchSnapshotAudits(token, selected).then(setAudits).catch(() => setAudits([]));
    }
  }, [tab, selected, token, neraca, labaRugi, audits.length, dirty]);

  // ── Pratinjau keseimbangan ─────────────────────────────────────────────────
  //
  // Server tetap pemegang keputusan: finalisasi ditolak backend kalau timpang.
  // Angka di sini semata-mata supaya admin tidak menekan Simpan berkali-kali
  // hanya untuk tahu kurang berapa.
  const preview = useMemo(() => {
    const byType: Record<string, number> = {};
    for (const acc of accounts) {
      const v = toNumber(draft[acc.id] ?? "");
      if (!v) continue;
      byType[acc.type] = (byType[acc.type] ?? 0) + v;
    }
    const aset = byType.asset ?? 0;
    const laba = (byType.revenue ?? 0) - (byType.expense ?? 0);
    const kanan = (byType.liability ?? 0) + (byType.equity ?? 0) + laba;
    return { aset, kanan, laba, selisih: aset - kanan, seimbang: aset - kanan === 0 };
  }, [accounts, draft]);

  const totals = detail?.totals;

  // ── Aksi ───────────────────────────────────────────────────────────────────

  async function run(label: string, fn: () => Promise<string | void>) {
    setBusy(true);
    try {
      const msg = await fn();
      if (msg) toast(msg, "success");
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : `${label} gagal`, "error");
    } finally {
      setBusy(false);
    }
  }

  function handleAdd() {
    const y = parseInt(newYear, 10);
    if (!y || y < 1980 || y > 2100) {
      toast("Tahun buku tidak masuk akal.", "error");
      return;
    }
    void run("Tambah tahun", async () => {
      await createSnapshot(token, y);
      setAddOpen(false);
      await loadYears(y);
      setTab("isi");
      return `Tahun buku ${y} dibuka sebagai draft.`;
    });
  }

  function handleSave() {
    if (selected == null) return;
    void run("Simpan", async () => {
      const lines = accounts
        .filter((a) => {
          const raw = draft[a.id];
          return raw && raw !== "-" && toNumber(raw) !== 0;
        })
        .map((a, i) => ({ account_id: a.id, amount: draft[a.id], sort_order: i }));
      const d = await saveSnapshot(token, selected, { notes, lines });
      setDetail(d);
      setDirty(false);
      setNeraca(null);
      setLabaRugi(null);
      setAudits([]);
      await loadYears(selected);
      return d.totals.is_balanced
        ? `Snapshot ${selected} tersimpan dan seimbang.`
        : `Snapshot ${selected} tersimpan. Selisih ${formatRupiah(d.totals.difference)} — belum bisa difinalkan.`;
    });
  }

  function handleFinalize() {
    if (selected == null) return;
    void run("Finalisasi", async () => {
      const d = await finalizeSnapshot(token, selected);
      setDetail(d);
      setFinalOpen(false);
      setAudits([]);
      await loadYears(selected);
      return `Snapshot ${selected} difinalkan (revisi ${d.revision}).`;
    });
  }

  function handleReopen() {
    if (selected == null) return;
    if (reason.trim().length < 10) {
      toast("Alasan wajib diisi minimal 10 karakter.", "error");
      return;
    }
    void run("Buka kembali", async () => {
      const d = await reopenSnapshot(token, selected, reason.trim());
      setDetail(d);
      setReopenOpen(false);
      setReason("");
      setAudits([]);
      await loadYears(selected);
      return `Snapshot ${selected} dibuka kembali untuk koreksi.`;
    });
  }

  function handleDelete() {
    if (selected == null) return;
    void run("Hapus", async () => {
      await deleteSnapshot(token, selected);
      setDeleteOpen(false);
      setSelected(null);
      await loadYears();
      return "Draft snapshot dihapus.";
    });
  }

  function setAmount(accountId: number, value: string) {
    setDraft((prev) => ({ ...prev, [accountId]: sanitize(value) }));
    setDirty(true);
  }

  // ── Render ─────────────────────────────────────────────────────────────────

  if (loading) {
    return <Card className="text-sm text-text-secondary">Memuat…</Card>;
  }

  return (
    <div className="space-y-5">
      <HistoricalBanner />

      <div className="flex flex-wrap items-center gap-2">
        {years.length === 0 && (
          <span className="text-sm text-text-secondary">Belum ada tahun historis.</span>
        )}
        {years.map((y) => {
          const active = y.fiscal_year === selected;
          return (
            <button
              key={y.fiscal_year}
              onClick={() => setSelected(y.fiscal_year)}
              className={`flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm transition-colors
                ${active
                  ? "border-accent bg-accent-light text-accent font-medium"
                  : "border-border text-text-secondary hover:border-accent/40 hover:text-accent"}`}
            >
              {y.fiscal_year}
              <Badge variant={y.status === "final" ? "success" : "warning"}>
                {y.status === "final" ? "Final" : "Draft"}
              </Badge>
            </button>
          );
        })}
        {canWrite && (
          <Button variant="secondary" size="sm" onClick={() => setAddOpen(true)}>
            + Tambah Tahun
          </Button>
        )}
      </div>

      {selected == null || !detail ? (
        <Card padding="none">
          <EmptyState
            title="Belum ada laporan historis"
            description="Tambahkan tahun buku sebelum sistem dipakai, lalu isi Neraca dan Laba Ruginya sebagaimana dilaporkan waktu itu. Setiap tahun berdiri sendiri — angka 2024 tidak diturunkan dari 2025."
            action={
              canWrite ? (
                <Button onClick={() => setAddOpen(true)}>Tambah Tahun Buku</Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <h2 className="text-lg font-semibold text-text-primary">
                Tahun Buku {detail.fiscal_year}
              </h2>
              <Badge variant={detail.status === "final" ? "success" : "warning"}>
                {detail.status === "final" ? "Final" : "Draft"}
              </Badge>
              {detail.revision > 0 && (
                <span className="text-xs text-text-tertiary">revisi {detail.revision}</span>
              )}
              {dirty && <Badge variant="accent">Belum disimpan</Badge>}
            </div>
            <div className="flex flex-wrap gap-2">
              {canWrite && detail.status === "draft" && (
                <>
                  <Button variant="secondary" onClick={handleSave} loading={busy} disabled={!dirty}>
                    Simpan
                  </Button>
                  <Button
                    onClick={() => setFinalOpen(true)}
                    disabled={busy || dirty || !totals?.is_balanced || !totals?.line_count}
                    title={
                      dirty
                        ? "Simpan dulu sebelum finalisasi"
                        : !totals?.line_count
                          ? "Belum ada angka yang diisi"
                          : totals?.is_balanced
                            ? undefined
                            : "Neraca belum seimbang"
                    }
                  >
                    Finalkan
                  </Button>
                  {detail.revision === 0 && (
                    <Button variant="ghost" onClick={() => setDeleteOpen(true)} disabled={busy}>
                      Hapus
                    </Button>
                  )}
                </>
              )}
              {isOwner && detail.status === "final" && (
                <Button variant="secondary" onClick={() => setReopenOpen(true)} disabled={busy}>
                  Buka Kembali
                </Button>
              )}
            </div>
          </div>

          <div className="flex gap-1 border-b border-border" role="tablist">
            {(
              [
                ["isi", "Isi Data"],
                ["neraca", "Neraca"],
                ["laba-rugi", "Laba Rugi"],
                ["riwayat", "Riwayat"],
              ] as [Tab, string][]
            ).map(([key, label]) => (
              <button
                key={key}
                role="tab"
                aria-selected={tab === key}
                onClick={() => setTab(key)}
                className={`-mb-px border-b-2 px-3 py-2 text-sm transition-colors
                  ${tab === key
                    ? "border-accent font-medium text-accent"
                    : "border-transparent text-text-secondary hover:text-accent"}`}
              >
                {label}
              </button>
            ))}
          </div>

          {tab === "isi" && (
            <SnapshotEditor
              accounts={accounts}
              excludeCode={detail.computed_account_code}
              draft={draft}
              onChange={setAmount}
              readOnly={readOnly}
              filter={filter}
              onFilter={setFilter}
              notes={notes}
              onNotes={(v) => {
                setNotes(v);
                setDirty(true);
              }}
              preview={preview}
              totals={totals}
              dirty={dirty}
            />
          )}

          {tab === "neraca" &&
            (dirty ? (
              <UnsavedNotice />
            ) : neraca ? (
              <NeracaPanel data={neraca} />
            ) : (
              <Card className="text-sm text-text-secondary">Memuat neraca…</Card>
            ))}

          {tab === "laba-rugi" &&
            (dirty ? (
              <UnsavedNotice />
            ) : labaRugi ? (
              <LabaRugiPanel data={labaRugi} />
            ) : (
              <Card className="text-sm text-text-secondary">Memuat laba rugi…</Card>
            ))}

          {tab === "riwayat" && <AuditPanel rows={audits} />}
        </>
      )}

      {/* ── Modal ── */}
      <Modal
        open={addOpen}
        onClose={() => setAddOpen(false)}
        title="Tambah Tahun Buku Historis"
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setAddOpen(false)} disabled={busy}>
              Batal
            </Button>
            <Button onClick={handleAdd} loading={busy}>
              Buka Tahun
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Input
            label="Tahun buku"
            type="number"
            value={newYear}
            onChange={(e) => setNewYear(e.target.value)}
          />
          <p className="text-xs text-text-tertiary">
            Hanya untuk tahun yang belum punya jurnal di sistem ini. Tahun yang bukunya
            sudah berjalan dilaporkan dari buku besar, bukan dari snapshot.
          </p>
        </div>
      </Modal>

      <ConfirmModal
        open={finalOpen}
        title={`Finalkan snapshot ${selected ?? ""}?`}
        confirmLabel="Finalkan"
        onCancel={() => setFinalOpen(false)}
        onConfirm={handleFinalize}
        loading={busy}
      >
        <p className="text-sm text-text-secondary">
          Angka tahun ini akan dikunci. Hanya owner yang bisa membukanya kembali, dan
          harus menyertakan alasan.
        </p>
      </ConfirmModal>

      <Modal
        open={reopenOpen}
        onClose={() => setReopenOpen(false)}
        title={`Buka kembali snapshot ${selected ?? ""}`}
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setReopenOpen(false)} disabled={busy}>
              Batal
            </Button>
            <Button onClick={handleReopen} loading={busy}>
              Buka Kembali
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <Textarea
            label="Alasan (minimal 10 karakter)"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            placeholder="Contoh: koreksi nilai persediaan hasil audit KAP"
          />
          <p className="text-xs text-text-tertiary">
            Alasan ini tersimpan permanen di riwayat snapshot.
          </p>
        </div>
      </Modal>

      <ConfirmModal
        open={deleteOpen}
        title={`Hapus draft ${selected ?? ""}?`}
        confirmLabel="Hapus"
        variant="danger"
        onCancel={() => setDeleteOpen(false)}
        onConfirm={handleDelete}
        loading={busy}
      >
        <p className="text-sm text-text-secondary">
          Draft ini belum pernah difinalkan, jadi boleh dihapus seluruhnya. Snapshot yang
          pernah final tidak bisa dihapus.
        </p>
      </ConfirmModal>
    </div>
  );
}

// ── Bagian layar ─────────────────────────────────────────────────────────────

function HistoricalBanner() {
  return (
    <div className="rounded-lg border border-warning/30 bg-warning-bg px-4 py-3">
      <p className="text-sm font-medium text-text-primary">Data historis — di luar buku besar</p>
      <p className="mt-1 text-sm text-text-secondary">
        Angka di halaman ini adalah laporan tahun-tahun sebelum sistem dipakai, dicatat apa
        adanya. Tidak ada jurnal yang dibuat, dan angka ini tidak ikut menyusun Neraca,
        Laba Rugi, dashboard, maupun tutup buku tahun berjalan.
      </p>
    </div>
  );
}

function UnsavedNotice() {
  return (
    <Card className="text-sm text-text-secondary">
      Ada perubahan yang belum disimpan. Simpan dulu di tab <strong>Isi Data</strong> —
      laporan selalu disusun dari angka yang tersimpan di server.
    </Card>
  );
}

interface EditorProps {
  accounts: Account[];
  excludeCode: string;
  draft: Record<number, string>;
  onChange: (accountId: number, value: string) => void;
  readOnly: boolean;
  filter: string;
  onFilter: (v: string) => void;
  notes: string;
  onNotes: (v: string) => void;
  preview: { aset: number; kanan: number; laba: number; selisih: number; seimbang: boolean };
  totals?: SnapshotDetail["totals"];
  dirty: boolean;
}

function SnapshotEditor({
  accounts,
  excludeCode,
  draft,
  onChange,
  readOnly,
  filter,
  onFilter,
  notes,
  onNotes,
  preview,
  totals,
  dirty,
}: EditorProps) {
  const q = filter.trim().toLowerCase();
  const visible = accounts.filter(
    (a) =>
      a.code !== excludeCode &&
      (!q || a.code.toLowerCase().includes(q) || a.name.toLowerCase().includes(q)),
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <input
          value={filter}
          onChange={(e) => onFilter(e.target.value)}
          placeholder="Cari akun…"
          className="w-64 rounded-lg border border-border bg-surface px-3.5 py-2.5 text-sm text-text-primary placeholder:text-text-tertiary outline-none transition-shadow focus:border-accent focus:ring-2 focus:ring-accent/25"
        />
        <p className="text-xs text-text-tertiary">
          Isi nominal dalam arah normal akun. Nilai negatif diperbolehkan (mis. akumulasi
          penyusutan, defisit). Baris kosong tidak disimpan.
        </p>
      </div>

      {SECTIONS.map((sec) => {
        const rows = visible.filter((a) => a.type === sec.type);
        if (rows.length === 0) return null;
        return (
          <Card key={sec.type} padding="none">
            <div className="border-b border-border px-4 py-2.5">
              <h3 className="text-sm font-semibold text-text-primary">{sec.label}</h3>
            </div>
            <table className="min-w-full text-sm">
              <tbody className="divide-y divide-border">
                {rows.map((a) => (
                  <tr key={a.id}>
                    <td className="w-24 px-4 py-1.5 text-text-secondary tabular">{a.code}</td>
                    <td className="px-4 py-1.5 text-text-primary">{a.name}</td>
                    <td className="w-52 px-4 py-1.5 text-right">
                      {readOnly ? (
                        <Rupiah value={draft[a.id] ?? "0"} />
                      ) : (
                        <input
                          value={displayAmount(draft[a.id] ?? "")}
                          onChange={(e) => onChange(a.id, e.target.value)}
                          placeholder="0"
                          inputMode="numeric"
                          className="num-right tabular w-full rounded-lg border border-border bg-surface px-3 py-1.5 text-sm text-text-primary outline-none transition-shadow focus:border-accent focus:ring-2 focus:ring-accent/25"
                        />
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        );
      })}

      <Card>
        <Textarea
          label="Catatan tahun ini"
          value={notes}
          onChange={(e) => onNotes(e.target.value)}
          rows={2}
          disabled={readOnly}
          placeholder="Contoh: sesuai laporan audit KAP, ditandatangani 30 April"
        />
      </Card>

      <div className="sticky bottom-0 rounded-lg border border-border bg-surface px-4 py-3 shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap gap-6 text-sm">
            <Stat label="Total Aset" value={dirty ? preview.aset : totals?.total_aset} />
            <Stat
              label="Kewajiban + Ekuitas + Laba"
              value={dirty ? preview.kanan : totals?.total_kewajiban_ekuitas}
            />
            <Stat
              label="Laba/Rugi Tahun Ini"
              value={dirty ? preview.laba : totals?.net_income}
            />
            <Stat label="Selisih" value={dirty ? preview.selisih : totals?.difference} />
          </div>
          {dirty ? (
            <Badge variant="accent">
              {preview.seimbang ? "Seimbang (belum disimpan)" : "Belum seimbang"}
            </Badge>
          ) : (
            <Badge variant={totals?.is_balanced ? "success" : "warning"}>
              {totals?.is_balanced ? "Seimbang" : "Belum seimbang"}
            </Badge>
          )}
        </div>
        {dirty && (
          <p className="mt-2 text-xs text-text-tertiary">
            Angka di atas pratinjau layar. Nilai yang berlaku dihitung server saat disimpan.
          </p>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value?: string | number }) {
  return (
    <div>
      <p className="text-xs text-text-tertiary">{label}</p>
      <Rupiah value={value ?? "0"} className="font-medium" />
    </div>
  );
}

function ReportTable({ title, rows }: { title: string; rows: { code: string; name: string; amount: string }[] }) {
  return (
    <Card padding="none">
      <div className="border-b border-border px-4 py-2.5">
        <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
      </div>
      {rows.length === 0 ? (
        <p className="px-4 py-3 text-sm text-text-tertiary">Tidak ada akun.</p>
      ) : (
        <table className="min-w-full text-sm">
          <tbody className="divide-y divide-border">
            {rows.map((l) => (
              <tr key={l.code}>
                <td className="w-24 px-4 py-1.5 text-text-secondary tabular">{l.code}</td>
                <td className="px-4 py-1.5 text-text-primary">{l.name}</td>
                <td className="w-44 px-4 py-1.5 text-right">
                  <Rupiah value={l.amount} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function TotalRow({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className="flex items-center justify-between px-4 py-2">
      <span className={`text-sm ${strong ? "font-semibold text-text-primary" : "text-text-secondary"}`}>
        {label}
      </span>
      <Rupiah value={value} className={strong ? "font-semibold" : ""} />
    </div>
  );
}

function NeracaPanel({ data }: { data: HistNeraca }) {
  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        <ReportTable title="Aset" rows={data.aset} />
        <div className="space-y-4">
          <ReportTable title="Kewajiban" rows={data.kewajiban} />
          <ReportTable title="Ekuitas" rows={data.ekuitas} />
        </div>
      </div>
      <Card padding="none">
        <TotalRow label="Total Aset" value={data.total_aset} strong />
        <TotalRow label="Total Kewajiban" value={data.total_kewajiban} />
        <TotalRow label="Total Ekuitas" value={data.total_ekuitas} />
        <TotalRow label="Laba/Rugi Tahun Berjalan" value={data.laba_rugi_tahun_berjalan} />
        <TotalRow label="Total Kewajiban + Ekuitas" value={data.total_kewajiban_ekuitas} strong />
        <div className="flex items-center justify-between border-t border-border px-4 py-2">
          <span className="text-sm text-text-secondary">Selisih</span>
          <span className="flex items-center gap-2">
            <Rupiah value={data.difference} />
            <Badge variant={data.is_balanced ? "success" : "danger"}>
              {data.is_balanced ? "Seimbang" : "Timpang"}
            </Badge>
          </span>
        </div>
      </Card>
      {data.notes && (
        <Card className="text-sm text-text-secondary">Catatan: {data.notes}</Card>
      )}
    </div>
  );
}

function LabaRugiPanel({ data }: { data: HistLabaRugi }) {
  return (
    <div className="space-y-4">
      <ReportTable title="Pendapatan" rows={data.pendapatan} />
      <ReportTable title="Beban" rows={data.beban} />
      <Card padding="none">
        <TotalRow label="Total Pendapatan" value={data.total_pendapatan} />
        <TotalRow label="Total Beban" value={data.total_beban} />
        <TotalRow label={`Laba/Rugi Bersih ${data.fiscal_year}`} value={data.laba_rugi_bersih} strong />
      </Card>
      {data.notes && (
        <Card className="text-sm text-text-secondary">Catatan: {data.notes}</Card>
      )}
    </div>
  );
}

const EVENT_LABEL: Record<SnapshotAudit["event"], string> = {
  created: "Tahun dibuka",
  saved: "Angka disimpan",
  finalized: "Difinalkan",
  reopened: "Dibuka kembali",
};

function AuditPanel({ rows }: { rows: SnapshotAudit[] }) {
  if (rows.length === 0) {
    return <Card className="text-sm text-text-secondary">Belum ada riwayat.</Card>;
  }
  return (
    <Card padding="none">
      <table className="min-w-full text-sm">
        <thead className="bg-bg">
          <tr>
            <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Waktu</th>
            <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Kejadian</th>
            <th className="px-4 py-2.5 text-right font-medium text-text-secondary">Total Aset</th>
            <th className="px-4 py-2.5 text-right font-medium text-text-secondary">Laba/Rugi</th>
            <th className="px-4 py-2.5 text-right font-medium text-text-secondary">Baris</th>
            <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Alasan</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {rows.map((a) => (
            <tr key={a.id}>
              <td className="px-4 py-2 text-text-secondary">
                {new Date(a.created_at).toLocaleString("id-ID")}
              </td>
              <td className="px-4 py-2 text-text-primary">
                {EVENT_LABEL[a.event]}
                {a.event === "finalized" && (
                  <span className="ml-2 text-xs text-text-tertiary">revisi {a.revision}</span>
                )}
              </td>
              <td className="px-4 py-2 text-right">
                <Rupiah value={a.total_assets} />
              </td>
              <td className="px-4 py-2 text-right">
                <Rupiah value={a.net_income} />
              </td>
              <td className="px-4 py-2 text-right tabular text-text-secondary">{a.line_count}</td>
              <td className="px-4 py-2 text-text-secondary">{a.reason ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}
