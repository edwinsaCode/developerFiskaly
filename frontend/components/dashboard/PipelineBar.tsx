import { Rupiah } from "@/components/format/Rupiah";
import { PipelineReport } from "@/lib/types/api";

interface PipelineBarProps {
  data: PipelineReport;
}

export function PipelineBar({ data }: PipelineBarProps) {
  const total = data.total_units;
  const sold = data.sold.count;
  const reserved = data.reserved.count;
  const available = data.available.count;

  const soldPct     = total > 0 ? (sold / total) * 100 : 0;
  const reservedPct = total > 0 ? (reserved / total) * 100 : 0;
  const availPct    = total > 0 ? (available / total) * 100 : 0;

  return (
    <div className="bg-surface border border-border rounded-lg p-5">
      <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-3">
        Pipeline Unit
      </p>

      <div className="h-3 rounded-full overflow-hidden flex bg-border-subtle mb-4">
        <div style={{ width: `${soldPct}%` }}     className="bg-success" />
        <div style={{ width: `${reservedPct}%` }} className="bg-warning" />
        <div style={{ width: `${availPct}%` }}    className="bg-accent/30" />
      </div>

      <div className="flex gap-6">
        <LegendItem
          color="bg-success"
          label="Terjual"
          count={sold}
          value={data.sold.total_contract_value ?? data.sold.total_list_price}
        />
        <LegendItem
          color="bg-warning"
          label="Dipesan"
          count={reserved}
          value={data.reserved.total_advance ?? data.reserved.total_list_price}
        />
        <LegendItem
          color="bg-accent/30"
          label="Tersedia"
          count={available}
          value={data.available.total_list_price}
        />
      </div>
    </div>
  );
}

function LegendItem({
  color, label, count, value
}: { color: string; label: string; count: number; value: string }) {
  return (
    <div className="flex items-start gap-2 min-w-0">
      <div className={`w-2.5 h-2.5 rounded-sm mt-0.5 shrink-0 ${color}`} />
      <div className="min-w-0">
        <p className="text-xs text-text-secondary">{label} ({count})</p>
        <p className="text-sm font-semibold text-text-primary tabular num-right">
          <Rupiah value={value} />
        </p>
      </div>
    </div>
  );
}
