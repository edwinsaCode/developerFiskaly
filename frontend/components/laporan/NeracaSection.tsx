import type { NeracaLine, NeracaReport } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { ReportEmptyCTA } from "@/components/laporan/ReportEmptyCTA";

interface Props {
  data: NeracaReport | null;
  error: boolean;
}

function fmtID(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString("id-ID", { day: "numeric", month: "long", year: "numeric" });
}

export function NeracaSection({ data, error }: Props) {
  if (error) {
    return (
      <Card>
        <p className="text-sm text-danger">Gagal memuat Neraca. Coba lagi nanti.</p>
      </Card>
    );
  }
  if (!data) {
    return <ReportEmptyCTA kind="neraca" />;
  }
  const isEmpty =
    (data.aset?.length ?? 0) === 0 &&
    (data.kewajiban?.length ?? 0) === 0 &&
    (data.ekuitas?.length ?? 0) === 0 &&
    data.laba_rugi_tahun_berjalan === "0";
  if (isEmpty) {
    return <ReportEmptyCTA kind="neraca" />;
  }

  return (
    <div className="space-y-6">
      {/* Seimbang banner */}
      <div className={`rounded-lg px-4 py-3 flex items-center gap-3 text-sm font-medium
        ${data.is_balanced
          ? "bg-success-bg border border-success/30 text-success"
          : "bg-danger-bg border border-danger/30 text-danger"}`}>
        <span className="text-lg">{data.is_balanced ? "✓" : "✗"}</span>
        {data.is_balanced
          ? `Neraca seimbang per ${fmtID(data.as_of)}`
          : "PERINGATAN: Neraca tidak seimbang — ada jurnal yang belum balance"}
      </div>

      {data.from && (
        <div className="rounded-lg px-4 py-3 bg-surface border border-border text-sm -mt-2">
          <p className="text-text-secondary">
            Laba/Rugi Periode Terpilih ({fmtID(data.from)} s/d {fmtID(data.as_of)}):{" "}
            <span className="font-semibold text-text-primary">
              <Rupiah value={data.laba_rugi_periode_terpilih ?? "0"} colorSign={false} />
            </span>
          </p>
          <p className="text-xs text-text-tertiary mt-1">
            Angka ini murni informasi — Neraca (Aset/Kewajiban/Ekuitas dan Laba/Rugi Tahun Berjalan di bawah) selalu snapshot kumulatif s/d {fmtID(data.as_of)} dan tidak dipengaruhi Start Date.
          </p>
        </div>
      )}

      {/* Ringkasan totals */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <TotalPill label="Total Aset" value={data.total_aset} />
        <TotalPill label="Total Kewajiban" value={data.total_kewajiban} />
        <TotalPill label="Total Ekuitas (termasuk laba berjalan)" value={data.total_ekuitas_efektif ?? data.total_ekuitas} />
      </div>

      {/* Aset */}
      <NeracaTable title="Aset" lines={data.aset ?? []} total={data.total_aset} totalLabel="Total Aset" />

      {/* Kewajiban */}
      <NeracaTable title="Kewajiban" lines={data.kewajiban ?? []} total={data.total_kewajiban} totalLabel="Total Kewajiban" />

      {/* Ekuitas */}
      <Card padding="none">
        <CardHeader className="px-5 pt-5">
          <CardTitle>Ekuitas</CardTitle>
        </CardHeader>
        <Table>
          <TableHead>
            <TableRow>
              <Th>Kode</Th><Th>Nama Akun</Th><Th right>Saldo</Th>
            </TableRow>
          </TableHead>
          <TableBody>
            {(data.ekuitas ?? []).map(line => (
              <AccountRow key={line.code} line={line} />
            ))}
            <TableRow>
              <Td></Td>
              <Td><span className="text-xs text-text-secondary italic">Laba/Rugi Tahun Berjalan</span></Td>
              <Td right><Rupiah value={data.laba_rugi_tahun_berjalan} colorSign={false} /></Td>
            </TableRow>
          </TableBody>
        </Table>
        <TotalRow label="Total Ekuitas" value={data.total_ekuitas_efektif ?? data.total_ekuitas} />
      </Card>

      {/* Persamaan */}
      <Card>
        <div className="flex items-center justify-between text-sm">
          <span className="text-text-secondary">Total Kewajiban + Ekuitas</span>
          <span className="font-bold text-base tabular-nums">
            <Rupiah value={data.total_kewajiban_ekuitas} colorSign={false} />
          </span>
        </div>
      </Card>
    </div>
  );
}

function NeracaTable({ title, lines, total, totalLabel }: {
  title: string;
  lines: NeracaLine[];
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
            <TableRow>
              <Th>Kode</Th><Th>Nama Akun</Th><Th right>Saldo</Th>
            </TableRow>
          </TableHead>
          <TableBody>
            {lines.map(line => <AccountRow key={line.code} line={line} />)}
          </TableBody>
        </Table>
      )}
      <TotalRow label={totalLabel} value={total} />
    </Card>
  );
}

function AccountRow({ line }: { line: NeracaLine }) {
  return (
    <TableRow>
      <Td><span className="font-mono text-xs text-text-tertiary">{line.code}</span></Td>
      <Td>{line.name}</Td>
      <Td right><Rupiah value={line.amount} colorSign={false} /></Td>
    </TableRow>
  );
}

function TotalRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-border-subtle/30">
      <span className="text-sm font-semibold text-text-primary">{label}</span>
      <span className="font-bold text-sm tabular-nums">
        <Rupiah value={value} colorSign={false} />
      </span>
    </div>
  );
}

function TotalPill({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface border border-border rounded-lg px-4 py-3">
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className="text-base font-bold tabular-nums text-text-primary">
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}
