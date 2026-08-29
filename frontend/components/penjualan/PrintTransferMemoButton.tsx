"use client";

// PrintTransferMemoButton — MEMO TRANSFER INTERNAL (T-1, keputusan klien 2026-08-05).
//
// Perpindahan sisa titipan (ke harga rumah / ke grup lain) BUKAN kas masuk baru:
// uangnya sudah diterima dan sudah ber-kwitansi KWR. Karena itu transfer tidak
// pernah menerbitkan kwitansi — dokumen audit trail-nya adalah memo ini
// (seri MTI/{tahun}/{6 digit}), yang menjelaskan dana pindah dari mana ke mana.

import { useState } from "react";
import { fetchTransferMemo, type TransferMemo } from "@/lib/api/charge";
import { useToast } from "@/components/ui/Toast";

interface Props {
  token: string;
  settlementId: number;
  label?: string;
}

function rupiah(value: string): string {
  const n = parseFloat(String(value).replace(",", "."));
  const safe = isNaN(n) ? 0 : n;
  return `Rp ${new Intl.NumberFormat("id-ID", { maximumFractionDigits: 0 }).format(safe)}`;
}

function tanggal(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleDateString("id-ID", { day: "numeric", month: "long", year: "numeric" });
}

function esc(s: string | undefined | null): string {
  return String(s ?? "").replace(/[&<>"]/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] as string,
  );
}

function memoHTML(m: TransferMemo): string {
  const rows: [string, string][] = [
    ["Nomor Memo", m.memo_number],
    ["Tanggal", tanggal(m.date)],
    ["Unit", m.source_unit_code || "—"],
    ["Pembeli", m.buyer_name || "—"],
    ["Dana Berasal Dari", `${m.source_group_label} (grup #${m.source_group_id})`],
    ["Dipindahkan Ke", m.target_label],
    ["Jumlah", rupiah(m.amount)],
    ["Referensi Jurnal", m.journal_entry_id ? `JE #${m.journal_entry_id}` : "—"],
  ];
  if (m.notes) rows.push(["Keterangan", m.notes]);

  return `<!DOCTYPE html>
<html lang="id"><head><meta charset="utf-8" />
<title>Memo Transfer Internal ${esc(m.memo_number)}</title>
<style>
  *,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
  body{font-family:'Segoe UI',Arial,sans-serif;background:#f0f4f8;color:#1a2535;line-height:1.55;font-size:14px}
  .bar{position:fixed;top:0;left:0;right:0;z-index:10;background:#1e3a5f;color:#fff;display:flex;
       align-items:center;justify-content:space-between;padding:10px 24px}
  .bar h1{font-size:1rem;font-weight:600}
  .btn{background:#f59e0b;color:#1a2535;border:0;border-radius:6px;padding:8px 22px;font-weight:700;cursor:pointer}
  .wrap{padding:80px 32px 40px;display:flex;justify-content:center}
  .sheet{width:210mm;min-height:180mm;background:#fff;border-radius:10px;
         box-shadow:0 4px 32px rgba(0,0,0,.12);padding:16mm}
  .company{font-size:1.15rem;font-weight:700;letter-spacing:.02em}
  .doc{margin-top:10mm;text-align:center}
  .doc h2{font-size:1.3rem;letter-spacing:.08em;text-transform:uppercase}
  .doc p{font-size:.85rem;color:#5b6b80;margin-top:2mm}
  table{width:100%;border-collapse:collapse;margin-top:10mm}
  td{padding:7px 0;vertical-align:top;border-bottom:1px solid #e7edf3}
  td.k{width:45mm;color:#5b6b80}
  td.v{font-weight:600}
  .amount{font-size:1.15rem}
  .note{margin-top:10mm;padding:5mm;background:#f7fafc;border-left:3px solid #1e3a5f;font-size:.85rem;color:#42536a}
  .sign{margin-top:18mm;display:flex;justify-content:flex-end;text-align:center}
  .sign div{width:60mm}
  .line{margin-top:20mm;border-top:1px solid #1a2535;padding-top:2mm;font-size:.85rem}
  @media print{.bar{display:none!important}.wrap{padding:0}.sheet{box-shadow:none;border-radius:0;width:auto}
    body{background:#fff}@page{size:A4;margin:12mm}}
</style></head>
<body>
<div class="bar"><h1>Memo Transfer Internal — ${esc(m.memo_number)}</h1>
  <button class="btn" onclick="window.print()">&#128438; Cetak / Simpan PDF</button></div>
<div class="wrap"><div class="sheet">
  <div class="company">${esc(m.company_name || "—")}</div>
  <div class="doc">
    <h2>Memo Transfer Internal</h2>
    <p>Dokumen ini BUKAN bukti terima uang. Dana telah diterima sebelumnya dan
       sudah berkwitansi; memo ini mencatat perpindahan dana antar pos.</p>
  </div>
  <table>
    ${rows
      .map(
        ([k, v]) =>
          `<tr><td class="k">${esc(k)}</td><td class="v${k === "Jumlah" ? " amount" : ""}">${esc(v)}</td></tr>`,
      )
      .join("")}
  </table>
  <div class="note">
    Perpindahan dana ini tidak menambah kas dan tidak menimbulkan pendapatan baru.
    Pengaruhnya hanya memindahkan saldo antar akun kewajiban/piutang sesuai jurnal di atas.
  </div>
  <div class="sign"><div><div class="line">Dibuat &amp; disetujui</div></div></div>
</div></div>
</body></html>`;
}

export function PrintTransferMemoButton({ token, settlementId, label = "Cetak Memo" }: Props) {
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      const memo = await fetchTransferMemo(token, settlementId);
      const blob = new Blob([memoHTML(memo)], { type: "text/html;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, "_blank");
      if (!win) toast("Popup diblokir browser. Izinkan popup untuk halaman ini.", "error");
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err) {
      toast(err instanceof Error ? err.message : "Gagal membuka memo transfer", "error");
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
