"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Badge } from "@/components/ui/Badge";
import { EmptyState } from "@/components/ui/EmptyState";
import type { ARAgingReport, ARAgingBucket, ReceivableSource } from "@/lib/types/api";

// W-4: nilai filter layar. "all" bukan sumber — ia ketiadaan filter, dan
// sengaja tidak dikirim ke server sebagai nilai (server fail-closed pada
// sumber yang tidak dikenal).
type SourceFilter = "all" | ReceivableSource;

interface Props {
  report: ARAgingReport;
  asOf: string;
  source: SourceFilter;
}

const bucketLabel: Record<ARAgingBucket, string> = {
  current: "Belum Jatuh Tempo",
  "1_30": "1–30 hari",
  "31_60": "31–60 hari",
  "61_90": "61–90 hari",
  "90_plus": "> 90 hari",
};

const bucketVariant: Record<ARAgingBucket, "accent" | "warning" | "danger"> = {
  current: "accent",
  "1_30": "warning",
  "31_60": "warning",
  "61_90": "danger",
  "90_plus": "danger",
};

const sourceLabel: Record<ReceivableSource, string> = {
  house: "Harga Rumah",
  realization: "Biaya Realisasi",
  legacy: "Proyek Lama",
};

const filterTabs: { value: SourceFilter; label: string }[] = [
  { value: "all", label: "Semua" },
  // Kualifikasi "(pasca-BAST)" hanya di tab pemilih ruang lingkup, tempat orang
  // memutuskan apa yang ia lihat. Badge per baris tetap pendek — definisinya
  // sudah dinyatakan sekali di catatan kaki laporan.
  { value: "house", label: "Harga Rumah (pasca-BAST)" },
  { value: "realization", label: "Biaya Realisasi" },
  { value: "legacy", label: "Proyek Lama" },
];

const sourceBadge: Record<ReceivableSource, "warning" | "neutral" | "accent"> = {
  house: "neutral",
  realization: "warning",
  legacy: "accent",
};

