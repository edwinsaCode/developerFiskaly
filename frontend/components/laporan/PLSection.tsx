import type { PLReport, PLLine } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { ReportEmptyCTA } from "@/components/laporan/ReportEmptyCTA";

interface Props {
  data: PLReport | null;
  error: boolean;
  title?: string;
}

export function PLSection({ data, error, title = "Laba Rugi" }: Props) {
  if (error) {
    return <Card><p className="text-sm text-danger">Gagal memuat {title}. Coba lagi nanti.</p></Card>;
  }
  if (!data) {
    return <ReportEmptyCTA kind="laba-rugi" />;
  }
  const hasAnyLine =
    (data.pendapatan?.length ?? 0) > 0 ||
    (data.hpp?.length ?? 0) > 0 ||
    (data.beban_operasional?.length ?? 0) > 0 ||
    (data.pendapatan_luar_usaha?.length ?? 0) > 0 ||
    (data.beban_luar_usaha?.length ?? 0) > 0 ||
    (data.beban_pajak?.length ?? 0) > 0;
  if (!hasAnyLine) {
    return <ReportEmptyCTA kind="laba-rugi" />;
  }

  const labaPositive = !data.laba_rugi_bersih.startsWith("-");

  return (
    <div className="space-y-6">
      {/* Ringkasan */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <SummaryPill label="Total Pendapatan" value={data.total_pendapatan} />
        <SummaryPill label="Laba Kotor" value={data.laba_kotor} signed />
        <SummaryPill label="Laba Operasional" value={data.laba_operasional} signed />
        <SummaryPill
          label="Laba/Rugi Bersih"
          value={data.laba_rugi_bersih}
          highlight={labaPositive ? "success" : "danger"}
          signed
        />
      </div>

      {/* Pendapatan */}
      <PLTable title="Pendapatan" lines={data.pendapatan ?? []} total={data.total_pendapatan} totalLabel="Total Pendapatan" />

      {/* HPP */}
      <PLTable title="HPP (Harga Pokok Penjualan)" lines={data.hpp ?? []} total={data.total_hpp} totalLabel="Total HPP" />

      {/* Beban Operasional */}
      <PLTable title="Beban Operasional" lines={data.beban_operasional ?? []} total={data.total_beban_operasional} totalLabel="Total Beban Operasional" />

      {/* Pendapatan & Beban Luar Usaha */}
      <PLTable title="Pendapatan Luar Usaha" lines={data.pendapatan_luar_usaha ?? []} total={data.total_pendapatan_luar_usaha} totalLabel="Total Pendapatan Luar Usaha" />
      <PLTable title="Beban Luar Usaha" lines={data.beban_luar_usaha ?? []} total={data.total_beban_luar_usaha} totalLabel="Total Beban Luar Usaha" />

      {/* Beban Pajak */}
      <PLTable title="Beban Pajak" lines={data.beban_pajak ?? []} total={data.total_beban_pajak} totalLabel="Total Beban Pajak" />

      {/* Hasil */}
      <Card>
        <div className="space-y-2">
          <PLSummaryRow label="Total Pendapatan" value={data.total_pendapatan} />
          <PLSummaryRow label="Total HPP" value={data.total_hpp} />
          <div className="border-t border-border pt-2">
            <PLSummaryRow label="Laba Kotor" value={data.laba_kotor} bold />
          </div>
          <PLSummaryRow label="Total Beban Operasional" value={data.total_beban_operasional} />
          <div className="border-t border-border pt-2">
            <PLSummaryRow label="Laba Operasional" value={data.laba_operasional} bold />
          </div>
          <PLSummaryRow label="Pendapatan Luar Usaha" value={data.total_pendapatan_luar_usaha} />
          <PLSummaryRow label="Beban Luar Usaha" value={data.total_beban_luar_usaha} />
          <div className="border-t border-border pt-2">
            <PLSummaryRow label="Laba Bersih Sebelum Pajak" value={data.laba_bersih_sebelum_pajak} bold />
          </div>
          <PLSummaryRow label="Beban Pajak" value={data.total_beban_pajak} />
          <div className="border-t border-border pt-2">
            <PLSummaryRow label="Laba/Rugi Bersih Setelah Pajak" value={data.laba_rugi_bersih} bold highlight={labaPositive ? "success" : "danger"} />
          </div>
        </div>
      </Card>
    </div>
  );
}

function PLTable({ title, lines, total, totalLabel }: {
  title: string;
  lines: PLLine[];
  total: string;
  totalLabel: string;
}) {
  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      {lines.length === 0 ? (
        <EmptyState title={`Tidak ada ${title.toLowerCase()}`} />
      ) : (
        <Table>
          <TableHead>
            <TableRow><Th>Kode</Th><Th>Nama Akun</Th><Th right>Jumlah</Th></TableRow>
          </TableHead>
          <TableBody>
            {lines.map(line => (
              <TableRow key={line.code}>
                <Td><span className="font-mono text-xs text-text-tertiary">{line.code}</span></Td>
                <Td>{line.name}</Td>
                <Td right><Rupiah value={line.amount} colorSign={false} /></Td>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-border-subtle/30">
        <span className="text-sm font-semibold">{totalLabel}</span>
        <span className="font-bold tabular-nums text-sm"><Rupiah value={total} colorSign={false} /></span>
      </div>
    </Card>
  );
}

function PLSummaryRow({ label, value, bold, highlight }: {
  label: string;
  value: string;
  bold?: boolean;
  highlight?: "success" | "danger";
}) {
  const colorClass = highlight === "success" ? "text-success" : highlight === "danger" ? "text-danger" : "";
  return (
    <div className={`flex justify-between items-center text-sm ${bold ? "font-semibold" : ""}`}>
      <span className="text-text-secondary">{label}</span>
      <span className={`tabular-nums ${colorClass}`}><Rupiah value={value} colorSign={false} /></span>
    </div>
  );
}

function SummaryPill({ label, value, signed, highlight }: {
  label: string;
  value: string;
  signed?: boolean;
  highlight?: "success" | "danger";
}) {
  const borderClass = highlight === "success"
    ? "border-success/30 bg-success-bg"
    : highlight === "danger"
    ? "border-danger/30 bg-danger-bg"
    : "";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 ${borderClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-sm font-bold tabular-nums ${highlight === "success" ? "text-success" : highlight === "danger" ? "text-danger" : ""}`}>
        <Rupiah value={value} colorSign={signed} />
      </p>
    </div>
  );
}
