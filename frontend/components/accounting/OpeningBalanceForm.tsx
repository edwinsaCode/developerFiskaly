"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";
import { createJournal, postJournal } from "@/lib/api/ledger";
import type { Account } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
  accounts: Account[];
  readOnly: boolean;
}

interface BalanceLine {
  accountId: number;
  debit: string;
  credit: string;
}

function parseAmt(s: string): number {
  const n = parseFloat(s.replace(/[^0-9.]/g, ""));
  return isNaN(n) ? 0 : n;
}

function sumLines(lines: BalanceLine[], side: "debit" | "credit"): number {
  return lines.reduce((acc, l) => acc + parseAmt(l[side]), 0);
}

function fmtRupiah(n: number): string {
  return n.toLocaleString("id-ID");
}

export function OpeningBalanceForm({ token, accounts, readOnly }: Props) {
  const { toast } = useToast();
  const router = useRouter();
  const [date, setDate] = useState(todayLocalStr());
  const [lines, setLines] = useState<BalanceLine[]>(
    accounts.map((a) => ({ accountId: a.id, debit: "", credit: "" }))
  );
  const [submitting, setSubmitting] = useState(false);

  const totalDebit  = sumLines(lines, "debit");
  const totalCredit = sumLines(lines, "credit");
  const balanced    = Math.abs(totalDebit - totalCredit) < 0.0001;

  function setLine(idx: number, field: "debit" | "credit", value: string) {
    setLines((prev) =>
      prev.map((l, i) => (i === idx ? { ...l, [field]: value } : l))
    );
  }

  async function handleSubmit() {
    if (!balanced) {
      toast("Total Debit harus sama dengan Total Kredit.", "error");
      return;
    }
    const activeLines = lines.filter(
      (l) => parseAmt(l.debit) > 0 || parseAmt(l.credit) > 0
    );
    if (activeLines.length < 2) {
      toast("Minimal 2 baris dengan nilai > 0.", "error");
      return;
    }

    setSubmitting(true);
    try {
      const entry = await createJournal(token, {
        date,
        description: "Saldo Awal",
        source: "opening_balance",
        lines: activeLines.map((l) => ({
          account_id: l.accountId,
          debit:  parseAmt(l.debit)  > 0 ? String(parseAmt(l.debit))  : "0",
          credit: parseAmt(l.credit) > 0 ? String(parseAmt(l.credit)) : "0",
        })),
      });
      await postJournal(token, entry.id);
      toast("Saldo awal berhasil disimpan dan diposting.", "success");
      setLines(accounts.map((a) => ({ accountId: a.id, debit: "", credit: "" })));
      // Invalidate data server component induk agar daftar/saldo terbaru muncul
      // tanpa reload manual.
      router.refresh();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Gagal menyimpan saldo awal";
      toast(msg, "error");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      {/* Date picker */}
      <div className="flex items-center gap-4">
        <label className="text-sm font-medium text-text-secondary w-28">Tanggal Saldo</label>
        {readOnly ? (
          <span className="text-sm">{date}</span>
        ) : (
          <input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            className="border border-border rounded-md px-3 py-1.5 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/30"
          />
        )}
      </div>

      {readOnly && (
        <div className="rounded-md bg-bg border border-border px-4 py-3 text-sm text-text-secondary">
          Anda login sebagai <strong>viewer</strong>. Hanya owner dan accountant yang dapat input saldo awal.
        </div>
      )}

      {/* Account table */}
      <div className="overflow-x-auto rounded border border-border">
        <table className="min-w-full text-sm">
          <thead className="bg-bg">
            <tr>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary w-24">Kode</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Nama Akun</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary w-20">Tipe</th>
              <th className="px-4 py-2.5 text-right font-medium text-text-secondary w-36">Debit (Rp)</th>
              <th className="px-4 py-2.5 text-right font-medium text-text-secondary w-36">Kredit (Rp)</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {accounts.map((acc, idx) => {
              const line = lines[idx];
              return (
                <tr key={acc.id} className="hover:bg-bg/40">
                  <td className="px-4 py-2 font-mono text-xs text-text-secondary">{acc.code}</td>
                  <td className="px-4 py-2">{acc.name}</td>
                  <td className="px-4 py-2 text-xs text-text-secondary capitalize">{acc.type}</td>
                  <td className="px-4 py-2 text-right">
                    {readOnly ? (
                      <span className="font-mono text-xs">{line?.debit || "—"}</span>
                    ) : (
                      <input
                        type="number"
                        min="0"
                        step="1"
                        value={line?.debit ?? ""}
                        onChange={(e) => setLine(idx, "debit", e.target.value)}
                        placeholder="0"
                        className="w-32 text-right border border-border rounded px-2 py-1 text-sm bg-surface focus:outline-none focus:ring-1 focus:ring-accent/40"
                      />
                    )}
                  </td>
                  <td className="px-4 py-2 text-right">
                    {readOnly ? (
                      <span className="font-mono text-xs">{line?.credit || "—"}</span>
                    ) : (
                      <input
                        type="number"
                        min="0"
                        step="1"
                        value={line?.credit ?? ""}
                        onChange={(e) => setLine(idx, "credit", e.target.value)}
                        placeholder="0"
                        className="w-32 text-right border border-border rounded px-2 py-1 text-sm bg-surface focus:outline-none focus:ring-1 focus:ring-accent/40"
                      />
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
          <tfoot className="bg-bg border-t-2 border-border">
            <tr>
              <td colSpan={3} className="px-4 py-2.5 text-sm font-semibold">Total</td>
              <td className={`px-4 py-2.5 text-right font-mono text-sm font-semibold ${!balanced && totalDebit > 0 ? "text-danger" : ""}`}>
                Rp {fmtRupiah(totalDebit)}
              </td>
              <td className={`px-4 py-2.5 text-right font-mono text-sm font-semibold ${!balanced && totalCredit > 0 ? "text-danger" : ""}`}>
                Rp {fmtRupiah(totalCredit)}
              </td>
            </tr>
          </tfoot>
        </table>
      </div>

      {/* Balance status */}
      {(totalDebit > 0 || totalCredit > 0) && (
        <div className={`rounded-md px-4 py-3 text-sm ${balanced ? "bg-success-bg text-success" : "bg-danger-bg text-danger"}`}>
          {balanced
            ? "Seimbang — Total Debit = Total Kredit."
            : `Tidak seimbang — selisih Rp ${fmtRupiah(Math.abs(totalDebit - totalCredit))}.`}
        </div>
      )}

      {!readOnly && (
        <div className="flex justify-end">
          <Button
            onClick={handleSubmit}
            disabled={submitting || !balanced || (totalDebit === 0 && totalCredit === 0)}
            loading={submitting}
          >
            {submitting ? "Menyimpan..." : "Simpan & Posting Saldo Awal"}
          </Button>
        </div>
      )}
    </div>
  );
}
