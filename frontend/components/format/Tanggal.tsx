// <Tanggal> — format tanggal ke format Indonesia.
// value: string ISO 8601 dari API

interface TanggalProps {
  /** ISO 8601 string dari API. */
  value: string;
  /** "short" = "15 Jan 2024", "long" = "15 Januari 2024". Default "short". */
  format?: "short" | "long";
  className?: string;
}

export function Tanggal({ value, format = "short", className = "" }: TanggalProps) {
  if (!value) return <span className={className}>—</span>;

  const date = new Date(value);
  if (isNaN(date.getTime())) return <span className={className}>—</span>;

  const formatted = new Intl.DateTimeFormat("id-ID", {
    day: "numeric",
    month: format === "long" ? "long" : "short",
    year: "numeric",
  }).format(date);

  return <span className={`whitespace-nowrap ${className}`.trim()}>{formatted}</span>;
}
