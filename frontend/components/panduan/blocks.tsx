import { ReactNode } from "react";
import { Card } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Rupiah } from "@/components/format/Rupiah";

// Blok presentasi untuk halaman Panduan Sistem (/panduan). Semua komponen di
// sini murni tampilan — tidak ada fetch/API, kontennya datang dari file
// content-*.tsx yang mengutip docs/SYSTEM-DOCUMENTATION.md apa adanya.

export function Lead({ children }: { children: ReactNode }) {
  return <p className="text-[15px] leading-relaxed text-text-secondary">{children}</p>;
}

export function P({ children }: { children: ReactNode }) {
  return <p className="text-sm leading-relaxed text-text-secondary">{children}</p>;
}

export function UL({ items }: { items: ReactNode[] }) {
  return (
    <ul className="list-disc space-y-1.5 pl-5 text-sm leading-relaxed text-text-secondary marker:text-accent/60">
      {items.map((it, i) => (
        <li key={i}>{it}</li>
      ))}
    </ul>
  );
}

export function OL({ items }: { items: ReactNode[] }) {
  return (
    <ol className="list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-text-secondary marker:font-semibold marker:text-accent">
      {items.map((it, i) => (
        <li key={i} className="pl-1">{it}</li>
      ))}
    </ol>
  );
}

export function H3({ children }: { children: ReactNode }) {
  return <h3 className="text-base font-semibold text-text-primary">{children}</h3>;
}

export function Code({ children }: { children: ReactNode }) {
  return (
    <code className="rounded bg-border-subtle px-1.5 py-0.5 font-mono text-[12px] text-text-primary whitespace-nowrap">
      {children}
    </code>
  );
}

/** Tautan lompat ke section lain di halaman yang sama (mis. lihat §Booking). */
export function XRef({ id, children }: { id: string; children: ReactNode }) {
  return (
    <a href={`#${id}`} className="font-medium text-accent underline decoration-accent/30 underline-offset-2 hover:decoration-accent">
      {children}
    </a>
  );
}

type CalloutVariant = "info" | "warning" | "danger" | "frozen";

const CALLOUT_STYLES: Record<CalloutVariant, string> = {
  info: "bg-accent-light border-accent/30",
  warning: "bg-warning-bg border-warning/30",
  danger: "bg-danger-bg border-danger/30",
  frozen: "bg-border-subtle border-border",
};

const CALLOUT_LABELS: Record<CalloutVariant, string> = {
  info: "Info",
  warning: "Perhatian",
  danger: "Defect / Belum Selesai",
  frozen: "Aturan Final (Frozen)",
};

export function Callout({
  variant = "info",
  title,
  children,
}: {
  variant?: CalloutVariant;
  title?: string;
  children: ReactNode;
}) {
  return (
    <div className={`rounded-lg border px-4 py-3 ${CALLOUT_STYLES[variant]}`}>
      <p className="eyebrow mb-1.5">{title ?? CALLOUT_LABELS[variant]}</p>
      <div className="space-y-1.5 text-sm leading-relaxed text-text-secondary">{children}</div>
    </div>
  );
}

