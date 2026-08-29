"use client";

import { useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Rupiah } from "@/components/format/Rupiah";
import { fetchInvoicePrintHTML } from "@/lib/api/billing";
import { useToast } from "@/components/ui/Toast";
import { EmptyState } from "@/components/ui/EmptyState";
import type { InvoiceSummary, InvoiceStatus } from "@/lib/types/api";

interface Props {
  token: string;
  initialInvoices: InvoiceSummary[];
  // S9/R-9: total belum-terbayar dihitung backend — FE display saja.
  totalUnpaid: string;
}

function fmtDate(d: string) {
  return new Date(d).toLocaleDateString("id-ID", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

const statusVariant: Record<InvoiceStatus, "success" | "warning" | "danger" | "neutral"> = {
  issued:    "warning",
  paid:      "success",
  overdue:   "danger",
  cancelled: "neutral",
};

const statusLabel: Record<InvoiceStatus, string> = {
  issued:    "Diterbitkan",
  paid:      "Lunas",
  overdue:   "Jatuh Tempo",
  cancelled: "Batal",
};

const typeLabel: Record<string, string> = {
  DP:        "Uang Muka",
  TERMIN:    "Termin",
  PELUNASAN: "Pelunasan",
  KEKURANGAN: "Kekurangan",
};

export function AllInvoiceList({ token, initialInvoices, totalUnpaid }: Props) {
  const { toast } = useToast();
  const [printing, setPrinting] = useState<number | null>(null);
  const [selected, setSelected] = useState<InvoiceSummary | null>(null);
  // R6/U2 — filter & cari (client-side; data sudah dimuat server).
  const [q, setQ] = useState("");
  const [statusFilter, setStatusFilter] = useState<InvoiceStatus | "all">("all");

  const counts = initialInvoices.reduce(
    (acc, inv) => { acc[inv.status] = (acc[inv.status] ?? 0) + 1; return acc; },
    {} as Record<string, number>,
  );
  const needle = q.trim().toLowerCase();
  const filtered = initialInvoices.filter((inv) => {
    if (statusFilter !== "all" && inv.status !== statusFilter) return false;
    if (!needle) return true;
    return (
      inv.invoice_number.toLowerCase().includes(needle) ||
      inv.buyer_name.toLowerCase().includes(needle) ||
      inv.unit_code.toLowerCase().includes(needle)
    );
  });
  // S9/R-9: total dari backend (decimal) — FE tidak menjumlah ulang.
  const hasUnpaid = totalUnpaid !== "0" && totalUnpaid !== "";

  async function handlePrint(inv: InvoiceSummary) {
    setPrinting(inv.id);
    try {
      const html = await fetchInvoicePrintHTML(token, inv.id);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) {
        toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      }
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Gagal membuka invoice";
      toast(msg, "error");
    } finally {
      setPrinting(null);
    }
  }

  if (initialInvoices.length === 0) {
    return (
      <EmptyState
        icon={
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
            strokeLinecap="round" strokeLinejoin="round">
            <path d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
        }
        title="Belum ada invoice diterbitkan"
        description="Invoice tidak dibuat langsung di sini. Setiap invoice lahir dari jadwal pembayaran (DP, termin, atau pelunasan) pada sebuah kontrak penjualan unit. Buka unit yang sudah terjual lalu terbitkan invoice dari termin-nya."
        action={
          <a
            href="/penjualan"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
          >
            Ke Unit & Penjualan →
          </a>
        }
      />
    );
  }

  return (
    <div className="space-y-4">
      {/* R6/U2 — ringkasan + filter + cari */}
      <div className="flex flex-wrap items-center gap-2">
        {([
          ["all", `Semua (${initialInvoices.length})`],
          ["issued", `Diterbitkan (${counts.issued ?? 0})`],
          ["overdue", `Jatuh Tempo (${counts.overdue ?? 0})`],
          ["paid", `Lunas (${counts.paid ?? 0})`],
          ["cancelled", `Batal (${counts.cancelled ?? 0})`],
        ] as [InvoiceStatus | "all", string][]).map(([v, label]) => (
          <button
            key={v}
            onClick={() => setStatusFilter(v)}
            className={`rounded-full border px-3 py-1 text-xs transition-colors ${
              statusFilter === v
                ? "border-accent bg-accent text-white"
                : "border-border text-text-secondary hover:border-accent hover:text-accent"
            }`}
          >
            {label}
          </button>
        ))}
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Cari nomor / customer / unit…"
          className="ml-auto w-64 rounded-md border border-border bg-surface px-3 py-1.5 text-sm
            focus:border-accent focus:outline-none"
        />
      </div>
      {hasUnpaid && (
        <p className="text-xs text-text-secondary">
          Belum terbayar (diterbitkan + jatuh tempo):{" "}
          <strong className="tabular-nums">
            Rp {Number(totalUnpaid).toLocaleString("id-ID")}
          </strong>
        </p>
      )}
      {filtered.length === 0 && (
        <p className="rounded-md border border-dashed border-border p-4 text-sm text-text-secondary">
          Tidak ada invoice yang cocok dengan filter/pencarian.
        </p>
      )}
      <div className="overflow-x-auto rounded border border-border">
        <table className="min-w-full text-sm">
          <thead className="bg-bg">
            <tr>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">No. Invoice</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Customer</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Unit</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Tipe</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Tanggal</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Jatuh Tempo</th>
              <th className="px-4 py-2.5 text-right font-medium text-text-secondary">Nominal</th>
              <th className="px-4 py-2.5 text-left font-medium text-text-secondary">Status</th>
              <th className="px-4 py-2.5 text-center font-medium text-text-secondary">Aksi</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {filtered.map((inv) => (
              <tr key={inv.id} className="hover:bg-bg/50">
                <td className="px-4 py-2.5 font-mono text-xs">{inv.invoice_number}</td>
                <td className="px-4 py-2.5">{inv.buyer_name}</td>
                <td className="px-4 py-2.5 font-mono text-xs">{inv.unit_code}</td>
                <td className="px-4 py-2.5">{typeLabel[inv.invoice_type] ?? inv.invoice_type}</td>
                <td className="px-4 py-2.5">{fmtDate(inv.issue_date)}</td>
                <td className="px-4 py-2.5">{fmtDate(inv.due_date)}</td>
                <td className="px-4 py-2.5 text-right font-mono">
                  <Rupiah value={inv.amount} />
                </td>
                <td className="px-4 py-2.5">
                  <Badge variant={statusVariant[inv.status]}>
                    {statusLabel[inv.status]}
                  </Badge>
                </td>
                <td className="px-4 py-2.5 text-center">
                  <div className="flex gap-2 justify-center items-center">
                    <button
                      onClick={() => setSelected(inv)}
                      className="text-xs text-accent hover:underline"
                    >
                      Detail
                    </button>
                    <button
                      onClick={() => handlePrint(inv)}
                      disabled={printing === inv.id}
                      className="text-xs text-text-secondary hover:underline disabled:opacity-50"
                    >
                      {printing === inv.id ? "Memuat..." : "Print"}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {selected && (
        <div
          className="fixed inset-0 z-overlay flex items-center justify-center bg-text-primary/30 backdrop-blur-sm"
          onClick={() => setSelected(null)}
        >
          <div
            className="bg-surface rounded-lg shadow-xl w-full max-w-lg p-6 space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex justify-between items-start">
              <div>
                <p className="text-xs text-text-secondary mb-0.5">No. Invoice</p>
                <p className="font-mono text-sm font-semibold">{selected.invoice_number}</p>
              </div>
              <Badge variant={statusVariant[selected.status]}>
                {statusLabel[selected.status]}
              </Badge>
            </div>

            <div className="grid grid-cols-2 gap-3 text-sm">
              <div>
                <p className="text-xs text-text-secondary">Tipe</p>
                <p>{typeLabel[selected.invoice_type] ?? selected.invoice_type}</p>
              </div>
              <div>
                <p className="text-xs text-text-secondary">Customer</p>
                <p>{selected.buyer_name}</p>
              </div>
              <div>
                <p className="text-xs text-text-secondary">Unit</p>
                <p className="font-mono text-xs">{selected.unit_code}</p>
              </div>
              <div>
                <p className="text-xs text-text-secondary">Jatuh Tempo</p>
                <p>{fmtDate(selected.due_date)}</p>
              </div>
              <div className="col-span-2">
                <p className="text-xs text-text-secondary">Jumlah</p>
                <p className="text-lg font-semibold font-mono">
                  <Rupiah value={selected.amount} />
                </p>
              </div>
              {selected.notes && (
                <div className="col-span-2">
                  <p className="text-xs text-text-secondary">Catatan</p>
                  <p className="text-sm">{selected.notes}</p>
                </div>
              )}
            </div>

            <div className="flex gap-2 pt-2">
              <Button
                onClick={() => handlePrint(selected)}
                variant="secondary"
                size="sm"
                loading={printing === selected.id}
                disabled={printing === selected.id}
              >
                {printing === selected.id ? "Memuat PDF..." : "Download / Print PDF"}
              </Button>
              <Button onClick={() => setSelected(null)} variant="ghost" size="sm">
                Tutup
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
