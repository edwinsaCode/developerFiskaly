"use client";

import { AllocationResult, Unit } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import {
  TableSearch,
  TablePagination,
  NoSearchResult,
  useTableView,
} from "@/components/ui/TableView";

// ── Ringkasan per unit untuk halaman detail proyek ────────────────────────────

interface ProjectCostTableProps {
  results: AllocationResult[];
  unitMap: Record<number, Unit>;
}

export function ProjectCostTable({ results, unitMap }: ProjectCostTableProps) {
  // Hook mendahului cabang kosong — hook tidak boleh di belakang early return.
  const view = useTableView({
    rows: results,
    searchFields: (r) => {
      const u = unitMap[r.unit_id];
      return [u?.code, u?.unit_type, u?.type_label, u?.buyer_name];
    },
    pageSize: 25,
  });

  if (results.length === 0) {
    return (
      <EmptyState
        title="Belum ada data alokasi"
        description="Konfigurasikan alokasi biaya proyek terlebih dahulu."
      />
    );
  }

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>Biaya Aktual Terakumulasi per Unit</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Realisasi yang <strong>sudah dibelanjakan</strong> sampai hari ini — biaya langsung +
          alokasi biaya bersama. Ini <strong>bukan</strong> HPP yang diakui saat penjualan; HPP
          diakui dari RAB (lihat rincian di halaman unit).
        </p>
      </CardHeader>
      <TableSearch
        value={view.query}
        onChange={view.setQuery}
        placeholder="Cari kode unit, tipe, atau pembeli…"
      />
      <Table>
        <TableHead>
          <TableRow>
            <Th>Unit</Th>
            <Th right>Tanah</Th>
            <Th right>Konstruksi</Th>
            <Th right>Lunak</Th>
            <Th right>Pendanaan</Th>
            <Th right>Total Biaya Aktual</Th>
          </TableRow>
        </TableHead>
        <TableBody>
          {view.visible.length === 0 ? (
            <NoSearchResult query={view.query} colSpan={6} />
          ) : view.visible.map(r => {
            const unit = unitMap[r.unit_id];
            return (
              <TableRow key={r.unit_id}>
                <Td>
                  <span className="font-medium">{unit?.code ?? `#${r.unit_id}`}</span>
                  {unit && (
                    <span className="ml-2 text-xs text-text-tertiary">{unit.unit_type}</span>
                  )}
                </Td>
                <Td right><Rupiah value={r.total.land} /></Td>
                <Td right><Rupiah value={r.total.hard} /></Td>
                <Td right><Rupiah value={r.total.soft} /></Td>
                <Td right><Rupiah value={r.total.financing} /></Td>
                <Td right>
                  <span className="font-semibold">
                    <Rupiah value={r.total.total} />
                  </span>
                </Td>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      <TablePagination
        page={view.page}
        pages={view.pages}
        pageSize={view.pageSize}
        total={view.total}
        onPage={view.setPage}
      />
    </Card>
  );
}

// ── Detail breakdown satu unit (langsung / alokasi / total) ───────────────────

interface UnitCostBreakdownProps {
  result: AllocationResult;
}

// "Total Biaya Aktual" — BUKAN "Total HPP".
//
// Kedua angka itu berbeda dan keduanya benar: yang ini realisasi belanja sampai
// hari ini, sedangkan HPP yang diakui saat penjualan berasal dari RAB (PSAK 44,
// di-true-up saat proyek selesai). Dulu baris ini bernama "Total HPP", sehingga
// satu halaman memajang dua angka berbeda dengan satu nama — dan pembaca wajar
// menyimpulkan sistemnya salah hitung. Nama yang jujur menutup salah paham itu.
const BREAKDOWN_ROWS = [
  { key: "direct",    label: "Langsung",           subtle: false },
  { key: "allocated", label: "Dialokasikan",       subtle: true  },
  { key: "total",     label: "Total Biaya Aktual", subtle: false },
] as const;

export function UnitCostBreakdown({ result }: UnitCostBreakdownProps) {
  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>Biaya Aktual Terakumulasi Unit</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Realisasi belanja sampai hari ini. Kategori yang belum dibelanjakan tampil nol —
          itu wajar, bukan data hilang. HPP yang diakui saat penjualan dihitung dari RAB,
          jadi angkanya memang berbeda dari tabel ini.
        </p>
      </CardHeader>
      <Table>
        <TableHead>
          <TableRow>
            <Th>Kategori</Th>
            <Th right>Tanah</Th>
            <Th right>Konstruksi</Th>
            <Th right>Lunak</Th>
            <Th right>Pendanaan</Th>
            <Th right>Total</Th>
          </TableRow>
        </TableHead>
        <TableBody>
          {BREAKDOWN_ROWS.map(row => {
            const bd = result[row.key];
            return (
              <TableRow key={row.key} subtle={row.subtle}>
                <Td>
                  <span className={row.key === "total" ? "font-semibold" : ""}>
                    {row.label}
                  </span>
                </Td>
                <Td right><Rupiah value={bd.land} /></Td>
                <Td right><Rupiah value={bd.hard} /></Td>
                <Td right><Rupiah value={bd.soft} /></Td>
                <Td right><Rupiah value={bd.financing} /></Td>
                <Td right>
                  <span className={row.key === "total" ? "font-semibold" : ""}>
                    <Rupiah value={bd.total} />
                  </span>
                </Td>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </Card>
  );
}
