"use client";

import { useState } from "react";
import { generateReceipt, fetchReceiptPrintHTML } from "@/lib/api/billing";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  terminId: number;
  /** Catatan opsional yang tercetak di kwitansi. */
  notes?: string;
  /** Gaya tampilan: "link" (teks kecil) atau "button". */
  variant?: "link" | "button";
  label?: string;
}

// PrintReceiptButton: satu klik untuk memastikan kwitansi ada (idempoten) lalu
// membuka versi cetak A4 di tab baru. Dipakai di statement & panel unit.
export function PrintReceiptButton({ token, terminId, notes, variant = "link", label = "Cetak Kwitansi" }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const receipt = await generateReceipt(token, terminId, notes);
      const html = await fetchReceiptPrintHTML(token, receipt.id);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) {
        toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      }
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Gagal membuat kwitansi";
      toast(msg, "error");
    } finally {
      setLoading(false);
    }
  }

  if (variant === "button") {
    return (
      <button
        onClick={handleClick}
        disabled={loading}
        className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border border-border text-sm
          text-text-secondary hover:text-accent hover:border-accent/40 transition-colors disabled:opacity-50"
      >
        {loading ? "Memuat…" : `🧾 ${label}`}
      </button>
    );
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
