"use client";

import { useState } from "react";
import { fetchExpensePrintHTML } from "@/lib/api/expense";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  expenseId: number;
  documentNumber: string;
}

// PrintExpenseReceiptLink: nomor dokumen (mis. BKK/2026/000001) yang bisa
// diklik langsung ke Bukti Kas Keluar siap-A4 di tab baru. Sama seperti
// PrintPaymentReceiptButton (AP) — dokumennya sudah ada, tombol ini murni
// membaca, tidak pernah membuat apa pun.
export function PrintExpenseReceiptLink({ token, expenseId, documentNumber }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const html = await fetchExpensePrintHTML(token, expenseId);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      toast(err instanceof Error ? err.message : "Gagal membuka bukti kas keluar", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={loading}
      title="Cetak / simpan PDF bukti kas keluar"
      className="block font-mono text-[10px] text-accent hover:underline mt-0.5 disabled:opacity-50 disabled:no-underline"
    >
      {loading ? "Memuat…" : documentNumber}
    </button>
  );
}
