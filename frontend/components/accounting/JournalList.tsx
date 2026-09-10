"use client";

import { useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { fetchJournals, type JournalListFilter } from "@/lib/api/ledger";
import type { JournalSummary, JournalSource } from "@/lib/types/api";
import { Can } from "@/components/ui/Can";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LinkifiedText } from "@/components/documents/LinkifiedText";

interface Props {
  token: string;
}

const SOURCE_LABELS: Record<JournalSource, string> = {
  manual: "Manual",
  system: "Sistem",
  reversal: "Pembalik",
  cost: "Biaya",
  sale: "Penjualan",
  tax: "Pajak",
  termin: "Termin",
  opening_balance: "Saldo Awal",
};

// Chip sumber jurnal — NETRAL seragam (label teks sudah membedakan; hindari
// 9 warna tabrakan). Reversal ditandai warning karena butuh perhatian audit.
const NEUTRAL_CHIP = "bg-border-subtle text-text-secondary";
const SOURCE_COLORS: Record<JournalSource, string> = {
  manual: NEUTRAL_CHIP,
  system: NEUTRAL_CHIP,
  reversal: "bg-warning-bg text-warning",
  cost: NEUTRAL_CHIP,
  sale: NEUTRAL_CHIP,
  tax: NEUTRAL_CHIP,
  termin: NEUTRAL_CHIP,
  opening_balance: NEUTRAL_CHIP,
};

function formatRupiah(value: string): string {
  const num = parseFloat(value);
  if (isNaN(num)) return "Rp 0";
  return "Rp " + num.toLocaleString("id-ID", { minimumFractionDigits: 0, maximumFractionDigits: 0 });
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });
}

