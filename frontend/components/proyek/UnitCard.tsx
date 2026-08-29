import Link from "next/link";
import { Unit } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { StatusBadge } from "@/components/ui/Badge";
import { buyerRoleLabel } from "@/lib/unit-label";

interface UnitCardProps {
  unit: Unit;
  projectId: number;
}

export function UnitCard({ unit, projectId }: UnitCardProps) {
  return (
    <Link
      href={`/proyek/${projectId}/unit/${unit.id}`}
      className="block bg-surface border border-border rounded-lg p-3
        hover:border-accent/40 hover:shadow-sm transition-all group"
    >
      <div className="flex items-start justify-between mb-2 gap-2">
        <span className="text-sm font-semibold text-text-primary group-hover:text-accent transition-colors">
          {unit.code}
        </span>
        <StatusBadge status={unit.status} />
      </div>

      <p className="text-xs text-text-secondary">
        {unit.unit_type}
        {unit.type_label ? <span className="text-text-tertiary"> · {unit.type_label}</span> : null}
      </p>
      <p className="text-xs text-text-tertiary mt-0.5">{unit.saleable_area} m²</p>

      <div className="mt-3 pt-2.5 border-t border-border-subtle">
        <p className="text-xs text-text-tertiary mb-0.5">Harga</p>
        <div className="text-sm font-semibold">
          <Rupiah value={unit.list_price} colorSign={false} />
        </div>
      </div>

      {/* buyer_name, bukan buyer_ref: buyer_ref baru terisi saat BAST, sehingga
          unit `booked`/`ppjb` dulu tampil tanpa nama sama sekali. */}
      {unit.buyer_name && (
        <p className="mt-2 text-xs text-text-tertiary truncate">
          {buyerRoleLabel(unit.buyer_source)}: {unit.buyer_name}
        </p>
      )}
    </Link>
  );
}
