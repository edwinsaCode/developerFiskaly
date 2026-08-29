"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Card } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { EmptyState } from "@/components/ui/EmptyState";
import type { BillingPlan, BillingPlanStatus, BillingPlanUnit } from "@/lib/api/sale";

// W-8 · P6 — Jadwal Penagihan (pra-BAST).
//
// Ini BUKAN laporan piutang dan bukan mesin aging kedua: tidak ada bucket umur,
// tidak ada saldo akun. Ia menjawab satu hal — apa yang harus ditagih pada
// kontrak yang unitnya belum diserahterimakan. Status setiap cicilan dihitung
// backend dengan fungsi yang sama dengan rekening koran pembeli, supaya penagih
// dan pembeli tidak pernah melihat dua status berbeda untuk cicilan yang sama.

const statusLabel: Record<BillingPlanStatus, string> = {
  paid: "Lunas",
  overdue: "Menunggak",
  due_today: "Jatuh tempo hari ini",
  scheduled: "Dijadwalkan",
};

const statusVariant: Record<BillingPlanStatus, "success" | "danger" | "warning" | "neutral"> = {
  paid: "success",
  overdue: "danger",
  due_today: "warning",
  scheduled: "neutral",
};

export function BillingPlanView({ plan, asOf }: { plan: BillingPlan; asOf: string }) {
  const router = useRouter();

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2">
        <label className="text-sm text-text-secondary">Per tanggal</label>
        <input
          type="date"
          defaultValue={asOf}
          onChange={(e) =>
            e.target.value && router.push(`/accounting/billing-schedule?as_of=${e.target.value}`)
          }
          className="text-sm border border-border rounded-md px-2.5 py-1.5 bg-surface text-text-primary
            focus:outline-none focus:ring-2 focus:ring-accent/30"
        />
      </div>

      {/* Catatan definisi datang dari server: kalimat yang membedakan "rencana
          penagihan" dari "piutang" tidak boleh punya dua versi. */}
      <div className="rounded-lg border border-accent/30 bg-accent-light px-4 py-3">
        <p className="text-sm text-text-primary">{plan.note}</p>
        <p className="text-xs text-text-secondary mt-1">
          Setelah BAST, sisa harga unit pindah ke{" "}
          <Link href="/accounting/receivable?source=house" className="text-accent hover:underline">
            Piutang Customer
          </Link>{" "}
          dan mulai terhitung dalam umur piutang.
        </p>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <SummaryCard label="Total Dijadwalkan" value={plan.total_scheduled} />
        <SummaryCard label="Sudah Dibayar" value={plan.total_paid} />
        <SummaryCard label="Sisa Tagihan" value={plan.total_outstanding} tone="accent" />
        <SummaryCard
          label={`Lewat Jatuh Tempo (${plan.overdue_unit_count} unit)`}
          value={plan.total_overdue}
          tone="danger"
        />
      </div>

      {plan.units.length === 0 ? (
        <EmptyState
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
              strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 13l4 4L19 7" />
            </svg>
          }
          title="Semua kontrak sudah diserahterimakan"
          description={`Per ${asOf} tidak ada kontrak yang menunggu BAST. Sisa tagihan unit yang sudah diserahterimakan sudah menjadi piutang dan ada di Piutang Customer.`}
          action={
            <Link
              href="/accounting/receivable?source=house"
              className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
            >
              Ke Piutang Customer →
            </Link>
          }
        />
      ) : (
        <div className="space-y-3">
          {plan.units.map((u) => (
            <UnitRow key={u.unit_id} unit={u} />
          ))}
        </div>
      )}
    </div>
  );
}

// UnitRow default terbuka bila ada tunggakan: layar penagihan tidak boleh
// menyembunyikan pekerjaan yang mendesak di balik satu klik.
function UnitRow({ unit }: { unit: BillingPlanUnit }) {
  const [open, setOpen] = useState(unit.overdue_count > 0);

  return (
    <Card padding="none">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="w-full flex flex-wrap items-center justify-between gap-3 px-5 py-3 text-left hover:bg-border-subtle/40 transition-colors"
      >
        <div className="flex items-center gap-3 min-w-0">
          <span className="text-text-tertiary text-xs">{open ? "▾" : "▸"}</span>
          <span className="font-mono text-sm text-text-primary">{unit.unit_code || "—"}</span>
          <span className="text-sm text-text-secondary truncate">{unit.buyer_name}</span>
          {unit.overdue_count > 0 && (
            <Badge variant="danger">
              {unit.overdue_count} cicilan menunggak
            </Badge>
          )}
          {unit.scheme_state && <Badge variant="neutral">{unit.scheme_state}</Badge>}
        </div>
        <div className="flex items-center gap-5 text-sm tabular-nums">
          <span className="text-text-secondary">
            Nilai kontrak <Rupiah value={unit.contract_value} colorSign={false} />
          </span>
          <span className="text-text-primary font-semibold">
            Sisa <Rupiah value={unit.outstanding} colorSign={false} />
          </span>
        </div>
      </button>

      {open && (
        <div className="border-t border-border">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <tbody>
                {unit.schedules.map((l) => (
                  <tr key={l.schedule_id} className="border-b border-border-subtle last:border-0">
                    <td className="px-5 py-2 text-text-secondary whitespace-nowrap">
                      {l.installment_number > 0 && (
                        <span className="text-text-tertiary mr-1.5">#{l.installment_number}</span>
                      )}
                      {l.label}
                      {l.invoice_number && (
                        <span className="block text-[11px] text-text-tertiary font-mono">
                          {l.invoice_number}
                        </span>
                      )}
                    </td>
                    <td className="px-3 py-2 whitespace-nowrap">
                      <Tanggal value={l.due_date} />
                      {l.days_overdue > 0 && (
                        <span className="text-danger text-xs ml-1">(+{l.days_overdue}h)</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      <Rupiah value={l.amount} colorSign={false} />
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums text-text-secondary">
                      <Rupiah value={l.paid} colorSign={false} />
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums font-medium">
                      <Rupiah value={l.outstanding} colorSign={false} />
                    </td>
                    <td className="px-3 py-2">
                      <Badge variant={statusVariant[l.status]}>{statusLabel[l.status]}</Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 px-5 py-2.5 bg-border-subtle/30">
            <span className="text-xs text-text-secondary">
              {unit.next_due_date ? (
                <>
                  Jatuh tempo berikutnya <Tanggal value={unit.next_due_date} />
                </>
              ) : (
                "Tidak ada jatuh tempo berikutnya"
              )}
              {unit.buyer_phone && <span className="ml-3">· {unit.buyer_phone}</span>}
            </span>
            {/* Pembayaran tetap lewat pintu yang sudah ada — layar ini read-only. */}
            <Link
              href={`/accounting/receivable/${unit.contract_id}`}
              className="text-xs text-accent hover:underline whitespace-nowrap"
            >
              Rekening koran & catat pembayaran →
            </Link>
          </div>
        </div>
      )}
    </Card>
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
  const valueClass = tone === "danger" ? "text-danger" : "text-text-primary";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 shadow-sm ${toneClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-lg font-bold tabular-nums ${valueClass}`}>
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}
