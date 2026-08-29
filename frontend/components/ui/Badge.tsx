export type BadgeVariant =
  | "default"
  | "success"
  | "danger"
  | "warning"
  | "accent"
  | "neutral";

interface BadgeProps {
  variant?: BadgeVariant;
  className?: string;
  children: React.ReactNode;
}

const variantClasses: Record<BadgeVariant, string> = {
  default: "bg-border-subtle text-text-secondary",
  success: "bg-success-bg text-success",
  danger: "bg-danger-bg text-danger",
  warning: "bg-warning-bg text-warning",
  accent: "bg-accent-light text-accent",
  neutral: "bg-border text-text-primary",
};

export function Badge({
  variant = "default",
  className = "",
  children,
}: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium
        ${variantClasses[variant]} ${className}`}
    >
      {children}
    </span>
  );
}

// Badge konvensi status domain (proyek, fase, unit, pajak, dll)
export function StatusBadge({ status }: { status: string }) {
  const map: Record<string, BadgeVariant> = {
    // unit (Increment 6 — 9 status lifecycle)
    available: "accent",
    booked:    "accent",
    reserved: "warning",
    ppjb:      "warning",
    sold:      "success",
    occupied:  "neutral",
    hold:      "warning",
    blocked:   "danger",
    maintenance: "neutral",
    // rab plan
    draft:     "warning",
    // proyek
    planning:  "neutral",
    selling:   "accent",
    completed: "success",
    // fase (active di-handle oleh "active" di bawah)
    // pajak / kontrak
    outstanding: "warning",
    paid:        "success",
    overdue:     "danger",
    active:      "success",
    superseded:  "neutral",
  };
  const labels: Record<string, string> = {
    available:   "Tersedia",
    booked:      "Dibooking",
    reserved:    "Dipesan",
    ppjb:        "PPJB",
    sold:        "Terjual",
    occupied:    "Dihuni",
    hold:        "Hold",
    blocked:     "Diblokir",
    maintenance: "Perbaikan",
    draft:       "Draf",
    planning:    "Perencanaan",
    selling:     "Penjualan",
    completed:   "Selesai",
    outstanding: "Belum Bayar",
    paid:        "Lunas",
    overdue:     "Jatuh Tempo",
    active:      "Aktif",
    superseded:  "Diganti",
  };
  return (
    <Badge variant={map[status] ?? "default"}>
      {labels[status] ?? status}
    </Badge>
  );
}
