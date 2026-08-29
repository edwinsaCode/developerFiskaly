import { RABvsRealisasiReport } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Persen } from "@/components/format/Persen";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";

const CATEGORY_LABELS: Record<string, string> = {
  land:         "Tanah",
  construction: "Konstruksi",
  soft:         "Biaya Lunak",
  financing:    "Pendanaan",
  marketing:    "Pemasaran",
  other:        "Lain-lain",
};

interface Props {
  report: RABvsRealisasiReport | null;
  error: boolean;
}

export function RABvsRealisasiSection({ report, error }: Props) {
  if (error) {
    return (
      <Card>
        <p className="text-sm text-danger">
          Gagal memuat RAB vs Realisasi. Pastikan sudah ada plan yang aktif.
        </p>
      </Card>
    );
  }

  if (!report) {
    return (
      <Card>
        <EmptyState
          title="Belum ada plan aktif"
          description="Approve sebuah RAB untuk melihat perbandingan anggaran vs realisasi."
        />
      </Card>
    );
  }

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>RAB vs Realisasi</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          {report.plan_label} (v{report.plan_version}) — angka realisasi dari jurnal yang sudah diposting
        </p>
      </CardHeader>

      <Table>
        <TableHead>
          <TableRow>
            <Th>Kategori</Th>
            <Th right>Anggaran</Th>
            <Th right>Realisasi</Th>
            <Th right>Selisih</Th>
            <Th right>% Terpakai</Th>
          </TableRow>
        </TableHead>
        <TableBody>
          {report.rows.map(row => {
            const overBudget = parseFloat(row.persen_realisasi) > 100;
            return (
              <TableRow key={row.category} subtle={overBudget}>
                <Td>
                  <span className={overBudget ? "font-semibold text-danger" : ""}>
                    {CATEGORY_LABELS[row.category] ?? row.category}
                  </span>
                </Td>
                <Td right><Rupiah value={row.budgeted} /></Td>
                <Td right>
                  <span className={overBudget ? "text-danger font-semibold" : ""}>
                    <Rupiah value={row.realisasi} />
                  </span>
                </Td>
                <Td right>
                  <span className={overBudget ? "text-danger" : "text-text-secondary"}>
                    <Rupiah value={row.selisih} colorSign />
                  </span>
                </Td>
                <Td right>
                  <span className={overBudget ? "text-danger font-semibold" : ""}>
                    <Persen value={row.persen_realisasi} />
                  </span>
                </Td>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>

      {/* Totals */}
      <div className="px-5 py-3 border-t border-border bg-border-subtle/30 grid grid-cols-1 sm:grid-cols-3 gap-4">
        <TotalCell label="Total Anggaran" value={report.total_budgeted} />
        <TotalCell label="Total Realisasi" value={report.total_realisasi} />
        <TotalCell label="Selisih" value={report.total_selisih} signed />
      </div>
    </Card>
  );
}

function TotalCell({ label, value, signed }: { label: string; value: string; signed?: boolean }) {
  return (
    <div>
      <p className="text-xs text-text-secondary uppercase tracking-wide">{label}</p>
      <p className="text-base font-bold tabular num-right mt-0.5">
        <Rupiah value={value} colorSign={signed} />
      </p>
    </div>
  );
}
