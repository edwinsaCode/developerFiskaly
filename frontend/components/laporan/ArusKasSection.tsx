import type { ArusKasReport } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ReportEmptyCTA } from "@/components/laporan/ReportEmptyCTA";

interface Props {
  data: ArusKasReport | null;
  error: boolean;
}

export function ArusKasSection({ data, error }: Props) {
  if (error) {
    return <Card><p className="text-sm text-danger">Gagal memuat Arus Kas. Coba lagi nanti.</p></Card>;
  }
  if (!data) {
    return <ReportEmptyCTA kind="arus-kas" />;
  }
  const noMovements =
    (data.operasi?.lines?.length ?? 0) === 0 &&
    (data.investasi?.lines?.length ?? 0) === 0 &&
    (data.pendanaan?.lines?.length ?? 0) === 0;
  if (noMovements) {
    return <ReportEmptyCTA kind="arus-kas" />;
  }

  const netPositive = !data.net_change.startsWith("-");

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <SummaryPill label="Arus Operasi" value={data.operasi.net} />
        <SummaryPill label="Arus Investasi" value={data.investasi.net} />
        <SummaryPill label="Arus Pendanaan" value={data.pendanaan.net} />
        <SummaryPill
          label="Perubahan Kas Bersih"
          value={data.net_change}
          highlight={netPositive ? "success" : "danger"}
        />
      </div>

      <ArusSection title="Aktivitas Operasi" items={data.operasi?.lines ?? []} net={data.operasi?.net ?? "0"} />
      <ArusSection title="Aktivitas Investasi" items={data.investasi?.lines ?? []} net={data.investasi?.net ?? "0"} />
      <ArusSection title="Aktivitas Pendanaan" items={data.pendanaan?.lines ?? []} net={data.pendanaan?.net ?? "0"} />

      <Card>
        <div className={`flex justify-between items-center text-base font-bold
          ${netPositive ? "text-success" : "text-danger"}`}>
          <span>Perubahan Kas Bersih</span>
          <span className="tabular-nums"><Rupiah value={data.net_change} colorSign /></span>
        </div>
      </Card>
    </div>
  );
}

function ArusSection({ title, items, net }: {
  title: string;
  items: Array<{ description: string; amount: string }>;
  net: string;
}) {
  const netPos = !net.startsWith("-");
  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      {items.length === 0 ? (
        <EmptyState title={`Tidak ada transaksi ${title.toLowerCase()}`} />
      ) : (
        <div className="divide-y divide-border">
          {items.map((item, i) => {
            const pos = !item.amount.startsWith("-");
            return (
              <div key={i} className="flex justify-between items-center px-5 py-3 text-sm">
                <span className="text-text-primary">{item.description}</span>
                <span className={`tabular-nums font-medium ${pos ? "text-success" : "text-danger"}`}>
                  <Rupiah value={item.amount} colorSign />
                </span>
              </div>
            );
          })}
        </div>
      )}
      <div className={`flex justify-between items-center px-5 py-3 border-t border-border bg-border-subtle/30 font-semibold text-sm
        ${netPos ? "text-success" : "text-danger"}`}>
        <span>Arus Bersih</span>
        <span className="tabular-nums"><Rupiah value={net} colorSign /></span>
      </div>
    </Card>
  );
}

function SummaryPill({ label, value, highlight }: {
  label: string;
  value: string;
  highlight?: "success" | "danger";
}) {
  const pos = !value.startsWith("-");
  const colorClass = pos ? "text-success" : "text-danger";
  const highlightClass = highlight === "success"
    ? "border-success/30 bg-success-bg"
    : highlight === "danger"
    ? "border-danger/30 bg-danger-bg"
    : "";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 ${highlightClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-sm font-bold tabular-nums ${colorClass}`}>
        <Rupiah value={value} colorSign />
      </p>
    </div>
  );
}
