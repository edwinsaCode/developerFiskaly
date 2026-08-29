import type { AllocationResult, AllocationTotals, Unit } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";

interface Props {
  results: AllocationResult[];
  // S9/R-9: roll-up + share % dihitung backend — FE display saja.
  totals: AllocationTotals;
  unitMap: Record<number, Unit>;
}

export function AllocationResultTable({ results, totals, unitMap }: Props) {

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <CardTitle>Hasil Alokasi HPP per Unit</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Langsung = biaya langsung unit. Dialokasi = porsi biaya proyek. Total HPP = Langsung + Dialokasi.
        </p>
      </CardHeader>
      <Table>
        <TableHead>
          <TableRow>
            <Th>Unit</Th>
            <Th right>HPP Langsung</Th>
            <Th right>HPP Dialokasi</Th>
            <Th right>Total HPP</Th>
            <Th right>%</Th>
          </TableRow>
        </TableHead>
        <TableBody>
          {results.map((r) => {
            const unit = unitMap[r.unit_id];
            const pct = r.share_pct ?? "0";
            return (
              <TableRow key={r.unit_id}>
                <Td>
                  <span className="font-medium">{unit?.code ?? `#${r.unit_id}`}</span>
                  {unit && (
                    <span className="ml-1.5 text-xs text-text-tertiary">{unit.unit_type}</span>
                  )}
                </Td>
                <Td right>
                  <Rupiah value={r.direct.total} colorSign={false} />
                </Td>
                <Td right>
                  <Rupiah value={r.allocated.total} colorSign={false} />
                </Td>
                <Td right>
                  <span className="font-semibold">
                    <Rupiah value={r.total.total} colorSign={false} />
                  </span>
                </Td>
                <Td right>
                  <span className="tabular-nums text-text-secondary">{pct}%</span>
                </Td>
              </TableRow>
            );
          })}
          {results.length > 1 && (
            <TableRow>
              <Td>
                <span className="font-semibold text-text-primary">Total Proyek</span>
              </Td>
              <Td right>
                <span className="font-semibold">
                  <Rupiah value={totals.direct} colorSign={false} />
                </span>
              </Td>
              <Td right>
                <span className="font-semibold">
                  <Rupiah value={totals.allocated} colorSign={false} />
                </span>
              </Td>
              <Td right>
                <span className="font-semibold">
                  <Rupiah value={totals.total} colorSign={false} />
                </span>
              </Td>
              <Td right>
                <span className="font-semibold text-text-primary">100%</span>
              </Td>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </Card>
  );
}
