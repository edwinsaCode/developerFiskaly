"use client";

// R1 — Pipeline KPR: satu layar semua kontrak KPR & posisinya. Jawaban untuk
// "jam 8 pagi staf keuangan mengejar siapa hari ini?" — kartu menua diberi
// warning; setiap kartu klik → workspace unit (aksi milestone/pencairan).

import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import type { KPRPipelineReport, KPRPipelineRow } from "@/lib/api/reports";

// Ambang umur proses per tahap (hari) — di atasnya kartu diberi warning.
const AGE_WARN_DAYS: Record<string, number> = {
  submitted_to_bank: 21, // bank biasanya jawab < 3 minggu
  bank_approved: 30,     // SP3K berlaku terbatas — akad jangan lewat sebulan
  akad: 14,              // pasca-akad dana biasanya cair cepat
  disbursed: 30,         // sisa kekurangan jangan menggantung sebulan
};

const COLUMNS: { key: string; label: string; states: string[]; hint: string }[] = [
  { key: "prep", label: "Belum Diajukan", states: ["signed", "dp_paid", "bank_rejected"], hint: "kontrak KPR tanpa pengajuan" },
  { key: "submitted", label: "Pengajuan", states: ["submitted_to_bank"], hint: "menunggu jawaban bank" },
  { key: "approved", label: "SP3K", states: ["bank_approved"], hint: "disetujui — menunggu akad" },
  { key: "akad", label: "Akad", states: ["akad"], hint: "menunggu pencairan" },
  { key: "disbursed", label: "Dana Cair", states: ["disbursed"], hint: "cek kekurangan pembayaran" },
];

function num(s: string | undefined): number {
  const n = parseFloat(s ?? "0");
  return isNaN(n) ? 0 : n;
}

function CardKPR({ r }: { r: KPRPipelineRow }) {
  const warnAfter = AGE_WARN_DAYS[r.scheme_state];
  const stale = warnAfter !== undefined && r.state_age_days > warnAfter;
  const outstanding = num(r.outstanding);
  return (
    <Link
      href={`/penjualan/${r.unit_id}`}
      className={`block rounded-lg border bg-surface p-3 shadow-sm transition-all hover:shadow hover:border-accent
        ${stale ? "border-warning" : "border-border"}`}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{r.buyer_name || "—"}</p>
          <p className="text-[11px] text-text-secondary">{r.unit_code} · {r.project_name}</p>
        </div>
        {stale && (
          <Badge variant="warning" className="shrink-0">{r.state_age_days} hari</Badge>
        )}
      </div>
      <dl className="mt-2 space-y-0.5 text-[11px] tabular-nums">
        <div className="flex justify-between">
          <dt className="text-text-tertiary">Harga</dt>
          <dd className="font-medium"><Rupiah value={r.gross_amount} colorSign={false} /></dd>
        </div>
        {r.bank_name && (
          <div className="flex justify-between">
            <dt className="text-text-tertiary">Bank</dt>
            <dd>{r.bank_name}</dd>
          </div>
        )}
        {num(r.disbursed_total) > 0 && (
          <div className="flex justify-between">
            <dt className="text-text-tertiary">Cair</dt>
            <dd className="font-medium text-success"><Rupiah value={r.disbursed_total} colorSign={false} /></dd>
          </div>
        )}
        <div className="flex justify-between">
          <dt className="text-text-tertiary">Outstanding</dt>
          <dd className={`font-semibold ${outstanding > 0 ? "text-accent" : "text-success"}`}>
            <Rupiah value={r.outstanding} colorSign={false} />
          </dd>
        </div>
      </dl>
      {!stale && r.state_age_days > 0 && (
        <p className="mt-1.5 text-[10px] text-text-tertiary">{r.state_age_days} hari di tahap ini</p>
      )}
    </Link>
  );
}

export function KPRPipelineBoard({ report }: { report: KPRPipelineReport }) {
  const active = report.rows.filter((r) => num(r.outstanding) > 0);
  const settled = report.rows.filter((r) => num(r.outstanding) <= 0);

  if (report.rows.length === 0) {
    return (
      <EmptyState
        title="Belum ada kontrak KPR"
        description="Kontrak dengan skema KPR akan muncul di sini beserta posisinya: pengajuan → SP3K → akad → pencairan → lunas. Buat kontrak dari halaman unit."
      />
    );
  }

  return (
    <div className="space-y-4">
      {/* Ringkasan agregat */}
      <div className="flex flex-wrap gap-3">
        <Card padding="sm" className="min-w-[170px]">
          <p className="text-[11px] text-text-secondary">Total Dana Cair</p>
          <p className="text-lg font-bold tabular-nums text-success">
            <Rupiah value={report.total_disbursed} colorSign={false} />
          </p>
        </Card>
        <Card padding="sm" className="min-w-[170px]">
          <p className="text-[11px] text-text-secondary">Outstanding KPR</p>
          <p className="text-lg font-bold tabular-nums text-accent">
            <Rupiah value={report.total_outstanding} colorSign={false} />
          </p>
        </Card>
        <Card padding="sm" className="min-w-[130px]">
          <p className="text-[11px] text-text-secondary">Akad</p>
          <p className="text-lg font-bold tabular-nums">{report.count_settled} konsumen</p>
        </Card>
      </div>

      {/* Board 5 kolom */}
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-5 gap-3">
        {COLUMNS.map((col) => {
          const rows = active.filter((r) => col.states.includes(r.scheme_state));
          return (
            <div key={col.key} className="flex flex-col">
              <div className="rounded-t-lg border border-b-2 border-border border-b-accent bg-border-subtle/60 px-3 py-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold uppercase tracking-wide">{col.label}</span>
                  <span className="rounded-full border border-border bg-surface px-2 text-xs font-bold">
                    {rows.length}
                  </span>
                </div>
                <p className="text-[10px] text-text-tertiary">{col.hint}</p>
              </div>
              <div className="flex-1 space-y-2 rounded-b-lg border border-t-0 border-border bg-border-subtle/20 p-2 min-h-[140px]">
                {rows.length === 0 ? (
                  <p className="py-5 text-center text-xs text-text-tertiary">—</p>
                ) : (
                  rows.map((r) => <CardKPR key={r.contract_id} r={r} />)
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* Lunas — daftar ringkas */}
      {settled.length > 0 && (
        <Card padding="sm">
          <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-secondary">
            Lunas ({settled.length})
          </p>
          <div className="flex flex-wrap gap-2">
            {settled.map((r) => (
              <Link key={r.contract_id} href={`/penjualan/${r.unit_id}`}>
                <Badge variant="success">{r.buyer_name} · {r.unit_code}</Badge>
              </Link>
            ))}
          </div>
        </Card>
      )}
    </div>
  );
}
