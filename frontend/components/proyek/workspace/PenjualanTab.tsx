"use client";

import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { StatusBadge } from "@/components/ui/Badge";
import { Rupiah } from "@/components/format/Rupiah";
import { EmptyState } from "@/components/ui/EmptyState";
import {
  TableSearch,
  TablePagination,
  NoSearchResult,
  useTableView,
} from "@/components/ui/TableView";
import type { Unit } from "@/lib/types/api";
import { buyerRoleLabel } from "@/lib/unit-label";

// PS-2 — Tab Penjualan workspace: seluruh unit + status siklus jual; satu klik
// ke panel penjualan unit (kontrak/termin/BAST/pembatalan).
export function PenjualanTab({ units }: { units: Unit[] }) {
  const order: Record<string, number> = {
    reserved: 0, ppjb: 1, booked: 2, available: 3, sold: 4,
    occupied: 5, hold: 6, blocked: 7, maintenance: 8,
  };
  const sorted = [...units].sort(
    (a, b) => (order[a.status] ?? 9) - (order[b.status] ?? 9) || a.code.localeCompare(b.code),
  );

  // Hook dipanggil sebelum cabang "belum ada unit" — hook tidak boleh berada
  // di belakang early return, dan daftar kosong ditangani hook dengan wajar.
  const view = useTableView({
    rows: sorted,
    searchFields: (u) => [u.code, u.unit_type, u.type_label, u.buyer_name, u.status],
    pageSize: 25,
  });

  if (units.length === 0) {
    return (
      <EmptyState
        title="Belum ada unit"
        description="Tambahkan unit di tab Unit terlebih dahulu, lalu kelola siklus jual per unit dari sini."
      />
    );
  }

  return (
    <Card padding="none">
      <TableSearch
        value={view.query}
        onChange={view.setQuery}
        placeholder="Cari kode unit, tipe, atau nama pembeli…"
      />
      <Table>
        <TableHead>
          <TableRow>
            <Th>Unit</Th>
            <Th>Status</Th>
            <Th>Pembeli</Th>
            <Th right>Harga List</Th>
            <Th right>Harga Jual</Th>
            <Th> </Th>
          </TableRow>
        </TableHead>
        <TableBody>
          {view.visible.length === 0 ? (
            <NoSearchResult query={view.query} colSpan={6} />
          ) : (
            view.visible.map((u) => (
              <TableRow key={u.id}>
                <Td><span className="font-medium">{u.code}</span>
                  <span className="block text-xs text-text-tertiary">{u.unit_type}</span>
                </Td>
                <Td><StatusBadge status={u.status} /></Td>
                {/* buyer_name terisi sejak booking/kontrak; buyer_ref baru saat
                    BAST — kolom ini dulu kosong untuk semua unit ppjb. */}
                <Td>
                  {u.buyer_name ? (
                    <>
                      <span>{u.buyer_name}</span>
                      <span className="block text-xs text-text-tertiary">
                        {buyerRoleLabel(u.buyer_source)}
                      </span>
                    </>
                  ) : (
                    <span className="text-text-tertiary">—</span>
                  )}
                </Td>
                <Td right><Rupiah value={u.list_price} colorSign={false} /></Td>
                <Td right>
                  {u.sale_price
                    ? <Rupiah value={u.sale_price} colorSign={false} />
                    : <span className="text-text-tertiary">—</span>}
                </Td>
                <Td right>
                  <Link
                    href={`/penjualan/${u.id}`}
                    className="text-sm text-accent hover:underline font-medium"
                  >
                    Kelola →
                  </Link>
                </Td>
              </TableRow>
            ))
          )}
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
