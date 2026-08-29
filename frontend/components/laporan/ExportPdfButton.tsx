"use client";

import { useState } from "react";
import { fetchReportPrintHTML } from "@/lib/api/reports";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  report: string;
  params?: Record<string, string>;
  label?: string;
}

// Sama seperti PrintInvoiceButton (components/billing/): backend merender
// HTML A4 siap-cetak, browser yang menyimpan sebagai PDF lewat window.print()
// di tab baru — tidak ada library PDF di frontend maupun backend.
export function ExportPdfButton({ token, report, params = {}, label = "Export PDF" }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const html = await fetchReportPrintHTML(token, report, params);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      toast(err instanceof Error ? err.message : "Gagal memuat laporan", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button variant="secondary" size="sm" onClick={handleClick} loading={loading}>
      &#128438; {label}
    </Button>
  );
}
