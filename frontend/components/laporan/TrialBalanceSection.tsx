"use client";

import { useRouter, useSearchParams, usePathname } from "next/navigation";
import type { TrialBalance, LedgerEntry } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { ReportEmptyCTA } from "@/components/laporan/ReportEmptyCTA";

const ACCOUNT_TYPE_LABELS: Record<string, string> = {
  asset: "Aset",
  liability: "Kewajiban",
  equity: "Ekuitas",
  revenue: "Pendapatan",
  expense: "Beban",
};

interface Props {
  data: TrialBalance | null;
  error: boolean;
  ledger: LedgerEntry[] | null;
  ledgerError: boolean;
  selectedAccountId: number | null;
  selectedAccountName: string | null;
}

export function TrialBalanceSection({ data, error, ledger, ledgerError, selectedAccountId, selectedAccountName }: Props) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  function selectAccount(accountId: number, accountName: string) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("tab", "neraca-saldo");
    params.set("account_id", String(accountId));
    params.set("account_name", accountName);
    router.push(`${pathname}?${params.toString()}`);
  }

  function clearAccount() {
    const params = new URLSearchParams(searchParams.toString());
    params.delete("account_id");
    params.delete("account_name");
    router.push(`${pathname}?${params.toString()}`);
  }

  if (error) {
    return (
      <Card>
        <p className="text-sm text-danger">Gagal memuat Neraca Saldo. Coba lagi nanti.</p>
      </Card>
    );
  }
  if (!data || (data.rows?.length ?? 0) === 0) {
    return <ReportEmptyCTA kind="neraca-saldo" />;
  }

  const isBalanced = data.total_debit === data.total_credit;

  return (
    <div className="space-y-6">
      {/* Balance check */}
      <div className={`rounded-lg px-4 py-3 flex items-center gap-3 text-sm font-medium
        ${isBalanced
          ? "bg-success-bg border border-success/30 text-success"
          : "bg-danger-bg border border-danger/30 text-danger"}`}>
        <span className="text-lg">{isBalanced ? "✓" : "✗"}</span>
        {isBalanced
          ? "Neraca Saldo seimbang — total debit = total kredit"
          : "PERINGATAN: Total debit ≠ total kredit. Ada kesalahan posting jurnal."}
      </div>

      {/* Trial Balance Table */}
      <Card padding="none">
        <CardHeader className="px-5 pt-5 pb-0">
          <CardTitle>Neraca Saldo</CardTitle>
          <p className="text-xs text-text-secondary mt-1">Klik baris akun untuk melihat buku besar detail</p>
        </CardHeader>
        {(data.rows ?? []).length === 0 ? (
          <EmptyState title="Tidak ada transaksi" />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <Th>Kode</Th>
                <Th>Nama Akun</Th>
                <Th>Tipe</Th>
                <Th right>Total Debit</Th>
                <Th right>Total Kredit</Th>
                <Th right>Saldo</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {(data.rows ?? []).map(row => (
                <TableRow
                  key={row.account_id}
                  onClick={() => selectAccount(row.account_id, row.account_name)}
                  className={`cursor-pointer hover:bg-border-subtle/40 transition-colors
                    ${selectedAccountId === row.account_id ? "bg-accent-light" : ""}`}
                >
                  <Td><span className="font-mono text-xs text-text-tertiary">{row.account_code}</span></Td>
                  <Td>
                    <span className={`text-sm ${selectedAccountId === row.account_id ? "font-semibold text-accent" : ""}`}>
                      {row.account_name}
                    </span>
                  </Td>
                  <Td>
                    <span className="text-xs text-text-secondary">
                      {ACCOUNT_TYPE_LABELS[row.account_type] ?? row.account_type}
                    </span>
                  </Td>
                  <Td right><Rupiah value={row.total_debit} colorSign={false} /></Td>
                  <Td right><Rupiah value={row.total_credit} colorSign={false} /></Td>
                  <Td right>
                    <span className={row.balance.startsWith("-") ? "text-danger" : ""}>
                      <Rupiah value={row.balance} colorSign={false} />
                    </span>
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {/* Totals footer */}
        {/* Footer total: tabelnya bisa digulir horizontal, jadi grid 6 kolom
            tidak pernah benar-benar sejajar. Dipakai baris ringkas berlabel —
            terbaca di layar sempit maupun lebar. */}
        <div className="flex flex-wrap items-center gap-x-8 gap-y-2 px-4 py-3 border-t
          border-border bg-border-subtle/30 text-xs font-semibold">
          <span className="text-text-secondary uppercase tracking-wide">Total</span>
          <span className="ml-auto flex items-baseline gap-2">
            <span className="font-medium text-text-secondary">Debit</span>
            <span className="tabular"><Rupiah value={data.total_debit} colorSign={false} /></span>
          </span>
          <span className="flex items-baseline gap-2">
            <span className="font-medium text-text-secondary">Kredit</span>
            <span className="tabular"><Rupiah value={data.total_credit} colorSign={false} /></span>
          </span>
        </div>
      </Card>

      {/* General Ledger drill-down */}
      {selectedAccountId !== null && (
        <Card padding="none">
          <CardHeader className="px-5 pt-5 pb-3">
            <div className="flex items-center justify-between">
              <CardTitle>
                Buku Besar — {selectedAccountName ?? `Akun #${selectedAccountId}`}
              </CardTitle>
              <button
                onClick={clearAccount}
                className="text-xs text-text-secondary hover:text-accent transition-colors"
              >
                ✕ Tutup
              </button>
            </div>
          </CardHeader>
          {ledgerError ? (
            <p className="text-sm text-danger px-5 pb-4">Gagal memuat buku besar.</p>
          ) : !ledger || ledger.length === 0 ? (
            <EmptyState title="Tidak ada transaksi untuk akun ini" />
          ) : (
            <Table>
              <TableHead>
                <TableRow>
                  <Th>Tanggal</Th>
                  <Th>Referensi</Th>
                  <Th>Deskripsi</Th>
                  <Th right>Debit</Th>
                  <Th right>Kredit</Th>
                  <Th right>Saldo</Th>
                </TableRow>
              </TableHead>
              <TableBody>
                {ledger.map((entry, i) => (
                  <TableRow key={i}>
                    <Td><Tanggal value={entry.date} /></Td>
                    <Td><span className="font-mono text-xs">{entry.reference}</span></Td>
                    <Td>{entry.description}</Td>
                    <Td right>
                      {entry.debit !== "0" && entry.debit !== "0.0000"
                        ? <Rupiah value={entry.debit} colorSign={false} />
                        : <span className="text-text-tertiary">—</span>
                      }
                    </Td>
                    <Td right>
                      {entry.credit !== "0" && entry.credit !== "0.0000"
                        ? <Rupiah value={entry.credit} colorSign={false} />
                        : <span className="text-text-tertiary">—</span>
                      }
                    </Td>
                    <Td right>
                      <span className={entry.balance.startsWith("-") ? "text-danger" : ""}>
                        <Rupiah value={entry.balance} colorSign={false} />
                      </span>
                    </Td>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
      )}
    </div>
  );
}
