import { Fragment } from "react";
import { RABvsRealisasiReport, ConstructionRealisasiTree } from "@/lib/types/api";
import { Rupiah } from "@/components/format/Rupiah";
import { Persen } from "@/components/format/Persen";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";

const CATEGORY_LABELS: Record<string, string> = {
  land:         "Tanah",
  construction: "Konstruksi",
  soft:         "Biaya Lunak",
  operational:  "Operasional",
  marketing:    "Pemasaran",
  other:        "Lain-lain",
};

interface Props {
  report: RABvsRealisasiReport | null;
  error: boolean;
  // Detail hierarki Konstruksi (Produksi Subsidi/Komersial, Sarana &
  // Prasarana, Perizinan → item RAB individual). Opsional: bila tidak
  // dikirim (atau gagal dimuat), baris Konstruksi tetap tampil sebagai
  // baris tunggal seperti kategori lain — tidak memblokir keseluruhan tabel.
  constructionTree?: ConstructionRealisasiTree | null;
}

export function RABvsRealisasiSection({ report, error, constructionTree }: Props) {
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
            const isNA = row.persen_realisasi === "N/A";
            // Backend mengirim fraksi mentah (0.6 = 60%), kontrak yang sama
            // dipakai komponen <Persen> (mengalikan 100 sendiri) — bukan lagi
            // string ber-suffix "%".
            const overBudget = !isNA && parseFloat(row.persen_realisasi) > 1;
            const rowEl = (
              <TableRow subtle={overBudget}>
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
                    {isNA ? "–" : <Persen value={row.persen_realisasi} />}
                  </span>
                </Td>
              </TableRow>
            );

            const showDetail = row.category === "construction" && constructionTree && constructionTree.groups.length > 0;
            return (
              <Fragment key={row.category}>
                {rowEl}
                {showDetail && <ConstructionDetailRows tree={constructionTree!} />}
              </Fragment>
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

// ConstructionDetailRows memecah baris "Konstruksi" menjadi hierarki penuh:
// Subkategori (Produksi Subsidi/Komersial, Sarana & Prasarana, Perizinan) →
// item RAB individual (nama dari deskripsi yang diinput user, bukan
// hardcode). Total subkategori = SUM item-itemnya dari backend
// (GetConstructionRealisasiTree) — bukan rata-rata persentase — dan
// persen_realisasi di sini SUDAH string berformat "27.78%", tampilkan apa
// adanya (bukan lewat <Persen>, yang mengharapkan fraksi mentah).
function ConstructionDetailRows({ tree }: { tree: ConstructionRealisasiTree }) {
  return (
    <>
      {tree.groups.map(group => (
        <Fragment key={group.subcategory}>
          <TableRow className="bg-border-subtle/40">
            <Td>
              <span className="pl-4 text-sm font-semibold text-text-primary">{group.label}</span>
            </Td>
            <Td right><span className="font-semibold"><Rupiah value={group.budgeted} /></span></Td>
            <Td right><span className="font-semibold"><Rupiah value={group.realisasi} /></span></Td>
            <Td right><Rupiah value={group.selisih} colorSign /></Td>
            <Td right><span className="font-semibold">{group.persen_realisasi}</span></Td>
          </TableRow>
          {group.items.map(item => (
            <TableRow key={item.item_id}>
              <Td>
                <span className="pl-9 text-sm text-text-secondary">{item.description || "(tanpa nama)"}</span>
              </Td>
              <Td right><Rupiah value={item.budgeted} /></Td>
              <Td right><Rupiah value={item.realisasi} /></Td>
              <Td right><span className="text-text-secondary"><Rupiah value={item.selisih} colorSign /></span></Td>
              <Td right><span className="text-text-secondary">{item.persen_realisasi}</span></Td>
            </TableRow>
          ))}
        </Fragment>
      ))}
    </>
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
