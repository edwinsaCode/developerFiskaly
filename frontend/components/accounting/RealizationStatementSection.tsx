"use client";

// Billing Batch 2 — seksi "Biaya Realisasi & Tagihan Lain" pada Customer
// Statement. Prinsip klien: harga rumah & biaya realisasi = DUA billing
// terpisah (outstanding/invoice/kwitansi sendiri) tetapi tampil dalam SATU
// statement. Semua angka dari GroupSummary kanonik backend.

import { useEffect, useState } from "react";
import Link from "next/link";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { fetchChargeGroupsByContract, type ChargeGroupSummary } from "@/lib/api/charge";
import { todayLocalStr } from "@/lib/date";

export function RealizationStatementSection({ token, contractId, unitId }: { token: string; contractId: number; unitId?: number }) {
  const [groups, setGroups] = useState<ChargeGroupSummary[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    fetchChargeGroupsByContract(token, contractId)
      .then((g) => { if (!cancelled) setGroups(g.filter((x) => x.status !== "cancelled")); })
      .catch(() => { /* seksi opsional — statement inti tetap tampil */ })
      .finally(() => { if (!cancelled) setLoaded(true); });
    return () => { cancelled = true; };
  }, [token, contractId]);

  if (!loaded || groups.length === 0) return null;

  return (
    <Card padding="none">
      <div className="px-5 py-4 border-b border-border flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">Biaya Realisasi &amp; Tagihan Lain</h2>
          <p className="text-xs text-text-secondary mt-0.5">
            Billing terpisah dari harga rumah — titipan (bukan pendapatan), outstanding &amp; kwitansi sendiri (KWR).
          </p>
        </div>
        {unitId && (
          <Link href={`/penjualan/${unitId}/tagihan`} className="text-xs text-accent hover:underline font-medium">
            Kelola Tagihan →
          </Link>
        )}
      </div>
      <Table>
        <TableHead>
          <TableRow>
            <Th>Grup</Th>
            <Th>Status</Th>
            <Th right>Total Tagihan</Th>
            <Th right>Total Dibayar</Th>
            <Th right>Sisa Terutang</Th>
            <Th right>Sisa Titipan</Th>
          </TableRow>
        </TableHead>
        <TableBody>
          {groups.map((g) => (
            <TableRow key={g.group_id}>
              <Td>
                <span className="font-medium text-text-primary">{g.label}</span>
                <span className="block text-[11px] text-text-tertiary">
                  {g.items.filter((i) => i.status === "open").map((i) => i.label).join(" · ")}
                </span>
              </Td>
              <Td>
                {g.status === "open"
                  ? <Badge variant="warning">Berjalan</Badge>
                  : <Badge variant="success">Selesai</Badge>}
              </Td>
              <Td right><Rupiah value={g.billed} /></Td>
              <Td right><Rupiah value={g.paid} /></Td>
              <Td right>
                <span className={g.outstanding !== "0" ? "font-semibold text-text-primary" : ""}>
                  <Rupiah value={g.outstanding} />
                </span>
              </Td>
              <Td right><Rupiah value={g.residual} /></Td>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <p className="px-5 py-2 text-[11px] text-text-tertiary border-t border-border">
        Outstanding di atas TIDAK termasuk dalam sisa tagihan harga rumah — keduanya billing terpisah.
        Dicetak per <Tanggal value={todayLocalStr()} />.
      </p>
    </Card>
  );
}
