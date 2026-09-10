"use client";

import { useState } from "react";
import { fetchExpenseListPrintHTML, type ExpenseListFilter } from "@/lib/api/expense";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  filter: ExpenseListFilter;
}

// ExportCostHistoryButton: buka Riwayat Biaya siap-A4 di tab baru. Sama seperti
// ExportPdfButton (laporan) — backend merender HTML, browser yang menyimpan
// sebagai PDF lewat window.print(), tidak ada library PDF di kedua sisi.
export function ExportCostHistoryButton({ token, filter }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const html = await fetchExpenseListPrintHTML(token, filter);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      toast(err instanceof Error ? err.message : "Gagal memuat riwayat biaya", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button variant="secondary" size="sm" onClick={handleClick} loading={loading}>
      &#128438; Export PDF
    </Button>
  );
}
