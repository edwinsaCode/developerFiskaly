"use client";

import { ReactNode, useEffect, useId, useMemo, useState } from "react";
import {
  matchesQuery,
  pageCount,
  clampPage,
  pageSlice,
  rangeLabel,
} from "@/lib/table-view";

/**
 * Cari + halaman untuk tabel, satu perkakas dipakai semua tabel.
 *
 * Alasannya bukan sekadar ringkas: kalau tiap tabel menulis kotak carinya
 * sendiri, tiap tabel berpeluang beda perilaku — ada yang case-sensitive, ada
 * yang tidak me-reset halaman setelah difilter (dan tampil kosong). Logika
 * murninya ada di lib/table-view.ts dan diuji di sana.
 */

export interface TableViewOptions<T> {
  rows: T[];
  /** Kolom yang ikut dicari untuk satu baris. */
  searchFields: (row: T) => unknown[];
  /** Baris per halaman. 0 = tanpa halaman (tabel pendek yang wajar utuh). */
  pageSize?: number;
}

export interface TableView<T> {
  query: string;
  setQuery: (q: string) => void;
  page: number;
  setPage: (p: number) => void;
  /** Baris yang benar-benar dirender. */
  visible: T[];
  /** Baris setelah pencarian, sebelum dipotong per halaman. */
  matched: T[];
  total: number;
  pageSize: number;
  pages: number;
}

export function useTableView<T>({
  rows,
  searchFields,
  pageSize = 20,
}: TableViewOptions<T>): TableView<T> {
  const [query, setQueryRaw] = useState("");
  const [page, setPage] = useState(1);

  const matched = useMemo(
    () => (query.trim() ? rows.filter((r) => matchesQuery(searchFields(r), query)) : rows),
    // searchFields hampir selalu arrow inline di pemanggil — identitasnya baru
    // tiap render. Memasukkannya ke deps membuat memo ini tidak pernah kena,
    // jadi yang dipakai adalah data + kuerinya.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rows, query],
  );

  // Mengetik kata baru selalu mengembalikan pembaca ke halaman 1; kalau tidak,
  // ia bisa berdiri di halaman 5 dari hasil yang cuma punya 1 halaman.
  const setQuery = (q: string) => {
    setQueryRaw(q);
    setPage(1);
  };

  // Data bisa berubah dari luar (mis. setelah simpan) — halaman ikut ditarik
  // ke rentang yang masih ada.
  const safePage = clampPage(page, matched.length, pageSize);
  useEffect(() => {
    if (safePage !== page) setPage(safePage);
  }, [safePage, page]);

  return {
    query,
    setQuery,
    page: safePage,
    setPage,
    visible: pageSize > 0 ? pageSlice(matched, safePage, pageSize) : matched,
    matched,
    total: matched.length,
    pageSize,
    pages: pageSize > 0 ? pageCount(matched.length, pageSize) : 1,
  };
}

// ── Kotak cari ────────────────────────────────────────────────────────────────

interface TableSearchProps {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  /** Aksi di sisi kanan toolbar (tombol tambah, filter, dsb). */
  children?: ReactNode;
}

export function TableSearch({
  value,
  onChange,
  placeholder = "Cari…",
  children,
}: TableSearchProps) {
  const id = useId();
  return (
    <div className="flex flex-wrap items-center gap-3 px-4 py-3 border-b border-border">
      <div className="relative min-w-0 flex-1 sm:max-w-xs">
        <svg
          viewBox="0 0 20 20"
          aria-hidden="true"
          className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-text-tertiary"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
        >
          <circle cx="9" cy="9" r="5.5" />
          <path d="M13 13l4 4" />
        </svg>
        <input
          id={id}
          type="search"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          aria-label={placeholder}
          className="w-full rounded-lg border border-border bg-surface pl-8 pr-3 py-1.5 text-sm
            text-text-primary placeholder:text-text-tertiary
            focus:outline-none focus:ring-2 focus:ring-accent/30 focus:border-accent/50"
        />
      </div>
      {children && <div className="flex items-center gap-2 ml-auto">{children}</div>}
    </div>
  );
}

// ── Kaki tabel: rentang + navigasi halaman ────────────────────────────────────

interface TablePaginationProps {
  page: number;
  pages: number;
  pageSize: number;
  total: number;
  onPage: (p: number) => void;
}

export function TablePagination({
  page,
  pages,
  pageSize,
  total,
  onPage,
}: TablePaginationProps) {
  // Satu halaman penuh tidak perlu navigasi, tapi jumlah barisnya tetap
  // berguna — terutama sesudah mencari ("ketemu berapa?").
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-t border-border">
      <span className="text-xs text-text-secondary">
        {rangeLabel(page, pageSize, total)}
      </span>
      {pages > 1 && (
        <div className="flex items-center gap-1.5 ml-auto">
          <PageButton onClick={() => onPage(page - 1)} disabled={page <= 1} label="Sebelumnya">
            ‹
          </PageButton>
          <span className="text-xs text-text-secondary tabular px-1">
            {page} / {pages}
          </span>
          <PageButton onClick={() => onPage(page + 1)} disabled={page >= pages} label="Berikutnya">
            ›
          </PageButton>
        </div>
      )}
    </div>
  );
}

function PageButton({
  onClick,
  disabled,
  label,
  children,
}: {
  onClick: () => void;
  disabled: boolean;
  label: string;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className="h-7 w-7 grid place-items-center rounded-md border border-border text-sm
        text-text-secondary hover:text-text-primary hover:bg-border-subtle
        disabled:opacity-40 disabled:pointer-events-none
        focus:outline-none focus:ring-2 focus:ring-accent/30 transition-colors"
    >
      {children}
    </button>
  );
}

// ── Baris "tidak ketemu" ──────────────────────────────────────────────────────

/**
 * Hasil pencarian kosong BUKAN keadaan yang sama dengan tabel yang memang
 * belum berisi. Pesannya harus menyebut kata yang dicari, supaya pemakai tahu
 * datanya ada tapi kuerinya yang meleset.
 */
export function NoSearchResult({ query, colSpan }: { query: string; colSpan: number }) {
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-10 text-center">
        <p className="text-sm text-text-secondary">
          Tidak ada yang cocok dengan “{query}”.
        </p>
        <p className="text-xs text-text-tertiary mt-1">
          Coba kata yang lebih pendek, atau kosongkan kotak pencarian.
        </p>
      </td>
    </tr>
  );
}
