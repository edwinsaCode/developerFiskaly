import { ReactNode } from "react";
import { Rupiah } from "@/components/format/Rupiah";
import { Persen } from "@/components/format/Persen";

interface MetricCardProps {
  label: string;
  value: string;          // raw string from API
  type?: "money" | "persen" | "plain";
  caption?: string;
  trend?: ReactNode;
  className?: string;
}

export function MetricCard({ label, value, type = "money", caption, trend, className = "" }: MetricCardProps) {
  return (
    <div className={`bg-surface border border-border rounded-lg p-5 flex flex-col gap-1 ${className}`}>
      <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide">{label}</p>
      <p className="text-2xl font-bold text-text-primary num-right tabular leading-tight">
        {type === "money"  && <Rupiah value={value} />}
        {type === "persen" && <Persen value={value} />}
        {type === "plain"  && value}
      </p>
      {(caption || trend) && (
        <div className="flex items-center gap-2 mt-0.5">
          {caption && <span className="text-xs text-text-tertiary">{caption}</span>}
          {trend}
        </div>
      )}
    </div>
  );
}