export function ARAgingView({ report, asOf, source }: Props) {
  const router = useRouter();

  // Satu pembangun URL untuk semua kontrol: mengubah tanggal tidak boleh
  // diam-diam menghapus filter sumber, dan sebaliknya.
  function go(next: { asOf?: string; source?: SourceFilter }) {
    const params = new URLSearchParams();
    params.set("as_of", next.asOf ?? asOf);
    const src = next.source ?? source;
    if (src !== "all") params.set("source", src);
    router.push(`/accounting/receivable?${params}`);
  }

  const hasRows = report.rows.length > 0;
  const rateNum = parseFloat(report.collection_rate);
  const rateColor =
    rateNum >= 80 ? "text-success" : rateNum >= 50 ? "text-warning" : "text-danger";

  // Subtotal per sumber datang dari server (by_source). Frontend tidak
  // menjumlahkan baris sendiri — total di layar ini dan total di statement
  // harus lahir dari perhitungan yang sama.
  const bySource = report.by_source ?? [];

  return (
    <div className="space-y-6">
      {/* Filter tanggal + sumber */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-2">
          <label className="text-sm text-text-secondary">Per tanggal</label>
          <input
            type="date"
            defaultValue={asOf}
            onChange={(e) => e.target.value && go({ asOf: e.target.value })}
            className="text-sm border border-border rounded-md px-2.5 py-1.5 bg-surface text-text-primary
              focus:outline-none focus:ring-2 focus:ring-accent/30"
          />
        </div>
        <div className="flex items-center gap-2">
          <label className="text-sm text-text-secondary">Sumber</label>
          <div className="inline-flex rounded-md border border-border overflow-hidden">
            {filterTabs.map((tab) => (
              <button
                key={tab.value}
                type="button"
                onClick={() => go({ source: tab.value })}
                aria-pressed={source === tab.value}
                className={`px-3 py-1.5 text-sm transition-colors ${
                  source === tab.value
                    ? "bg-accent text-white"
                    : "bg-surface text-text-secondary hover:bg-border-subtle/50"
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Kartu ringkasan */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <SummaryCard label="Total Piutang" value={report.total_piutang} />
        <SummaryCard label="Belum Jatuh Tempo" value={report.current_due} tone="accent" />
        <SummaryCard
          label="Jatuh Tempo (Overdue)"
          value={report.overdue}
          tone={report.overdue !== "0" ? "danger" : undefined}
        />
        <Card>
          <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">Collection Rate</p>
          <p className={`text-lg font-bold tabular-nums ${rateColor}`}>{report.collection_rate}%</p>
          <p className="text-[11px] text-text-tertiary mt-0.5">
            <Rupiah value={report.total_collected} colorSign={false} /> /{" "}
            <Rupiah value={report.total_scheduled} colorSign={false} /> tertagih
          </p>
        </Card>
      </div>

      {/* Rincian per sumber — hanya bermakna saat keduanya ditampilkan */}
      {source === "all" && bySource.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {bySource.map((s) => (
            <div key={s.source} className="bg-surface border border-border rounded-lg px-4 py-3">
              <div className="flex items-baseline justify-between gap-3">
                <p className="text-xs text-text-secondary uppercase tracking-wide">
                  {sourceLabel[s.source] ?? s.source}{" "}
                  <span className="text-text-tertiary normal-case">({s.count} tagihan)</span>
                </p>
                <button
                  type="button"
                  onClick={() => go({ source: s.source })}
                  className="text-xs text-accent hover:underline whitespace-nowrap"
                >
                  Lihat saja ini →
                </button>
              </div>
              <p className="text-lg font-bold tabular-nums text-text-primary mt-1">
                <Rupiah value={s.outstanding} colorSign={false} />
              </p>
              <p className="text-[11px] text-text-tertiary mt-0.5">
                Menunggak <Rupiah value={s.overdue} colorSign={false} />
              </p>
            </div>
          ))}
        </div>
      )}

      {/* Rincian per bucket */}
      {hasRows && (
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-3">
          <BucketPill label="Belum JT" summary={report.buckets.current} tone="accent" />
          <BucketPill label="1–30 hari" summary={report.buckets.b1_30} tone="warning" />
          <BucketPill label="31–60 hari" summary={report.buckets.b31_60} tone="warning" />
          <BucketPill label="61–90 hari" summary={report.buckets.b61_90} tone="danger" />
          <BucketPill label="> 90 hari" summary={report.buckets.b90_plus} tone="danger" />
        </div>
      )}

      {/* Tabel piutang */}
      <Card padding="none">
        {!hasRows ? (
          <AgingEmptyState report={report} asOf={asOf} source={source} onClearFilter={() => go({ source: "all" })} />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <Th>Sumber</Th>
                <Th>Buyer</Th>
                <Th>Unit</Th>
                <Th>Tagihan</Th>
                <Th>Jatuh Tempo</Th>
                <Th right>Nominal</Th>
                <Th right>Dibayar</Th>
                <Th right>Outstanding</Th>
                <Th>Umur</Th>
                <Th></Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {report.rows.map((row, i) => (
                <TableRow key={`${row.source}-${row.ref_id ?? 0}-${row.contract_id}-${i}`}>
                  <Td>
                    <Badge variant={sourceBadge[row.source] ?? "neutral"}>
                      {sourceLabel[row.source] ?? row.source}
                    </Badge>
                  </Td>
                  <Td>{row.buyer_name}</Td>
                  {/* Piutang proyek lama memang tidak punya unit. Strip lebih
                      jujur daripada sel kosong yang terbaca sebagai data hilang. */}
                  <Td mono>{row.unit_code || "—"}</Td>
                  <Td>
                    <span className="text-sm">{row.label || "—"}</span>
                    {row.invoice_number && row.invoice_number !== "—" && (
                      <span className="block text-[11px] text-text-tertiary font-mono">
                        {row.invoice_number}
                      </span>
                    )}
                  </Td>
                  <Td>
                    <Tanggal value={row.due_date} />
                    {row.days_overdue > 0 && (
                      <span className="text-danger text-xs ml-1">(+{row.days_overdue}h)</span>
                    )}
                  </Td>
                  <Td right><Rupiah value={row.amount} colorSign={false} /></Td>
                  <Td right><Rupiah value={row.paid} colorSign={false} /></Td>
                  <Td right><Rupiah value={row.outstanding} colorSign={false} /></Td>
                  <Td>
                    <Badge variant={bucketVariant[row.bucket]}>{bucketLabel[row.bucket]}</Badge>
                  </Td>
                  <Td>
                    {/* Tagihan realisasi ditagih & dialokasikan di layar tagihan
                        unit; cicilan harga rumah di rekening koran kontrak;
                        piutang proyek lama di sub-ledger legacy (ref_id — ia
                        tidak punya kontrak untuk ditautkan). */}
                    {row.source === "legacy" ? (
                      <Link
                        href={`/accounting/legacy-ar/${row.ref_id ?? 0}`}
                        className="text-xs text-accent hover:underline whitespace-nowrap"
                      >
                        Rincian →
                      </Link>
                    ) : row.source === "realization" ? (
                      <Link
                        href={`/penjualan/${row.unit_id}/tagihan`}
                        className="text-xs text-accent hover:underline whitespace-nowrap"
                      >
                        Tagihan →
                      </Link>
                    ) : (
                      <Link
                        href={`/accounting/receivable/${row.contract_id}`}
                        className="text-xs text-accent hover:underline whitespace-nowrap"
                      >
                        Statement →
                      </Link>
                    )}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {hasRows && (
          <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-border-subtle/30">
            <span className="text-sm font-semibold">
              Total Outstanding
              {source !== "all" && (
                <span className="font-normal text-text-secondary"> — {sourceLabel[source]} saja</span>
              )}
            </span>
            <span className="font-bold tabular-nums text-sm">
              <Rupiah value={report.total_piutang} colorSign={false} />
            </span>
          </div>
        )}
        {/* W-5: laporan ini hanya memuat tagihan yang sudah BERDOKUMEN. Tanpa
            catatan ini, admin membaca selisih terhadap layar tagihan unit
            sebagai baris yang hilang — padahal itu memang belum piutang. */}
        {/* BD-1 (W-8): batas laporan ini harus terbaca DI laporan. Tanpa
            kalimat ini, hilangnya cicilan pra-BAST terbaca sebagai data hilang. */}
        {(source === "all" || source === "house") && (
          <p className="px-5 py-2 text-[11px] text-text-tertiary border-t border-border">
            Baris harga rumah hanya memuat unit yang <strong>sudah diserahterimakan (BAST)</strong> —
            piutang harga rumah baru diakui pada saat itu. Cicilan pada kontrak yang belum BAST tetap
            wajib ditagih dan ada di{" "}
            <Link href="/accounting/billing-schedule" className="text-accent hover:underline">
              Jadwal Penagihan
            </Link>
            .
          </p>
        )}
        {(source === "all" || source === "realization") && (
          <p className="px-5 py-2 text-[11px] text-text-tertiary border-t border-border">
            Baris biaya realisasi hanya memuat tagihan yang <strong>sudah diterbitkan invoice-nya</strong>.
            Tagihan yang belum ditagihkan masih berstatus titipan — belum menjadi piutang, dan
            terbaca di layar tagihan unit sebagai &ldquo;Belum ditagihkan&rdquo;.
          </p>
        )}
      </Card>
    </div>
  );
}

// AgingEmptyState membedakan tiga kekosongan yang berbeda maknanya. "Tidak ada
// baris" karena belum ada kontrak, karena semuanya sudah lunas, dan karena
// filter menyembunyikannya adalah tiga keadaan berbeda — satu kalimat generik
// untuk ketiganya membuat admin menyimpulkan hal yang salah.
function AgingEmptyState({
  report,
  asOf,
  source,
  onClearFilter,
}: {
  report: ARAgingReport;
  asOf: string;
  source: SourceFilter;
  onClearFilter: () => void;
}) {
  const checkIcon = (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
      strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 13l4 4L19 7" />
    </svg>
  );

  if (source === "realization") {
    return (
      <EmptyState
        icon={checkIcon}
        title="Tidak ada tagihan biaya realisasi outstanding"
        description={`Per ${asOf} tidak ada biaya realisasi (notaris, PDAM, listrik, BPHTB) yang sudah ditagihkan lewat invoice dan masih bersisa. Tagihan yang dibuat tetapi belum diterbitkan invoice-nya belum menjadi piutang — periksa di layar tagihan unit.`}
        action={
          <button
            type="button"
            onClick={onClearFilter}
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
          >
            Lihat semua piutang →
          </button>
        }
      />
    );
  }

  if (source === "legacy") {
    return (
      <EmptyState
        icon={checkIcon}
        title="Tidak ada piutang proyek lama outstanding"
        description={`Per ${asOf} tidak ada sisa tagihan dari proyek yang berjalan sebelum sistem ini dipakai. Kalau rinciannya memang belum pernah diimpor, saldonya mungkin masih berupa satu angka gelondongan di Saldo Awal.`}
        action={
          <Link
            href="/accounting/legacy-ar"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
          >
            Ke Piutang Proyek Lama →
          </Link>
        }
      />
    );
  }

  // BD-1: piutang harga rumah lahir SAAT BAST. Kalimat lama di sini ("seluruh
  // jadwal pembayaran sudah diterima") keliru — jadwal sebelum serah terima
  // memang tidak pernah masuk daftar ini, sehingga kosong di layar ini tidak
  // berarti tidak ada yang perlu ditagih.
  if (source === "house") {
    return (
      <EmptyState
        icon={checkIcon}
        title="Tidak ada piutang harga rumah outstanding"
        description={`Per ${asOf} tidak ada sisa harga rumah pada unit yang sudah diserahterimakan (BAST). Cicilan pada kontrak yang BELUM BAST bukan piutang — ia ada di Jadwal Penagihan.`}
        action={
          <div className="flex flex-wrap items-center justify-center gap-2">
            <Link
              href="/accounting/billing-schedule"
              className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
            >
              Ke Jadwal Penagihan →
            </Link>
            <button
              type="button"
              onClick={onClearFilter}
              className="inline-flex items-center gap-2 px-4 py-2 rounded-md border border-border text-sm font-medium text-text-secondary hover:bg-border-subtle/50 transition-colors"
            >
              Lihat semua piutang
            </button>
          </div>
        }
      />
    );
  }

  if (report.total_scheduled === "0") {
    return (
      <EmptyState
        icon={
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
            strokeLinecap="round" strokeLinejoin="round">
            <path d="M17 9V7a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2m2 4h10a2 2 0 002-2v-6a2 2 0 00-2-2H9a2 2 0 00-2 2v6a2 2 0 002 2z" />
          </svg>
        }
        title="Belum ada piutang"
        description="Piutang harga rumah diakui saat serah terima (BAST); piutang biaya realisasi saat invoice terbit. Sebelum itu, kewajiban pembeli tercatat sebagai Jadwal Penagihan. Buat kontrak dan jadwal cicilan di Unit & Penjualan agar tagihannya terlacak."
        action={
          <Link
            href="/penjualan"
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
          >
            Ke Unit & Penjualan →
          </Link>
        }
      />
    );
  }

  return (
    <EmptyState
      icon={checkIcon}
      title="Tidak ada piutang outstanding 🎉"
      description={`Per ${asOf} tidak ada piutang tersisa — harga rumah (unit yang sudah BAST), biaya realisasi, maupun piutang proyek lama. Collection rate ${report.collection_rate}%. Cicilan pada kontrak yang belum BAST ada di Jadwal Penagihan.`}
    />
  );
}

function SummaryCard({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: "accent" | "danger";
}) {
  const toneClass =
    tone === "danger" ? "border-danger/30 bg-danger-bg" : tone === "accent" ? "border-accent/30 bg-accent-light" : "";
  const valueClass =
    tone === "danger" ? "text-danger" : "text-text-primary";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 shadow-sm ${toneClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-lg font-bold tabular-nums ${valueClass}`}>
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}

function BucketPill({
  label,
  summary,
  tone,
}: {
  label: string;
  summary: { count: number; total: string };
  tone: "accent" | "warning" | "danger";
}) {
  const toneClass =
    tone === "danger" ? "text-danger" : tone === "warning" ? "text-warning" : "text-text-primary";
  return (
    <div className="bg-surface border border-border rounded-lg px-3 py-2.5">
      <p className="text-[11px] text-text-secondary mb-0.5">
        {label} <span className="text-text-tertiary">({summary.count})</span>
      </p>
      <p className={`text-sm font-semibold tabular-nums ${toneClass}`}>
        <Rupiah value={summary.total} colorSign={false} />
      </p>
    </div>
  );
}
