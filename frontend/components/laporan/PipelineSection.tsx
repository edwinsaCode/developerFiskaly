import type { PipelineReport } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { ReportEmptyCTA } from "@/components/laporan/ReportEmptyCTA";

interface Props {
  data: PipelineReport | null;
  error: boolean;
}

export function PipelineSection({ data, error }: Props) {
  if (error) {
    return <Card><p className="text-sm text-danger">Gagal memuat Pipeline Penjualan. Coba lagi nanti.</p></Card>;
  }
  if (!data || data.total_units === 0) {
    return <ReportEmptyCTA kind="pipeline" />;
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <TotalPill label="Total Unit" value={String(data.total_units)} isCount />
        <TotalPill label="Proyeksi Pendapatan" value={data.projected_revenue} isRupiah />
        <TotalPill label="Unit Terjual" value={String(data.sold.count)} isCount highlight="success" />
        <TotalPill label="Nilai Terjual" value={data.sold.total_contract_value ?? "0"} isRupiah highlight="success" />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <StatusCard
          label="Tersedia"
          icon="○"
          iconClass="text-text-tertiary"
          stat={data.available}
          showContractValue={false}
        />
        <StatusCard
          label="Reserved"
          icon="◎"
          iconClass="text-warning"
          stat={data.reserved}
          showContractValue
        />
        <StatusCard
          label="Terjual"
          icon="●"
          iconClass="text-success"
          stat={data.sold}
          showContractValue
        />
      </div>
    </div>
  );
}

function StatusCard({ label, icon, iconClass, stat, showContractValue }: {
  label: string;
  icon: string;
  iconClass: string;
  stat: PipelineReport["available"];
  showContractValue: boolean;
}) {
  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5 pb-3">
        <CardTitle>
          <span className={`mr-2 ${iconClass}`}>{icon}</span>
          {label}
        </CardTitle>
      </CardHeader>
      <div className="divide-y divide-border">
        <StatRow label="Jumlah Unit" value={String(stat.count)} isCount />
        <StatRow label="Total Harga List" value={stat.total_list_price} isRupiah />
        {stat.total_advance && (
          <StatRow label="Total Uang Muka" value={stat.total_advance} isRupiah />
        )}
        {showContractValue && stat.total_contract_value && (
          <StatRow label="Nilai Kontrak" value={stat.total_contract_value} isRupiah />
        )}
      </div>
    </Card>
  );
}

function StatRow({ label, value, isRupiah, isCount }: {
  label: string;
  value: string;
  isRupiah?: boolean;
  isCount?: boolean;
}) {
  return (
    <div className="flex justify-between items-center px-5 py-3 text-sm">
      <span className="text-text-secondary">{label}</span>
      {isRupiah
        ? <span className="tabular-nums font-medium"><Rupiah value={value} colorSign={false} /></span>
        : <span className="font-semibold">{value}</span>
      }
    </div>
  );
}

function TotalPill({ label, value, isRupiah, isCount, highlight }: {
  label: string;
  value: string;
  isRupiah?: boolean;
  isCount?: boolean;
  highlight?: "success" | "danger";
}) {
  const highlightClass = highlight === "success"
    ? "border-success/30 bg-success-bg"
    : highlight === "danger"
    ? "border-danger/30 bg-danger-bg"
    : "";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 ${highlightClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className="text-sm font-bold tabular-nums text-text-primary">
        {isRupiah
          ? <Rupiah value={value} colorSign={false} />
          : value
        }
      </p>
    </div>
  );
}
