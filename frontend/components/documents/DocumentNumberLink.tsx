"use client";

import { useState, type MouseEvent } from "react";
import { resolvePrintTarget } from "@/lib/api/document";
import { fetchReceiptPrintHTML, fetchInvoicePrintHTML } from "@/lib/api/billing";
import { fetchAPPaymentPrintHTML } from "@/lib/api/ap";
import { fetchExpensePrintHTML } from "@/lib/api/expense";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  number: string;
  className?: string;
}

function openPrintHTML(html: string, toast: (msg: string, kind: "error") => void) {
  const blob = new Blob([html], { type: "text/html;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const win = window.open(url, "_blank");
  if (!win) toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

// DocumentNumberLink: bungkus SATU nomor dokumen (mis. BKK/2026/000002) yang
// tampil sebagai teks di mana pun, jadi bisa diklik untuk LANGSUNG mencetak
// atau mengunduh dokumen aslinya — bukan mengarah ke layar lain. Keputusan
// owner: klik = cetak, bukan filter-ke-database-lalu-navigasi.
//
// Kwitansi/invoice punya cetakan langsung. Dokumen kas (BKK/BKM/BTP/RFC/KWD/JR)
// diterbitkan lewat jurnal bersama — resolvePrintTarget menembus ke pemilik
// jurnal itu (pembayaran vendor atau biaya tunai) di backend. "unknown" berarti
// belum ada cetakan untuk sumber ini di aplikasi mana pun — bukan alasan untuk
// menebak atau bernavigasi ke layar lain.
export function DocumentNumberLink({ token, number, className }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick(e: MouseEvent) {
    e.stopPropagation();
    setLoading(true);
    try {
      const target = await resolvePrintTarget(token, number);
      switch (target.kind) {
        case "receipt": {
          const html = await fetchReceiptPrintHTML(token, target.id!);
          openPrintHTML(html, toast);
          break;
        }
        case "invoice": {
          const html = await fetchInvoicePrintHTML(token, target.id!);
          openPrintHTML(html, toast);
          break;
        }
        case "ap_payment": {
          const html = await fetchAPPaymentPrintHTML(token, target.id!);
          openPrintHTML(html, toast);
          break;
        }
        case "expense": {
          const html = await fetchExpensePrintHTML(token, target.id!);
          openPrintHTML(html, toast);
          break;
        }
        default:
          toast(`Dokumen ${number} belum punya cetakan di layar mana pun`, "error");
      }
    } catch (err: unknown) {
      toast(err instanceof Error ? err.message : `Gagal membuka ${number}`, "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={loading}
      title={`Buka dokumen ${number}`}
      className={
        className ??
        "font-mono text-accent hover:underline disabled:opacity-50 disabled:no-underline"
      }
    >
      {loading ? "Memuat…" : number}
    </button>
  );
}
