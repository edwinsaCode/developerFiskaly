"use client";

import { useState } from "react";
import { fetchAPPaymentPrintHTML } from "@/lib/api/ap";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  paymentId: number;
}

// PrintPaymentReceiptButton: buka Bukti Kas Keluar (BKK) siap-A4 di tab baru.
// Dokumennya sudah ada (Payment.DocumentNumber, terbit saat pembayaran
// diposting) — tombol ini murni membaca, tidak pernah membuat apa pun.
export function PrintPaymentReceiptButton({ token, paymentId }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const html = await fetchAPPaymentPrintHTML(token, paymentId);
      const blob = new Blob([html], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) {
        toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      }
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Gagal membuka bukti kas keluar";
      toast(msg, "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button variant="secondary" onClick={handleClick} disabled={loading}>
      {loading ? "Memuat…" : "🖶 Cetak BKK"}
    </Button>
  );
}
