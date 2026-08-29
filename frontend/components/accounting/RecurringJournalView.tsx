"use client";

// PS-5 — Jurnal Berulang: template bulanan (sewa, gaji, penyusutan) yang
// dieksekusi menjadi jurnal POSTED via engine (balanced dijamin backend).

import { useCallback, useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { useToast } from "@/components/ui/Toast";
import { ApiError, apiFetch } from "@/lib/api/client";
import { fetchAccounts } from "@/lib/api/ledger";
import type { Account } from "@/lib/types/api";

interface RecurringLine {
  account_code: string;
  debit: string;
  credit: string;
  description?: string;
}

interface RecurringJournal {
  id: number;
  name: string;
  description?: string;
  day_of_month: number;
  lines: RecurringLine[];
  is_active: boolean;
  last_run_ym?: string;
  // S9/R-9: total template dihitung backend.
  total: string;
}

export function RecurringJournalView({ token }: { token: string }) {
  const { toast } = useToast();
  const [rows, setRows] = useState<RecurringJournal[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [showForm, setShowForm] = useState(false);

  const [fName, setFName] = useState("");
  const [fDay, setFDay] = useState("1");
  const [fLines, setFLines] = useState<RecurringLine[]>([
    { account_code: "", debit: "", credit: "" },
    { account_code: "", debit: "", credit: "" },
  ]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const rs = await apiFetch<RecurringJournal[]>(`/ledger/recurring`, { token });
      setRows(rs ?? []);
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    refresh();
    fetchAccounts(token).then(setAccounts).catch(() => {});
  }, [refresh, token]);

  async function handleRunDue() {
    setBusy(true);
    try {
      const r = await apiFetch<{ created: number; warning?: string }>(`/ledger/recurring/run-due`, {
        method: "POST", body: {}, token,
      });
      toast(
        r.created > 0
          ? `${r.created} jurnal berulang diposting bulan ini`
          : "Tidak ada template jatuh tempo — semua sudah dieksekusi",
        "success",
      );
      if (r.warning) toast(r.warning, "error");
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memproses", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleCreate() {
    setBusy(true);
    try {
      await apiFetch(`/ledger/recurring`, {
        method: "POST",
        body: {
          name: fName.trim(),
          day_of_month: parseInt(fDay, 10),
          lines: fLines
            .filter((l) => l.account_code)
            .map((l) => ({ ...l, debit: l.debit || "0", credit: l.credit || "0" })),
        },
        token,
      });
      toast("Template dibuat — akan diposting otomatis via [Jalankan]", "success");
      setShowForm(false);
      setFName("");
      setFLines([
        { account_code: "", debit: "", credit: "" },
        { account_code: "", debit: "", credit: "" },
      ]);
      await refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat template", "error");
    } finally {
      setBusy(false);
    }
  }

  function setLine(i: number, patch: Partial<RecurringLine>) {
    setFLines((prev) => prev.map((l, k) => (k === i ? { ...l, ...patch } : l)));
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <p className="text-sm text-text-secondary">
          Beban rutin (sewa, gaji, penyusutan) diposting otomatis tiap bulan — sekali setup.
        </p>
        <div className="flex gap-2">
          <Button size="sm" variant="secondary" onClick={handleRunDue} loading={busy}>
            Jalankan yang Jatuh Tempo
          </Button>
          <Button size="sm" onClick={() => setShowForm(true)}>+ Template</Button>
        </div>
      </div>

      {loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(3)].map((_, i) => <div key={i} className="h-9 rounded bg-border-subtle" />)}
          </div>
        </Card>
      ) : rows.length === 0 ? (
        <EmptyState
          title="Belum ada jurnal berulang"
          description="Contoh: Sewa kantor Rp 15jt tiap tanggal 1 — Dr Beban Sewa / Cr Bank. Setup sekali, klik Jalankan tiap awal bulan (atau via cron)."
          action={<Button size="sm" onClick={() => setShowForm(true)}>+ Template Pertama</Button>}
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-text-secondary">
                <th className="py-2 pr-3">Nama</th>
                <th className="py-2 pr-3">Tanggal</th>
                <th className="py-2 pr-3">Baris</th>
                <th className="py-2 pr-3 text-right">Total</th>
                <th className="py-2 pr-3">Terakhir Jalan</th>
                <th className="py-2 pr-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                // S9/R-9: total dari backend — FE tidak menjumlah lagi.
                const total = r.total ?? "0";
                return (
                  <tr key={r.id} className="border-b border-border last:border-0">
                    <td className="py-2 pr-3 font-medium">{r.name}</td>
                    <td className="py-2 pr-3">tiap tgl {r.day_of_month}</td>
                    <td className="py-2 pr-3 text-xs text-text-secondary">
                      {r.lines.map((l) => l.account_code).join(" · ")}
                    </td>
                    <td className="py-2 pr-3 text-right tabular-nums">
                      <Rupiah value={total} colorSign={false} />
                    </td>
                    <td className="py-2 pr-3 text-xs">{r.last_run_ym || "belum pernah"}</td>
                    <td className="py-2 pr-3">
                      <button
                        onClick={async () => {
                          try {
                            await apiFetch(`/ledger/recurring/${r.id}/active`, {
                              method: "PUT", body: { active: !r.is_active }, token,
                            });
                            await refresh();
                          } catch (err) {
                            toast(err instanceof ApiError ? err.message : "Gagal", "error");
                          }
                        }}
                      >
                        {r.is_active
                          ? <Badge variant="success">Aktif</Badge>
                          : <Badge variant="neutral">Nonaktif</Badge>}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      )}

      {/* Modal template baru */}
      <Modal
        open={showForm}
        onClose={() => setShowForm(false)}
        title="Template Jurnal Berulang"
        size="lg"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowForm(false)} disabled={busy}>Batal</Button>
            <Button onClick={handleCreate} loading={busy} disabled={!fName.trim()}>Simpan</Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <Input label="Nama" required value={fName} onChange={(e) => setFName(e.target.value)}
              placeholder="cth: Sewa Kantor Bulanan" />
            <Select label="Tanggal posting tiap bulan" value={fDay} onChange={(e) => setFDay(e.target.value)}>
              {Array.from({ length: 28 }, (_, i) => (
                <option key={i + 1} value={i + 1}>tanggal {i + 1}</option>
              ))}
            </Select>
          </div>

          <div className="space-y-2">
            <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary">
              Baris Jurnal (Σ debit harus = Σ kredit)
            </p>
            {fLines.map((l, i) => (
              <div key={i} className="grid grid-cols-12 gap-2 items-end">
                <div className="col-span-6">
                  <Select
                    label={i === 0 ? "Akun" : undefined}
                    value={l.account_code}
                    onChange={(e) => setLine(i, { account_code: e.target.value })}
                  >
                    <option value="">— akun —</option>
                    {accounts.map((a) => (
                      <option key={a.id} value={a.code}>{a.code} — {a.name}</option>
                    ))}
                  </Select>
                </div>
                <div className="col-span-3">
                  <RupiahInput
                    label={i === 0 ? "Debit" : undefined}
                    value={l.debit}
                    onChange={(v) => setLine(i, { debit: v, credit: v ? "" : l.credit })}
                  />
                </div>
                <div className="col-span-3">
                  <RupiahInput
                    label={i === 0 ? "Kredit" : undefined}
                    value={l.credit}
                    onChange={(v) => setLine(i, { credit: v, debit: v ? "" : l.debit })}
                  />
                </div>
              </div>
            ))}
            <button
              type="button"
              className="text-xs text-accent hover:underline"
              onClick={() => setFLines((p) => [...p, { account_code: "", debit: "", credit: "" }])}
            >
              + tambah baris
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
