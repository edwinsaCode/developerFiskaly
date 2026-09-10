"use client";

import { useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Rupiah } from "@/components/format/Rupiah";
import { generateInvoice, fetchInvoicePrintHTML } from "@/lib/api/billing";
import { useToast } from "@/components/ui/Toast";
import type { Invoice, PaymentSchedule, InvoiceStatus } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
  contractId: number;
  unitId: number;
  buyerName: string;
  schedules: PaymentSchedule[];
  initialInvoices: Invoice[];
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
  REALISASI: "Biaya Realisasi",
  KELEBIHAN_TANAH: "Kelebihan Tanah",
};

export function InvoiceList({
  token,
  contractId,
  unitId,
  buyerName,
  schedules,
  initialInvoices,
}: Props) {
  const { toast } = useToast();
  const [invoices, setInvoices] = useState<Invoice[]>(initialInvoices);
  const [generating, setGenerating] = useState<number | null>(null);
  const [printing, setPrinting] = useState<number | null>(null);
  const [printError, setPrintError] = useState<string | null>(null);
  const [selected, setSelected] = useState<Invoice | null>(null);

  const invoicedScheduleIds = new Set(
    invoices.filter((inv) => inv.schedule_id != null).map((inv) => inv.schedule_id!)
  );

  async function handleGenerate(schedule: PaymentSchedule) {
    setGenerating(schedule.id);
    try {
      const inv = await generateInvoice(token, contractId, {
        schedule_id: schedule.id,
        issue_date: todayLocalStr(),
      });
      setInvoices((prev) => [...prev, inv]);
      toast("Invoice berhasil dibuat: " + inv.invoice_number, "success");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Gagal membuat invoice";
      toast(msg, "error");
    } finally {
      setGenerating(null);
    }
  }

  async function handlePrint(inv: Invoice) {
    setPrinting(inv.id);
    setPrintError(null);
    try {
      const html = await fetchInvoicePrintHTML(token, inv.id);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) {
        toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      }
      // Revoke after 60s to avoid memory leak.
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Gagal membuka invoice";
      setPrintError(msg);
      toast(msg, "error");
    } finally {
      setPrinting(null);
    }
  }

  return (
    <div className="space-y-6">
      {/* ── Schedule table with generate buttons ── */}
      <section>
        <h2 className="text-base font-semibold mb-3">Jadwal Pembayaran</h2>
        {schedules.length === 0 ? (
          <div className="rounded border border-dashed border-border px-4 py-6 text-center">
            <p className="text-sm text-text-secondary">
              Kontrak ini belum memiliki jadwal pembayaran.
            </p>
            <p className="text-xs text-text-tertiary mt-1">
              Jadwal (DP, termin, pelunasan) dibuat di panel penjualan unit. Invoice baru bisa
              diterbitkan setelah jadwal tersedia.
            </p>
            <a
              href={`/penjualan/${unitId}`}
              className="inline-flex items-center gap-2 mt-3 px-3 py-1.5 rounded-md border border-border text-sm text-text-secondary hover:border-accent/40 hover:text-accent transition-colors"
            >
              Atur Jadwal Pembayaran →
            </a>
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-border">
            <table className="min-w-full text-sm">
              <thead className="bg-bg">
                <tr>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">#</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Tipe</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Jatuh Tempo</th>
                  <th className="px-4 py-2 text-right font-medium text-text-secondary">Jumlah</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Status</th>
                  <th className="px-4 py-2 text-center font-medium text-text-secondary">Invoice</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {schedules.map((s) => {
                  const hasInvoice = invoicedScheduleIds.has(s.id);
                  const inv = invoices.find((i) => i.schedule_id === s.id);
                  return (
                    <tr key={s.id} className="hover:bg-bg/50">
                      <td className="px-4 py-2">{s.installment_number}</td>
                      <td className="px-4 py-2 capitalize">
                        {s.type === "dp" ? "Uang Muka" : s.type === "final" ? "Pelunasan" : "Cicilan"}
                      </td>
                      <td className="px-4 py-2">{fmtDate(s.due_date)}</td>
                      <td className="px-4 py-2 text-right font-mono">
                        <Rupiah value={s.amount} />
                      </td>
                      <td className="px-4 py-2">
                        <Badge variant={
                          s.status === "received" ? "success" :
                          s.status === "overdue"  ? "danger"  : "warning"
                        }>
                          {s.status === "received" ? "Diterima" :
                           s.status === "overdue"  ? "Terlambat" : "Terjadwal"}
                        </Badge>
                      </td>
                      <td className="px-4 py-2 text-center">
                        {hasInvoice && inv ? (
                          <button
                            onClick={() => setSelected(inv)}
                            className="text-xs text-accent hover:underline font-medium"
                          >
                            {inv.invoice_number}
                          </button>
                        ) : (
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => handleGenerate(s)}
                            disabled={generating === s.id}
                          >
                            {generating === s.id ? "..." : "Generate"}
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* ── Invoice list ── */}
      <section>
        <h2 className="text-base font-semibold mb-3">Daftar Invoice</h2>
        {invoices.length === 0 ? (
          <div className="rounded border border-dashed border-border px-4 py-6 text-center">
            <p className="text-sm text-text-secondary">Belum ada invoice yang diterbitkan.</p>
            <p className="text-xs text-text-tertiary mt-1">
              {schedules.length > 0
                ? "Gunakan tombol Generate pada tabel Jadwal Pembayaran di atas untuk menerbitkan invoice dari termin yang dipilih."
                : "Buat jadwal pembayaran terlebih dahulu, lalu terbitkan invoice dari termin-nya."}
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto rounded border border-border">
            <table className="min-w-full text-sm">
              <thead className="bg-bg">
                <tr>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">No. Invoice</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Tipe</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Tanggal</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Jatuh Tempo</th>
                  <th className="px-4 py-2 text-right font-medium text-text-secondary">Jumlah</th>
                  <th className="px-4 py-2 text-left font-medium text-text-secondary">Status</th>
                  <th className="px-4 py-2 text-center font-medium text-text-secondary">Aksi</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {invoices.map((inv) => (
                  <tr key={inv.id} className="hover:bg-bg/50">
                    <td className="px-4 py-2 font-mono text-xs">{inv.invoice_number}</td>
                    <td className="px-4 py-2">{typeLabel[inv.invoice_type] ?? inv.invoice_type}</td>
                    <td className="px-4 py-2">{fmtDate(inv.issue_date)}</td>
                    <td className="px-4 py-2">{fmtDate(inv.due_date)}</td>
                    <td className="px-4 py-2 text-right font-mono">
                      <Rupiah value={inv.amount} />
                    </td>
                    <td className="px-4 py-2">
                      <Badge variant={statusVariant[inv.status]}>
                        {statusLabel[inv.status]}
                      </Badge>
                    </td>
                    <td className="px-4 py-2 text-center">
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
                          title="Buka halaman cetak / download PDF"
                        >
                          {printing === inv.id ? "Memuat..." : "Print / PDF"}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* ── Invoice detail modal ── */}
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
                <p className="text-xs text-text-secondary">Pembeli</p>
                <p>{buyerName}</p>
              </div>
              <div>
                <p className="text-xs text-text-secondary">Tanggal Invoice</p>
                <p>{fmtDate(selected.issue_date)}</p>
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

            {printError && (
              <p className="text-xs text-danger bg-danger-bg rounded px-3 py-2">{printError}</p>
            )}
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
