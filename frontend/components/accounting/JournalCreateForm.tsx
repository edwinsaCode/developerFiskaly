"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchAccounts, createJournal, postJournal, type CreateJournalLineInput } from "@/lib/api/ledger";
import type { Account } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
}

interface LineRow {
  account_id: number | "";
  debit: string;
  credit: string;
  description: string;
}

const emptyLine = (): LineRow => ({ account_id: "", debit: "", credit: "", description: "" });

function parseAmount(s: string): number {
  const n = parseFloat(s.replace(/[^0-9.]/g, ""));
  return isNaN(n) ? 0 : n;
}

function sumField(lines: LineRow[], field: "debit" | "credit"): number {
  return lines.reduce((sum, l) => sum + parseAmount(l[field]), 0);
}

function formatRupiah(n: number): string {
  return "Rp " + n.toLocaleString("id-ID", { minimumFractionDigits: 0, maximumFractionDigits: 0 });
}

export function JournalCreateForm({ token }: Props) {
  const router = useRouter();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [date, setDate] = useState(() => todayLocalStr());
  const [description, setDescription] = useState("");
  const [reference, setReference] = useState("");
  const [lines, setLines] = useState<LineRow[]>([emptyLine(), emptyLine()]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchAccounts(token)
      .then(data => setAccounts(data.filter(a => a.is_active)))
      .catch(() => setError("Gagal memuat daftar akun."));
  }, [token]);

  function updateLine(idx: number, field: keyof LineRow, value: string | number) {
    setLines(prev => {
      const next = [...prev];
      next[idx] = { ...next[idx], [field]: value };
      return next;
    });
  }

  function addLine() {
    setLines(prev => [...prev, emptyLine()]);
  }

  function removeLine(idx: number) {
    if (lines.length <= 2) return;
    setLines(prev => prev.filter((_, i) => i !== idx));
  }

  const totalDebit = sumField(lines, "debit");
  const totalCredit = sumField(lines, "credit");
  const isBalanced = Math.abs(totalDebit - totalCredit) < 0.001 && totalDebit > 0;
  const isValid = isBalanced && description.trim() !== "" && date !== "" &&
    lines.every(l => l.account_id !== "");

  function buildPayload(): { date: string; description: string; reference?: string; lines: CreateJournalLineInput[] } {
    return {
      date,
      description: description.trim(),
      reference: reference.trim() || undefined,
      lines: lines.map(l => ({
        account_id: Number(l.account_id),
        debit: parseAmount(l.debit).toFixed(4),
        credit: parseAmount(l.credit).toFixed(4),
        description: l.description,
      })),
    };
  }

  async function handleSaveDraft() {
    if (!isValid) return;
    setSaving(true);
    setError(null);
    try {
      const entry = await createJournal(token, buildPayload());
      router.push(`/accounting/jurnal/${entry.id}`);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Terjadi kesalahan";
      setError(msg);
    } finally {
      setSaving(false);
    }
  }

  async function handleSaveAndPost() {
    if (!isValid) return;
    setSaving(true);
    setError(null);
    try {
      const entry = await createJournal(token, buildPayload());
      await postJournal(token, entry.id);
      router.push(`/accounting/jurnal/${entry.id}`);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Terjadi kesalahan";
      setError(msg);
    } finally {
      setSaving(false);
    }
  }

  return (
    // Form dokumen: dibatasi lebarnya agar baris input tidak terlalu panjang,
    // tapi dipusatkan — bukan menempel kiri.
    <div className="space-y-6 mx-auto w-full max-w-5xl">
      {/* Header fields */}
      <div className="bg-surface rounded-xl border border-border p-5 grid grid-cols-1 md:grid-cols-3 gap-4">
        <div>
          <label className="block text-sm font-medium text-text-primary mb-1">Tanggal *</label>
          <input
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
            className="w-full border border-border rounded-lg px-3 py-2 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>
        <div className="md:col-span-2">
          <label className="block text-sm font-medium text-text-primary mb-1">Deskripsi *</label>
          <input
            type="text"
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder="Keterangan jurnal..."
            className="w-full border border-border rounded-lg px-3 py-2 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-text-primary mb-1">Referensi</label>
          <input
            type="text"
            value={reference}
            onChange={e => setReference(e.target.value)}
            placeholder="No. faktur, dll. (opsional)"
            className="w-full border border-border rounded-lg px-3 py-2 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>
      </div>

      {/* Lines table */}
      <div className="bg-surface rounded-xl border border-border overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-bg border-b border-border text-text-secondary">
                <th className="px-3 py-2 text-left font-medium w-8">#</th>
                <th className="px-3 py-2 text-left font-medium">Akun *</th>
                <th className="px-3 py-2 text-right font-medium w-40">Debit</th>
                <th className="px-3 py-2 text-right font-medium w-40">Kredit</th>
                <th className="px-3 py-2 text-left font-medium">Keterangan</th>
                <th className="px-3 py-2 w-8" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {lines.map((line, idx) => (
                <tr key={idx}>
                  <td className="px-3 py-2 text-text-secondary text-center">{idx + 1}</td>
                  <td className="px-3 py-2">
                    <select
                      value={line.account_id}
                      onChange={e => updateLine(idx, "account_id", e.target.value)}
                      className="w-full border border-border rounded-lg px-2 py-1.5 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
                    >
                      <option value="">— Pilih akun —</option>
                      {accounts.map(a => (
                        <option key={a.id} value={a.id}>
                          {a.code} — {a.name}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="number"
                      min="0"
                      step="1"
                      value={line.debit}
                      onChange={e => updateLine(idx, "debit", e.target.value)}
                      placeholder="0"
                      className="w-full border border-border rounded-lg px-2 py-1.5 text-sm text-right bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="number"
                      min="0"
                      step="1"
                      value={line.credit}
                      onChange={e => updateLine(idx, "credit", e.target.value)}
                      placeholder="0"
                      className="w-full border border-border rounded-lg px-2 py-1.5 text-sm text-right bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
                    />
                  </td>
                  <td className="px-3 py-2">
                    <input
                      type="text"
                      value={line.description}
                      onChange={e => updateLine(idx, "description", e.target.value)}
                      placeholder="Keterangan baris (opsional)"
                      className="w-full border border-border rounded-lg px-2 py-1.5 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
                    />
                  </td>
                  <td className="px-3 py-2">
                    <button
                      onClick={() => removeLine(idx)}
                      disabled={lines.length <= 2}
                      className="text-text-secondary hover:text-danger disabled:opacity-30 transition-colors"
                      aria-label="Hapus baris"
                    >
                      ×
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot className="border-t-2 border-border bg-bg">
              <tr>
                <td colSpan={2} className="px-3 py-2">
                  <button
                    onClick={addLine}
                    className="text-accent text-sm hover:underline"
                  >
                    + Tambah baris
                  </button>
                </td>
                <td className="px-3 py-2 text-right font-semibold text-text-primary font-mono">
                  {formatRupiah(totalDebit)}
                </td>
                <td className={`px-3 py-2 text-right font-semibold font-mono ${isBalanced ? "text-success" : "text-danger"}`}>
                  {formatRupiah(totalCredit)}
                </td>
                <td colSpan={2} className="px-3 py-2">
                  {totalDebit > 0 && !isBalanced && (
                    <span className="text-danger text-xs">Selisih: {formatRupiah(Math.abs(totalDebit - totalCredit))}</span>
                  )}
                  {isBalanced && (
                    <span className="text-success text-xs">Balanced</span>
                  )}
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="rounded-lg bg-danger-bg border border-danger/30 px-4 py-3 text-sm text-danger">
          {error}
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-3">
        <button
          onClick={handleSaveDraft}
          disabled={!isValid || saving}
          className="px-5 py-2 rounded-lg border border-border text-sm font-medium text-text-primary hover:bg-bg disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {saving ? "Menyimpan..." : "Simpan Draft"}
        </button>
        <button
          onClick={handleSaveAndPost}
          disabled={!isValid || saving}
          className="px-5 py-2 rounded-lg bg-accent text-white text-sm font-medium hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {saving ? "Memproses..." : "Simpan & Posting"}
        </button>
        <button
          onClick={() => router.back()}
          className="px-4 py-2 text-sm text-text-secondary hover:text-accent transition-colors"
        >
          Batal
        </button>
        {!isBalanced && totalDebit > 0 && (
          <span className="text-xs text-text-secondary">Jurnal harus balanced sebelum bisa disimpan</span>
        )}
      </div>
    </div>
  );
}
