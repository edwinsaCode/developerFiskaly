import type { TaxLiabilityReport } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { StatusBadge } from "@/components/ui/Badge";

interface Props {
  data: TaxLiabilityReport | null;
  error: boolean;
  bastWarningCount: number;
}

export function TaxSection({ data, error, bastWarningCount }: Props) {
  if (error) {
    return <Card><p className="text-sm text-danger">Gagal memuat Kewajiban Pajak. Coba lagi nanti.</p></Card>;
  }
  if (!data) {
    return <Card><EmptyState title="Belum ada data Pajak" /></Card>;
  }

  const hasOutstanding = data.total_outstanding !== "0" && data.total_outstanding !== "0.0000";

  return (
    <div className="space-y-6">
      {/* Warning BAST tanpa PPh Final */}
      {bastWarningCount > 0 && (
        <div className="bg-warning-bg border border-warning/30 rounded-lg px-4 py-3 flex items-start gap-3">
          <span className="text-warning text-lg flex-shrink-0">⚠</span>
          <div>
            <p className="text-sm font-medium text-warning">
              {bastWarningCount} unit telah BAST tetapi belum ada akrual PPh Final
            </p>
            <p className="text-xs text-text-secondary mt-1">
              Pastikan PPh Final sudah diakrualkan untuk unit-unit tersebut.
            </p>
          </div>
        </div>
      )}

      {/* Ringkasan */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <TotalPill label="Total Kewajiban" value={data.total_obligation} />
        <TotalPill label="Sudah Dibayar" value={data.total_paid} highlight="success" />
        <TotalPill label="Belum Dibayar" value={data.total_outstanding} highlight={hasOutstanding ? "danger" : undefined} />
      </div>

      {/* Tabel detail */}
      <Card padding="none">
        <CardHeader className="px-5 pt-5">
          <CardTitle>Detail Kewajiban Pajak</CardTitle>
        </CardHeader>
        {(data.items ?? []).length === 0 ? (
          <EmptyState title="Tidak ada kewajiban pajak dalam periode ini" />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <Th>Tanggal Akrual</Th>
                <Th>Kode Tarif</Th>
                <Th>Unit</Th>
                <Th right>Nilai Transfer</Th>
                <Th right>Tarif</Th>
                <Th right>Jumlah Pajak</Th>
                <Th>Status</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {(data.items ?? []).map(item => (
                <TableRow key={item.id}>
                  <Td><Tanggal value={item.accrual_date} /></Td>
                  <Td><span className="font-mono text-xs">{item.rate_code}</span></Td>
                  <Td>{item.unit_id ? `Unit #${item.unit_id}` : "—"}</Td>
                  <Td right><Rupiah value={item.transfer_value} colorSign={false} /></Td>
                  <Td right><span className="text-sm">{item.rate}%</span></Td>
                  <Td right><Rupiah value={item.tax_amount} colorSign={false} /></Td>
                  <Td>
                    <StatusBadge status={item.status} />
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-border-subtle/30">
          <span className="text-sm font-semibold">Total Kewajiban</span>
          <span className="font-bold tabular-nums text-sm">
            <Rupiah value={data.total_obligation} colorSign={false} />
          </span>
        </div>
      </Card>
    </div>
  );
}

function TotalPill({ label, value, highlight }: {
  label: string;
  value: string;
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
      <p className={`text-sm font-bold tabular-nums ${
        highlight === "success" ? "text-success" : highlight === "danger" ? "text-danger" : "text-text-primary"
      }`}>
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}
