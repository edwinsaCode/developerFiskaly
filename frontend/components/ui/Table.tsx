import { ReactNode, HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from "react";

// ── Table ─────────────────────────────────────────────────────────────────────

interface TableProps {
  children: ReactNode;
  className?: string;
}

export function Table({ children, className = "" }: TableProps) {
  return (
    <div className={`overflow-auto scrollbar-thin ${className}`}>
      <table className="w-full text-sm border-collapse">
        {children}
      </table>
    </div>
  );
}

// ── TableHead ─────────────────────────────────────────────────────────────────

export function TableHead({ children }: { children: ReactNode }) {
  return (
    <thead>
      {children}
    </thead>
  );
}

// ── TableBody ─────────────────────────────────────────────────────────────────

export function TableBody({ children }: { children: ReactNode }) {
  return <tbody>{children}</tbody>;
}

// ── TableRow ──────────────────────────────────────────────────────────────────

interface TableRowProps extends HTMLAttributes<HTMLTableRowElement> {
  children: ReactNode;
  subtle?: boolean;
}

export function TableRow({ children, subtle = false, className = "", ...props }: TableRowProps) {
  return (
    <tr
      className={`border-b border-border-subtle last:border-0
        ${subtle ? "bg-border-subtle/40" : "hover:bg-border-subtle/60 transition-colors"}
        ${className}`}
      {...props}
    >
      {children}
    </tr>
  );
}

// ── TableTh ───────────────────────────────────────────────────────────────────

interface ThProps extends ThHTMLAttributes<HTMLTableCellElement> {
  right?: boolean;
}

export function Th({ right = false, className = "", children, ...props }: ThProps) {
  return (
    <th
      className={`px-3 py-2.5 text-xs font-semibold text-text-secondary uppercase tracking-wide
        border-b border-border bg-border-subtle/40
        ${right ? "text-right" : "text-left"}
        ${className}`}
      {...props}
    >
      {children}
    </th>
  );
}

// ── TableTd ───────────────────────────────────────────────────────────────────

interface TdProps extends TdHTMLAttributes<HTMLTableCellElement> {
  right?: boolean;
  mono?: boolean;
}

export function Td({
  right = false,
  mono = false,
  className = "",
  children,
  ...props
}: TdProps) {
  return (
    <td
      className={`px-3 py-2.5 text-sm text-text-primary
        ${right ? "num-right tabular" : ""}
        ${mono ? "font-mono text-xs" : ""}
        ${className}`}
      {...props}
    >
      {children}
    </td>
  );
}