/** Tabel data generik: header string[], baris ReactNode[][]. rightCols = index kolom yang rata kanan (nominal). */
export function DataTable({
  head,
  rows,
  rightCols = [],
}: {
  head: string[];
  rows: ReactNode[][];
  rightCols?: number[];
}) {
  return (
    <Card padding="none">
      <Table>
        <TableHead>
          <TableRow>
            {head.map((h, i) => (
              <Th key={i} right={rightCols.includes(i)}>{h}</Th>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((r, ri) => (
            <TableRow key={ri}>
              {r.map((c, ci) => (
                <Td key={ci} right={rightCols.includes(ci)}>{c}</Td>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  );
}

export interface JournalLine {
  account: string;
  label: string;
  amount: number;
}

interface JournalEntryCardProps {
  title: string;
  when: string;
  debit: JournalLine[];
  credit: JournalLine[];
  note?: ReactNode;
  neraca?: ReactNode;
  labaRugi?: ReactNode;
}

/** Kartu contoh jurnal — Dr di atas, Cr menjorok, total otomatis dicek balance. */
export function JournalEntryCard({ title, when, debit, credit, note, neraca, labaRugi }: JournalEntryCardProps) {
  const totalDebit = debit.reduce((s, l) => s + l.amount, 0);
  const totalCredit = credit.reduce((s, l) => s + l.amount, 0);
  const balanced = totalDebit === totalCredit;

  return (
    <Card padding="none" className="overflow-hidden">
      <div className="flex flex-wrap items-start justify-between gap-2 border-b border-border bg-border-subtle/40 px-4 py-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-text-primary">{title}</p>
          <p className="mt-0.5 text-xs text-text-tertiary">{when}</p>
        </div>
        <Badge variant={balanced ? "success" : "danger"}>
          {balanced ? "Dr = Cr" : "TIDAK BALANCE"}
        </Badge>
      </div>
      <Table>
        <TableBody>
          {debit.map((l, i) => (
            <TableRow key={`d-${i}`}>
              <Td className="w-10">
                <span className="text-xs font-bold text-accent">Dr</span>
              </Td>
              <Td>
                <Code>{l.account}</Code> <span className="ml-1.5">{l.label}</span>
              </Td>
              <Td right><Rupiah value={l.amount} /></Td>
            </TableRow>
          ))}
          {credit.map((l, i) => (
            <TableRow key={`c-${i}`}>
              <Td className="w-10 pl-7">
                <span className="text-xs font-bold text-text-secondary">Cr</span>
              </Td>
              <Td className="pl-7">
                <Code>{l.account}</Code> <span className="ml-1.5">{l.label}</span>
              </Td>
              <Td right><Rupiah value={l.amount} /></Td>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {(neraca || labaRugi) && (
        <div className="grid gap-3 border-t border-border px-4 py-3 sm:grid-cols-2">
          {neraca && (
            <div className="border-l-2 border-accent/40 pl-3">
              <p className="eyebrow">Dampak Neraca</p>
              <p className="mt-0.5 text-sm text-text-secondary">{neraca}</p>
            </div>
          )}
          {labaRugi && (
            <div className="border-l-2 border-success/40 pl-3">
              <p className="eyebrow">Dampak Laba/Rugi</p>
              <p className="mt-0.5 text-sm text-text-secondary">{labaRugi}</p>
            </div>
          )}
        </div>
      )}
      {note && <p className="border-t border-border px-4 py-2.5 text-xs text-text-tertiary">{note}</p>}
    </Card>
  );
}

export function FlowStep({
  n,
  title,
  isLast = false,
  children,
}: {
  n: number;
  title: string;
  isLast?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="relative pl-12 pb-10 last:pb-0">
      {!isLast && <span className="absolute left-[15px] top-8 bottom-0 w-px bg-border" aria-hidden="true" />}
      <span className="absolute left-0 top-0 flex h-8 w-8 items-center justify-center rounded-full bg-accent text-sm font-bold text-white shadow-sm">
        {n}
      </span>
      <h3 className="text-base font-semibold leading-tight text-text-primary">{title}</h3>
      <div className="mt-2.5 space-y-3">{children}</div>
    </div>
  );
}

/** Kotak expand/collapse untuk tabel panjang (glossary, referensi jurnal penuh, COA). */
export function Collapsible({
  title,
  defaultOpen = false,
  children,
}: {
  title: string;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  return (
    <details open={defaultOpen} className="group rounded-lg border border-border bg-surface">
      <summary
        className="flex cursor-pointer list-none items-center justify-between px-4 py-3 text-sm font-semibold
          text-text-primary [&::-webkit-details-marker]:hidden"
      >
        {title}
        <svg
          width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
          strokeLinecap="round" strokeLinejoin="round"
          className="shrink-0 text-text-tertiary transition-transform group-open:rotate-180"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </summary>
      <div className="border-t border-border px-4 py-4">{children}</div>
    </details>
  );
}

export function SectionHeader({ eyebrow, title, lead }: { eyebrow: string; title: string; lead?: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <p className="eyebrow">{eyebrow}</p>
      <h2 className="display-lg text-xl text-text-primary sm:text-2xl">{title}</h2>
      {lead && <div className="max-w-3xl pt-1">{lead}</div>}
    </div>
  );
}
