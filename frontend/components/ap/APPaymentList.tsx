"use client";

// Daftar pembayaran hutang vendor (W-11 Tahap 5).
//
// Pembayaran yang sudah dibalik TETAP tampil, dengan penanda. Menyembunyikannya
// akan membuat daftar ini menjawab "tidak pernah ada" untuk uang yang pernah
// keluar lalu dikoreksi — padahal BKK-nya sudah tercetak dan ada di map fisik.
// Yang dicari orang saat memeriksa map itu justru baris yang dibalik.

import Link from "next/link";
import { useMemo, useState } from "react";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { Input, Select } from "@/components/ui/Input";
import { Table, TableBody, TableHead, TableRow, Td, Th } from "@/components/ui/Table";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import type { APPaymentView, Vendor } from "@/lib/types/api";
import { filterPayments, type PaymentListFilter } from "@/components/ap/paymentRules";

interface Props {
  payments: APPaymentView[];
  vendors: Vendor[];
  canWrite: boolean;
  loadError?: string;
}

export function APPaymentList({ payments, vendors, canWrite, loadError }: Props) {
  const [filter, setFilter] = useState<PaymentListFilter>({});

  const rows = useMemo(() => filterPayments(payments, filter), [payments, filter]);

  function set<K extends keyof PaymentListFilter>(k: K, v: PaymentListFilter[K]) {
    setFilter((f) => ({ ...f, [k]: v }));
  }

  const anyFilter =
    !!filter.q || !!filter.vendorId || !!filter.state || !!filter.from || !!filter.to;

  return (
    <Card padding="none">
      <div className="flex flex-wrap items-end gap-3 border-b border-border p-4">
        <Input
          label="Cari"
          wrapperClassName="min-w-[200px] flex-1"
          placeholder="Nomor BKK, vendor, atau uraian"
          value={filter.q ?? ""}
          onChange={(e) => set("q", e.target.value)}
        />
        <Select
          label="Vendor"
          wrapperClassName="w-52"
          value={filter.vendorId ?? ""}
          onChange={(e) => set("vendorId", e.target.value)}
        >
          <option value="">Semua vendor</option>
          {vendors.map((v) => (
            <option key={v.id} value={String(v.id)}>
              {v.name}
            </option>
          ))}
        </Select>
        <Select
          label="Keadaan"
          wrapperClassName="w-44"
          value={filter.state ?? ""}
          onChange={(e) => set("state", e.target.value as PaymentListFilter["state"])}
        >
          <option value="">Semua</option>
          <option value="posted">Berlaku</option>
          <option value="reversed">Dibalik</option>
        </Select>
        <Input
          label="Tanggal bayar dari"
          type="date"
          wrapperClassName="w-44"
          value={filter.from ?? ""}
          onChange={(e) => set("from", e.target.value)}
        />
        <Input
          label="sampai"
          type="date"
          wrapperClassName="w-44"
          value={filter.to ?? ""}
          onChange={(e) => set("to", e.target.value)}
        />
        {anyFilter && (
          <div className="pb-1">
            <Button size="sm" variant="ghost" onClick={() => setFilter({})}>
              Reset
            </Button>
          </div>
        )}
      </div>

      {loadError ? (
        <div className="p-4">
          <p className="text-sm text-danger">Daftar pembayaran gagal dimuat: {loadError}</p>
        </div>
      ) : rows.length === 0 ? (
        <EmptyState
          title={
            payments.length === 0
              ? "Belum ada pembayaran vendor"
              : "Tidak ada pembayaran yang cocok"
          }
          description={
            payments.length === 0
              ? "Pembayaran mengurangi hutang usaha dan mengeluarkan uang dari kas/bank. Setiap pembayaran menerbitkan satu bukti kas keluar (BKK) bernomor, dan sisa tagihan berkurang karena alokasinya — bukan karena ada kolom yang ditulis ulang."
              : "Ubah saringan atau kosongkan pencarian untuk melihat seluruh pembayaran."
          }
          action={
            payments.length === 0 && canWrite ? (
              <Link href="/accounting/pembayaran-vendor/baru">
                <Button>Bayar Tagihan Vendor</Button>
              </Link>
            ) : anyFilter ? (
              <Button variant="secondary" onClick={() => setFilter({})}>
                Reset saringan
              </Button>
            ) : undefined
          }
        />
      ) : (
        <>
          <Table>
            <TableHead>
              <tr>
                <Th>Nomor BKK</Th>
                <Th>Tanggal</Th>
                <Th>Vendor</Th>
                <Th>Sumber Dana</Th>
                <Th right>Jumlah</Th>
                <Th>Keadaan</Th>
              </tr>
            </TableHead>
            <TableBody>
              {rows.map((p) => (
                <TableRow key={p.id} subtle={p.is_reversed}>
                  <Td>
                    <Link
                      href={`/accounting/pembayaran-vendor/${p.id}`}
                      className="font-mono font-medium text-accent hover:underline"
                    >
                      {p.document_number}
                    </Link>
                    {p.description && (
                      <p className="mt-0.5 font-sans text-[11px] text-text-tertiary">
                        {p.description}
                      </p>
                    )}
                  </Td>
                  <Td>
                    <Tanggal value={p.payment_date} />
                  </Td>
                  <Td>{p.vendor_name || `#${p.vendor_id}`}</Td>
                  <Td mono>{p.cash_account_code}</Td>
                  <Td right mono>
                    <Rupiah value={p.amount} colorSign={false} />
                  </Td>
                  <Td>
                    {p.is_reversed ? (
                      <Badge variant="danger">Dibalik</Badge>
                    ) : (
                      <Badge variant="success">Berlaku</Badge>
                    )}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <p className="border-t border-border px-4 py-2.5 text-[11px] text-text-tertiary">
            {rows.length} dari {payments.length} pembayaran. Baris yang dibalik tetap ditampilkan
            — uangnya pernah keluar dan BKK-nya pernah terbit, jadi jejaknya tidak dihapus.
          </p>
        </>
      )}
    </Card>
  );
}
