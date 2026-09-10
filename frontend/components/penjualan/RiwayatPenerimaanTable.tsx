"use client";

// W-13: satu tabel "Riwayat Penerimaan" — menggantikan tampilan jadwal cicilan
// sebagai sumber utama riwayat uang, karena jadwal kini opsional (murni
// perencanaan) sedangkan tabel ini membaca TRANSAKSI AKTUAL (termin_payments).
//
// D-1 (requirement D): dokumen akuntansi ≠ kwitansi customer. Baris
// bank_disbursement TIDAK PERNAH menawarkan "Cetak Kwitansi Customer" — hanya
// "Cetak KWD" (dokumen internal, mekanisme sama, label berbeda), konsisten
// dengan FinancingMilestones.

import { useCallback, useEffect, useState } from "react";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Badge } from "@/components/ui/Badge";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { LinkifiedText } from "@/components/documents/LinkifiedText";
import { listTermins } from "@/lib/api/sale";
import { fetchReceiptByTermin } from "@/lib/api/billing";
import type { TerminPayment } from "@/lib/types/api";

const KIND_LABEL: Record<string, string> = {
  dp: "DP",
  installment: "Cicilan",
  final_payment: "Pelunasan",
  other: "Lainnya",
  bank_disbursement: "Pencairan Bank",
  land_excess: "Kelebihan Tanah",
};

interface Row extends TerminPayment {
  receiptNumber?: string;
}

export function RiwayatPenerimaanTable({ token, unitId, refreshKey }: {
  token: string;
  unitId: number;
  /** Naikkan nilai ini dari parent setelah aksi baru agar tabel ikut segar. */
  refreshKey?: number;
}) {
  const [rows, setRows] = useState<Row[] | null>(null);

  const load = useCallback(async () => {
    try {
      const { data } = await listTermins(token, unitId);
      const termins = (data ?? []).slice().sort((a, b) => b.date.localeCompare(a.date));
      const withReceipts = await Promise.all(
        termins.map(async (t): Promise<Row> => {
          const rec = await fetchReceiptByTermin(token, t.id).catch(() => null);
          return { ...t, receiptNumber: rec?.receipt_number };
        }),
      );
      setRows(withReceipts);
    } catch {
      setRows([]);
    }
  }, [token, unitId]);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  if (rows === null) {
    return <p className="text-sm text-text-tertiary">Memuat riwayat penerimaan…</p>;
  }
  if (rows.length === 0) {
    return (
      <p className="text-sm text-text-secondary">
        Belum ada penerimaan tercatat. Gunakan tombol <strong>+ Catat Penerimaan</strong> di atas.
      </p>
    );
  }

  return (
    <div className="overflow-x-auto -mx-1">
      <table className="w-full text-sm min-w-[640px]">
        <thead>
          <tr className="text-left text-xs text-text-secondary uppercase tracking-wide border-b border-border">
            <th className="px-1 py-2 font-medium">Tanggal</th>
            <th className="px-1 py-2 font-medium">Jenis</th>
            <th className="px-1 py-2 font-medium">Keterangan</th>
            <th className="px-1 py-2 font-medium text-right">Nominal</th>
            <th className="px-1 py-2 font-medium">Rekening</th>
            <th className="px-1 py-2 font-medium">Dokumen</th>
            <th className="px-1 py-2 font-medium text-right">Kwitansi</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-subtle">
          {rows.map((r) => {
            const isDisbursement = r.kind === "bank_disbursement" || r.payment_source === "kpr_disbursement";
            const kindLabel = KIND_LABEL[r.kind ?? "other"] ?? "Lainnya";
            return (
              <tr key={r.id}>
                <td className="px-1 py-2 whitespace-nowrap"><Tanggal value={r.date} /></td>
                <td className="px-1 py-2">
                  <Badge variant={isDisbursement ? "warning" : "success"}>
                    {kindLabel}{r.installment_no ? ` #${r.installment_no}` : ""}
                  </Badge>
                </td>
                <td className="px-1 py-2 text-text-secondary max-w-[220px] truncate" title={r.description}>
                  {r.description ? <LinkifiedText token={token} text={r.description} /> : "—"}
                </td>
                <td className="px-1 py-2 text-right tabular-nums font-medium">
                  <Rupiah value={r.amount} colorSign={false} />
                </td>
                <td className="px-1 py-2 text-text-tertiary font-mono text-xs">{r.bank_account_code}</td>
                <td className="px-1 py-2 font-mono text-xs text-text-secondary">{r.receiptNumber ?? "—"}</td>
                <td className="px-1 py-2 text-right">
                  <PrintReceiptButton
                    token={token}
                    terminId={r.id}
                    label={isDisbursement ? "Cetak KWD" : "Cetak Kwitansi"}
                  />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
