"use client";

import { useMemo } from "react";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { EmptyState } from "@/components/ui/EmptyState";
import {
  TableSearch,
  TablePagination,
  NoSearchResult,
  useTableView,
} from "@/components/ui/TableView";
import { PrintExpenseReceiptLink } from "./PrintExpenseReceiptLink";
import { LinkifiedText } from "@/components/documents/LinkifiedText";
import type { CostEntry, ExpenseListItem, PaymentMethod } from "@/lib/types/api";
import { constructionSubcategoryLabel } from "@/lib/constants/constructionSubcategory";

// Termasuk kategori beban (marketing/other) karena pengeluaran operasional yang
// di-tag ke proyek ikut muncul di daftar ini — tanpa label, ia tampil sebagai
// kode mentah.
const CATEGORY_LABELS: Record<string, string> = {
  land:        "Tanah",
  hard:        "Hard Cost",
  soft:        "Soft Cost",
  operational: "Operasional", // dahulu "financing"/Pendanaan
  marketing:   "Pemasaran",
  other:       "Biaya Lain-lain",
};

const PAYMENT_LABELS: Record<PaymentMethod, string> = {
  bank:    "Bank/Kas",
  payable: "Hutang Usaha",
};

/**
 * Riwayat biaya proyek — daftar yang paling cepat memanjang di seluruh aplikasi
 * (satu proyek berjalan bisa ratusan entri dalam sebulan).
 *
 * Yang ikut dicari sengaja termasuk NOMOR DOKUMEN: cara orang mencari biaya di
 * sini hampir selalu berangkat dari selembar bukti di tangan, bukan dari
 * ingatan soal tanggal.
 */
export function CostEntryTable({
  entries,
  expenses,
  canWrite,
  token,
}: {
  entries: CostEntry[];
  /** Metadata dari modul Pengeluaran (nama jenis, nomor dokumen, penanda RAB). */
  expenses: ExpenseListItem[];
  canWrite: boolean;
  /** Dipakai untuk cetak Bukti Kas Keluar per baris (lihat PrintExpenseReceiptLink). */
  token: string;
}) {
  const expenseMeta = useMemo(
    () => new Map<number, ExpenseListItem>(expenses.map((e) => [e.id, e])),
    [expenses],
  );

  const view = useTableView({
    rows: entries,
    searchFields: (e) => {
      const meta = expenseMeta.get(e.id);
      return [
        e.date,
        e.vendor,
        e.description,
        meta?.expense_type_name ?? CATEGORY_LABELS[e.category] ?? e.category,
        meta?.document_number,
        e.amount,
      ];
    },
    pageSize: 25,
  });

  return (
    <Card padding="none">
      <div className="px-5 py-4 border-b border-border">
        <h2 className="text-sm font-semibold text-text-primary">Riwayat Biaya</h2>
        <p className="text-xs text-text-secondary mt-0.5">
          {entries.length} entri — semua jurnal sudah diposting (immutable)
        </p>
      </div>

      {entries.length === 0 ? (
        <EmptyState
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" /><path d="M12 8v4M12 16h.01" />
            </svg>
          }
          title="Belum ada biaya"
          description={canWrite
            ? "Gunakan form di atas untuk mencatat biaya proyek pertama."
            : "Biaya proyek yang dicatat akan muncul di sini."
          }
        />
      ) : (
        <>
          <TableSearch
            value={view.query}
            onChange={view.setQuery}
            placeholder="Cari vendor, deskripsi, jenis biaya, atau nomor dokumen…"
          />
          <Table>
            <TableHead>
              <TableRow>
                <Th>Tanggal</Th>
                <Th>Kategori</Th>
                <Th>Vendor</Th>
                <Th>Deskripsi</Th>
                <Th>Metode</Th>
                <Th right>Jumlah</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {view.visible.length === 0 ? (
                <NoSearchResult query={view.query} colSpan={6} />
              ) : view.visible.map((entry) => {
                const meta = expenseMeta.get(entry.id);
                return (
                  <TableRow key={entry.id}>
                    <Td><Tanggal value={entry.date} /></Td>
                    <Td>
                      <span className="text-xs font-medium px-1.5 py-0.5 rounded bg-accent-light text-accent">
                        {meta?.expense_type_name ?? CATEGORY_LABELS[entry.category] ?? entry.category}
                      </span>
                      {/* BD-2: hanya biaya yang tertaut item RAB yang terhitung
                          sebagai realisasi anggaran — dibuat terlihat di sini
                          supaya tidak perlu dibuka satu per satu. */}
                      {meta?.is_rab_realization && (
                        <span className="ml-1 text-[10px] font-medium px-1.5 py-0.5 rounded bg-success-bg text-success">
                          RAB
                        </span>
                      )}
                      {/* UAT 2026-09-07: Produksi Subsidi vs Komersial harus
                          terlihat langsung di riwayat, bukan hanya di RAB —
                          di sinilah pool mana yang menerima biaya ini terlihat. */}
                      {entry.hard_subcategory && (
                        <div className="mt-0.5 text-[10px] text-text-tertiary">
                          {constructionSubcategoryLabel(entry.hard_subcategory)}
                        </div>
                      )}
                    </Td>
                    <Td>{entry.vendor}</Td>
                    <Td>
                      <span className="text-text-secondary">
                        <LinkifiedText token={token} text={entry.description} />
                      </span>
                      {meta?.document_number && (
                        <PrintExpenseReceiptLink
                          token={token}
                          expenseId={entry.id}
                          documentNumber={meta.document_number}
                        />
                      )}
                    </Td>
                    <Td>
                      <span className={`text-xs px-1.5 py-0.5 rounded ${
                        entry.payment_method === "bank"
                          ? "bg-success-bg text-success"
                          : "bg-warning-bg text-warning"
                      }`}>
                        {PAYMENT_LABELS[entry.payment_method]}
                      </span>
                    </Td>
                    <Td right><Rupiah value={entry.amount} /></Td>
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
        </>
      )}
    </Card>
  );
}
