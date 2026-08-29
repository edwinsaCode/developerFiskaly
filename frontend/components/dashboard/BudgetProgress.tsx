import { Rupiah } from "@/components/format/Rupiah";
import { Persen } from "@/components/format/Persen";

interface BudgetRow {
  label: string;
  budgeted: string;   // Rupiah string from API
  realized: string;   // Rupiah string from API
  pct: string;        // persen string from API e.g. "72.50"
}

interface BudgetProgressProps {
  rows: BudgetRow[];
  className?: string;
}

export function BudgetProgress({ rows, className = "" }: BudgetProgressProps) {
  return (
    <div className={`bg-surface border border-border rounded-lg p-5 ${className}`}>
      <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-4">
        RAB vs Realisasi
      </p>
      <div className="space-y-4">
        {rows.map(row => (
          <BudgetRow key={row.label} {...row} />
        ))}
      </div>
    </div>
  );
}

function BudgetRow({ label, budgeted, realized, pct }: BudgetRow) {
  const pctNum = parseFloat(pct);
  const barWidth = Math.min(100, Math.max(0, pctNum));
  const over = pctNum > 100;

  return (
    <div>
      <div className="flex justify-between items-baseline mb-1.5">
        <span className="text-sm text-text-primary">{label}</span>
        <span className={`text-sm font-semibold tabular ${over ? "text-danger" : "text-text-primary"}`}>
          <Persen value={pct} />
        </span>
      </div>
      <div className="h-1.5 rounded-full bg-border-subtle overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${over ? "bg-danger" : "bg-accent"}`}
          style={{ width: `${barWidth}%` }}
        />
      </div>
      <div className="flex justify-between mt-1">
        <span className="text-xs text-text-tertiary tabular">
          <Rupiah value={realized} />
        </span>
        <span className="text-xs text-text-tertiary tabular">
          / <Rupiah value={budgeted} />
        </span>
      </div>
    </div>
  );
}
