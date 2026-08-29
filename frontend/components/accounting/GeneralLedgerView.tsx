"use client";

// PS-5 — Buku Besar (General Ledger): akun → transaksi (running balance) →
// jurnal sumber. Drill-down inti PS-6: tidak ada angka yang buntu.

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Input, Select } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { fetchAccounts } from "@/lib/api/ledger";
import { fetchGeneralLedger } from "@/lib/api/reports";
import { ExportPdfButton } from "@/components/laporan/ExportPdfButton";
import type { Account, LedgerEntry } from "@/lib/types/api";
import { dateToLocalStr, todayLocalStr } from "@/lib/date";

function monthStartISO(): string {
  const d = new Date();
  return dateToLocalStr(new Date(d.getFullYear(), d.getMonth(), 1));
}

function todayISO(): string {
  return todayLocalStr();
}

export function GeneralLedgerView({ token, initialAccountId }: { token: string; initialAccountId?: number }) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [accountId, setAccountId] = useState<string>(initialAccountId ? String(initialAccountId) : "");
  const [from, setFrom] = useState(monthStartISO());
  const [to, setTo] = useState(todayISO());
  const [search, setSearch] = useState("");
  const [entries, setEntries] = useState<LedgerEntry[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    fetchAccounts(token).then(setAccounts).catch(() => {});
  }, [token]);

  const load = useCallback(async () => {
    if (!accountId) return;
    setLoading(true);
    try {
      const rows = await fetchGeneralLedger(token, parseInt(accountId, 10), { from, to });
      setEntries(rows ?? []);
    } catch {
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, [token, accountId, from, to]);

  useEffect(() => {
    load();
  }, [load]);

  const account = useMemo(
    () => accounts.find((a) => String(a.id) === accountId),
    [accounts, accountId],
  );

  const visible = useMemo(() => {
    if (!search.trim()) return entries;
    const q = search.toLowerCase();
    return entries.filter(
      (e) =>
        e.description.toLowerCase().includes(q) ||
        (e.reference ?? "").toLowerCase().includes(q),
    );
  }, [entries, search]);

  const closing = entries.length > 0 ? entries[entries.length - 1].balance : "0";

  return (
    <div className="space-y-4">
      {/* Kontrol */}
      <Card padding="sm">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <Select
            label="Akun"
            value={accountId}
            onChange={(e) => setAccountId(e.target.value)}
          >
            <option value="">— pilih akun —</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.code} — {a.name}
              </option>
            ))}
          </Select>
          <Input label="Dari" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
          <Input label="Sampai" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
          <Input
            label="Cari deskripsi / referensi"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="cth: BAST, booking, CX-…"
          />
        </div>
      </Card>

      {!accountId ? (
        <EmptyState
          title="Pilih akun untuk melihat buku besar"
          description="Setiap baris bisa diklik sampai ke jurnal sumber — tidak ada angka yang buntu."
        />
      ) : loading ? (
        <Card padding="sm">
          <div className="animate-pulse space-y-2">
            {[...Array(6)].map((_, i) => <div key={i} className="h-8 rounded bg-border-subtle" />)}
          </div>
        </Card>
      ) : entries.length === 0 ? (
        <EmptyState
          title="Tidak ada mutasi pada periode ini"
          description={`Akun ${account?.code ?? ""} tidak memiliki transaksi posted antara ${from} dan ${to}.`}
        />
      ) : (
        <Card padding="sm" className="overflow-x-auto">
          <div className="mb-2 flex items-center justify-between flex-wrap gap-2">
            <p className="text-sm font-semibold text-text-primary">
              {account?.code} — {account?.name}
            </p>
            <div className="flex items-center gap-3">
              <p className="text-sm text-text-secondary">
                Saldo akhir:{" "}
                <span className="font-semibold text-text-primary tabular-nums">
                  <Rupiah value={closing} colorSign={false} />
                </span>
              </p>
              <ExportPdfButton
                token={token}
                report="general-ledger"
                params={{ account_id: accountId, from, to }}
                label="Export PDF"
              />
            </div>
          </div>
          {/* Buku besar 6 kolom — digulir horizontal di layar sempit, bukan
              memaksa halaman melebar. */}
          <div className="overflow-x-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[640px]">
            <thead>
              <tr className="border-b border-border text-left text-xs text-text-secondary">
                <th className="py-2 pr-3">Tanggal</th>
                <th className="py-2 pr-3">Deskripsi</th>
                <th className="py-2 pr-3">Ref</th>
                <th className="py-2 pr-3 text-right">Debit</th>
                <th className="py-2 pr-3 text-right">Kredit</th>
                <th className="py-2 pr-3 text-right">Saldo</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {visible.map((e, i) => (
                <tr key={`${e.entry_id}-${i}`} className="border-b border-border last:border-0 hover:bg-border-subtle/40">
                  <td className="py-2 pr-3 whitespace-nowrap"><Tanggal value={String(e.date)} /></td>
                  <td className="py-2 pr-3">{e.description}</td>
                  <td className="py-2 pr-3 text-xs text-text-secondary">{e.reference || "—"}</td>
                  <td className="py-2 pr-3 text-right tabular-nums">
                    {e.debit !== "0" && <Rupiah value={e.debit} colorSign={false} />}
                  </td>
                  <td className="py-2 pr-3 text-right tabular-nums">
                    {e.credit !== "0" && <Rupiah value={e.credit} colorSign={false} />}
                  </td>
                  <td className="py-2 pr-3 text-right tabular-nums font-medium">
                    <Rupiah value={e.balance} colorSign={false} />
                  </td>
                  <td className="py-2 text-right">
                    <Link
                      href={`/accounting/jurnal/${e.entry_id}`}
                      className="text-xs text-accent hover:underline whitespace-nowrap"
                    >
                      jurnal →
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        </Card>
      )}
    </div>
  );
}
