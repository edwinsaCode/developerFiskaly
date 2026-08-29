"use client";

// Daftar tagihan vendor (W-11).
//
// Layar ini membawa DUA status yang sengaja tidak dilebur: status pengakuan
// (draft/diakui/dibalik) dan status pembayaran (belum/sebagian/lunas). Tagihan
// yang diakui tetapi belum dibayar bukan tagihan setengah jadi — ia kewajiban
// penuh yang berdiri di neraca, dan meleburkannya jadi satu kolom akan menutupi
// justru pertanyaan yang paling sering ditanyakan: mana yang harus dibayar.
//
// Kolom "Sisa" dan status pembayaran datang UTUH dari backend (`outstanding`,
// `payment_status`). Layar tidak mengurangkan pembayaran dari total sendiri:
// sisa lahir dari sub-ledger alokasi, dan definisi kedua yang dihitung di sini
// akan menyimpang tanpa pernah memunculkan error.

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
import type { APInvoiceView, Project, Vendor } from "@/lib/types/api";
import {
  AP_STATUS_LABEL,
  apStatusVariant,
  filterInvoices,
  type InvoiceListFilter,
} from "@/components/ap/apRules";
import {
  AP_PAYMENT_STATUS_LABEL,
  apPaymentStatusVariant,
} from "@/components/ap/paymentRules";

interface Props {
  invoices: APInvoiceView[];
  vendors: Vendor[];
  projects: Project[];
  canWrite: boolean;
  loadError?: string;
}

export function APInvoiceList({ invoices, vendors, projects, canWrite, loadError }: Props) {
  const [filter, setFilter] = useState<InvoiceListFilter>({});

  const projectName = useMemo(() => {
    const m = new Map<number, string>();
    projects.forEach((p) => m.set(p.id, p.name));
    return m;
  }, [projects]);

  const rows = useMemo(() => filterInvoices(invoices, filter), [invoices, filter]);

  function set<K extends keyof InvoiceListFilter>(k: K, v: InvoiceListFilter[K]) {
    setFilter((f) => ({ ...f, [k]: v }));
  }

  const anyFilter =
    !!filter.q || !!filter.vendorId || !!filter.status || !!filter.from || !!filter.to;

  return (
    <Card padding="none">
      <div className="flex flex-wrap items-end gap-3 border-b border-border p-4">
        <Input
          label="Cari"
          wrapperClassName="min-w-[200px] flex-1"
          placeholder="Nomor tagihan, vendor, atau uraian"
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
          label="Status"
          wrapperClassName="w-44"
          value={filter.status ?? ""}
          onChange={(e) => set("status", e.target.value)}
        >
          <option value="">Semua status</option>
          <option value="draft">Draft</option>
          <option value="posted">Diakui</option>
          <option value="reversed">Dibalik</option>
        </Select>
        <Input
          label="Tanggal tagihan dari"
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
          <p className="text-sm text-danger">Daftar tagihan gagal dimuat: {loadError}</p>
        </div>
      ) : rows.length === 0 ? (
        <EmptyState
          title={invoices.length === 0 ? "Belum ada tagihan vendor" : "Tidak ada tagihan yang cocok"}
          description={
            invoices.length === 0
              ? "Tagihan vendor adalah kewajiban yang lahir saat pekerjaan atau barangnya diterima — sebelum uangnya keluar. Catat tagihan pertama untuk mulai membentuk saldo hutang usaha dan realisasi RAB."
              : "Ubah saringan atau kosongkan pencarian untuk melihat seluruh tagihan."
          }
          action={
            invoices.length === 0 && canWrite ? (
              <Link href="/accounting/hutang/baru">
                <Button>Catat Tagihan Vendor</Button>
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
                <Th>Nomor Tagihan</Th>
                <Th>Vendor</Th>
                <Th>Tgl Tagihan</Th>
                <Th>Jatuh Tempo</Th>
                <Th>Proyek</Th>
                <Th right>DPP</Th>
                <Th right>PPN Masukan</Th>
                <Th right>Total Hutang</Th>
                <Th right>Sisa</Th>
                <Th>Pengakuan</Th>
                <Th>Pembayaran</Th>
              </tr>
            </TableHead>
            <TableBody>
              {rows.map((inv) => (
                <TableRow key={inv.id} subtle={inv.status === "reversed"}>
                  <Td>
                    <Link
                      href={`/accounting/hutang/${inv.id}`}
                      className="font-medium text-accent hover:underline"
                    >
                      {inv.invoice_number}
                    </Link>
                    {inv.description && (
                      <p className="mt-0.5 text-[11px] text-text-tertiary">{inv.description}</p>
                    )}
                  </Td>
                  <Td>{inv.vendor_name || `#${inv.vendor_id}`}</Td>
                  <Td>
                    <Tanggal value={inv.invoice_date} />
                  </Td>
                  <Td>
                    <Tanggal value={inv.due_date} />
                  </Td>
                  <Td>
                    {inv.project_id ? (
                      projectName.get(inv.project_id) ?? `#${inv.project_id}`
                    ) : (
                      <span className="text-text-tertiary">Overhead</span>
                    )}
                  </Td>
                  <Td right mono>
                    <Rupiah value={inv.dpp_amount} colorSign={false} />
                  </Td>
                  <Td right mono>
                    <Rupiah value={inv.ppn_amount} colorSign={false} />
                  </Td>
                  <Td right mono>
                    <Rupiah value={inv.payable_amount} colorSign={false} />
                  </Td>
                  <Td right mono>
                    {inv.status === "posted" ? (
                      <Rupiah value={inv.outstanding} colorSign={false} />
                    ) : (
                      // Tagihan draft/dibalik tidak punya kewajiban yang berdiri,
                      // jadi "sisa 0" akan terbaca seperti sudah lunas.
                      <span className="text-text-tertiary">—</span>
                    )}
                  </Td>
                  <Td>
                    <Badge variant={apStatusVariant(inv.status)}>
                      {AP_STATUS_LABEL[inv.status]}
                    </Badge>
                  </Td>
                  <Td>
                    <Badge variant={apPaymentStatusVariant(inv.payment_status)}>
                      {AP_PAYMENT_STATUS_LABEL[inv.payment_status]}
                    </Badge>
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <p className="border-t border-border px-4 py-2.5 text-[11px] text-text-tertiary">
            {rows.length} dari {invoices.length} tagihan. &quot;Pengakuan&quot; menjawab apakah
            kewajibannya sudah berdiri di buku; &quot;Pembayaran&quot; menjawab berapa yang sudah
            dilunasi. Keduanya dihitung backend — kolom Sisa berasal dari alokasi pembayaran,
            bukan dari pengurangan di layar ini.
          </p>
        </>
      )}
    </Card>
  );
}
