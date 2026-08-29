"use client";

import { useState } from "react";
import { fetchInvoicePrintHTML } from "@/lib/api/billing";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  invoiceId: number;
  label?: string;
}

// PrintInvoiceButton (R2): buka versi cetak A4 invoice di tab baru.
// Pola identik PrintReceiptButton — dipakai di Statement 360 & daftar invoice.
export function PrintInvoiceButton({ token, invoiceId, label = "Cetak" }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const html = await fetchInvoicePrintHTML(token, invoiceId);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) {
        toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      }
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      toast(err instanceof Error ? err.message : "Gagal memuat invoice", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      onClick={handleClick}
      disabled={loading}
      className="text-xs text-accent hover:underline whitespace-nowrap disabled:opacity-50"
    >
      {loading ? "Memuat…" : label}
    </button>
  );
}
