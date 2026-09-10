import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Rupiah } from "@/components/format/Rupiah";
import type { SaleRecord } from "@/lib/types/api";

interface Props {
  record: SaleRecord;
}

function fmt(d: string) {
  return new Date(d).toLocaleDateString("id-ID", {
    day: "2-digit", month: "long", year: "numeric",
  });
}

// Temuan #5: label basis HPP — bukan status baik/buruk, murni transparansi
// dari mana angka ini diturunkan (beda basis dari "Biaya Aktual Terakumulasi"
// di halaman biaya proyek, dan itu wajar — lihat CostBreakdownTable.tsx).
const HPP_METHOD_LABEL: Record<string, string> = {
  actual: "Aktual — biaya langsung terjurnal per unit",
  budgeted: "RAB Teranggarkan — alokasi dari rencana anggaran biaya",
  finalized: "Final — hasil true-up pasca-penyelesaian proyek",
  none: "Tidak berlaku (produk non-properti)",
};

export function SaleRecordCard({ record }: Props) {
  // S9/R-9: hpp_total & vat_amount dari backend — FE nol aritmetika bisnis.
  const hppTotal = record.hpp_total ?? "0";
  const hppMethodLabel = HPP_METHOD_LABEL[record.hpp_method] ?? record.hpp_method;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sale Record — Akad</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Tanggal Akad: {fmt(record.recognition_date)}
        </p>
      </CardHeader>

      <div className="space-y-4">
        {/* Pendapatan */}
        <section>
          <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">
            Pendapatan (Event 3)
          </p>
          <div className="space-y-1 text-sm">
            <Row label="Harga Jual (DPP)" value={record.sale_price} />
            {record.is_vat && (
              <Row
                label={`PPN (${(parseFloat(record.vat_rate) * 100).toFixed(0)}%)`}
                value={record.vat_amount ?? "0"}
              />
            )}
            <Row label="Uang Muka saat Akad" value={record.total_advance_at_bast} muted />
            <Row
              label="Buyer Ref"
              value={record.buyer_ref}
              isText
            />
          </div>
        </section>

        {/* HPP */}
        <section>
          <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">
            HPP (Event 4 — Invariant #4)
          </p>
          <p className="text-xs text-text-tertiary mb-2">
            Basis: {hppMethodLabel}
          </p>
          <div className="space-y-1 text-sm">
            <Row label="Tanah"      value={record.hpp_land}      muted />
            <Row label="Hard Cost"  value={record.hpp_hard}      muted />
            <Row label="Soft Cost (legacy)"           value={record.hpp_soft}      muted />
            <Row label="Pendanaan/Operasional (legacy)" value={record.hpp_financing} muted />
            <div className="border-t border-border pt-1 mt-1">
              <Row label="Total HPP" value={hppTotal} bold />
            </div>
          </div>
        </section>

        {/* Jurnal refs */}
        <section className="text-xs text-text-tertiary border-t border-border pt-3 space-y-0.5">
          <p>Jurnal Pendapatan: #{record.revenue_journal_id}</p>
          {record.cogs_journal_id && <p>Jurnal HPP: #{record.cogs_journal_id}</p>}
          <p>
            Serah Terima Fisik:{" "}
            {record.handed_over_at ? fmt(record.handed_over_at) : "Belum diserahkan"}
          </p>
        </section>
      </div>
    </Card>
  );
}

function Row({
  label, value, muted, bold, isText,
}: {
  label: string;
  value: string;
  muted?: boolean;
  bold?: boolean;
  isText?: boolean;
}) {
  return (
    <div className={`flex justify-between ${muted ? "text-text-secondary" : ""} ${bold ? "font-semibold" : ""}`}>
      <span>{label}</span>
      {isText ? (
        <span>{value}</span>
      ) : (
        <Rupiah value={value} colorSign={false} />
      )}
    </div>
  );
}