export function JournalList({ token }: Props) {
  const router = useRouter();
  const [journals, setJournals] = useState<JournalSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<JournalListFilter>({});
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [source, setSource] = useState("");

  const load = useCallback(async (f: JournalListFilter) => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchJournals(token, f);
      setJournals(data);
    } catch {
      setError("Gagal memuat jurnal.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => { load(filter); }, [load, filter]);

  function applyFilter() {
    setFilter({ date_from: dateFrom || undefined, date_to: dateTo || undefined, source: source || undefined });
  }

  function resetFilter() {
    setDateFrom("");
    setDateTo("");
    setSource("");
    setFilter({});
  }

  return (
    <div className="space-y-4">
      {/* Filter bar */}
      <div className="bg-bg rounded-xl p-4 flex flex-wrap gap-3 items-end">
        <div>
          <label className="block text-xs text-text-secondary mb-1">Dari</label>
          <input
            type="date"
            value={dateFrom}
            onChange={e => setDateFrom(e.target.value)}
            className="border border-border rounded-lg px-3 py-1.5 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>
        <div>
          <label className="block text-xs text-text-secondary mb-1">Sampai</label>
          <input
            type="date"
            value={dateTo}
            onChange={e => setDateTo(e.target.value)}
            className="border border-border rounded-lg px-3 py-1.5 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>
        <div>
          <label className="block text-xs text-text-secondary mb-1">Sumber</label>
          <select
            value={source}
            onChange={e => setSource(e.target.value)}
            className="border border-border rounded-lg px-3 py-1.5 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
          >
            <option value="">Semua</option>
            {(Object.keys(SOURCE_LABELS) as JournalSource[]).map(s => (
              <option key={s} value={s}>{SOURCE_LABELS[s]}</option>
            ))}
          </select>
        </div>
        <button
          onClick={applyFilter}
          className="px-4 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 transition-colors"
        >
          Filter
        </button>
        <button
          onClick={resetFilter}
          className="px-4 py-1.5 text-sm text-text-secondary hover:text-accent transition-colors"
        >
          Reset
        </button>
        <div className="flex-1" />
        <Can roles={["owner", "accountant"]}>
          <button
            onClick={() => router.push("/accounting/jurnal/create")}
            className="px-4 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 transition-colors"
          >
            + Buat Jurnal
          </button>
        </Can>
      </div>

      {/* Table */}
      {loading ? (
        <div className="space-y-2">
          {[...Array(5)].map((_, i) => (
            <div key={i} className="h-12 bg-border-subtle rounded-lg animate-pulse" />
          ))}
        </div>
      ) : error ? (
        <ErrorState description={error} onRetry={() => load(filter)} />
      ) : journals.length === 0 ? (
        (filter.date_from || filter.date_to || filter.source) ? (
          <EmptyState
            title="Tidak ada jurnal pada filter ini"
            description="Tidak ada jurnal yang cocok dengan rentang tanggal atau sumber yang dipilih. Ubah atau reset filter untuk melihat seluruh jurnal."
            action={
              <button
                onClick={resetFilter}
                className="text-sm px-3 py-1.5 rounded border border-border text-text-secondary hover:text-accent hover:border-accent/40 transition-colors"
              >
                Reset Filter
              </button>
            }
          />
        ) : (
          <EmptyState
            title="Belum ada jurnal"
            description="Buku besar masih kosong. Jurnal otomatis akan muncul saat ada biaya, penjualan, atau pajak yang diposting. Anda juga bisa membuat jurnal manual atau memasukkan saldo awal."
            action={
              <Can roles={["owner", "accountant"]}>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => router.push("/accounting/jurnal/create")}
                    className="px-3 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 transition-colors"
                  >
                    + Buat Jurnal Manual
                  </button>
                  <button
                    onClick={() => router.push("/accounting/opening-balance")}
                    className="px-3 py-1.5 rounded-lg border border-border text-sm text-text-secondary hover:text-accent hover:border-accent/40 transition-colors"
                  >
                    Input Saldo Awal
                  </button>
                </div>
              </Can>
            }
          />
        )
      ) : (
        <div className="bg-surface rounded-xl border border-border overflow-x-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[720px]">
            <thead>
              <tr className="border-b border-border bg-bg text-text-secondary">
                <th className="px-4 py-3 text-left font-medium">Tanggal</th>
                <th className="px-4 py-3 text-left font-medium">Deskripsi</th>
                <th className="px-4 py-3 text-left font-medium">Referensi</th>
                {/* W-3.6: nomor bukti kas ikut di ringkasan (LEFT JOIN),
                    bukan fetch per baris. "—" = jurnal non-kas. */}
                <th className="px-4 py-3 text-left font-medium">Bukti</th>
                <th className="px-4 py-3 text-left font-medium">Sumber</th>
                <th className="px-4 py-3 text-right font-medium">Total Debit</th>
                <th className="px-4 py-3 text-right font-medium">Total Kredit</th>
                <th className="px-4 py-3 text-center font-medium">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {journals.map(j => (
                <tr
                  key={j.id}
                  onClick={() => router.push(`/accounting/jurnal/${j.id}`)}
                  className="hover:bg-bg cursor-pointer transition-colors"
                >
                  <td className="px-4 py-3 text-text-secondary whitespace-nowrap">{formatDate(j.date)}</td>
                  <td className="px-4 py-3 text-text-primary max-w-xs truncate">
                    {j.is_reversing && <span className="text-warning mr-1">↩</span>}
                    <LinkifiedText token={token} text={j.description} />
                  </td>
                  <td className="px-4 py-3 text-text-secondary">{j.reference || "—"}</td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    {j.document_number ? (
                      <span className="font-mono text-xs text-text-primary">{j.document_number}</span>
                    ) : (
                      <span className="text-text-secondary">—</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${SOURCE_COLORS[j.source] ?? "bg-border-subtle text-text-secondary"}`}>
                      {SOURCE_LABELS[j.source] ?? j.source}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-text-primary">{formatRupiah(j.total_debit)}</td>
                  <td className="px-4 py-3 text-right font-mono text-text-primary">{formatRupiah(j.total_credit)}</td>
                  <td className="px-4 py-3 text-center">
                    {j.posted_at ? (
                      <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-success-bg text-success">Diposting</span>
                    ) : (
                      <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-warning-bg text-warning">Draft</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
