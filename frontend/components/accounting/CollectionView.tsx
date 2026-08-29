"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Badge } from "@/components/ui/Badge";
import { EmptyState } from "@/components/ui/EmptyState";
import { RecordPaymentButton } from "@/components/billing/RecordPaymentButton";
import { Can } from "@/components/ui/Can";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";
import { markOverdueSchedules } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import type { ARAgingReport, ARAgingRow, PaymentStatus } from "@/lib/types/api";

interface Props {
  report: ARAgingReport;
  asOf: string;
  token: string;
}

const statusLabel: Record<PaymentStatus, string> = {
  scheduled: "Dijadwalkan",
  due_today: "Jatuh Tempo Hari Ini",
  overdue: "Menunggak",
  paid: "Lunas",
};

const statusVariant: Record<PaymentStatus, "success" | "warning" | "danger" | "neutral" | "accent"> = {
  scheduled: "accent",
  due_today: "warning",
  overdue: "danger",
  paid: "success",
};

type FilterKey = "all" | "due_today" | "overdue" | "due_week" | "scheduled";

export function CollectionView({ report, asOf, token }: Props) {
  const router = useRouter();
  const { toast } = useToast();
  const [filter, setFilter] = useState<FilterKey>("all");
  const [sweeping, setSweeping] = useState(false);

  async function handleSweep() {
    setSweeping(true);
    try {
      const r = await markOverdueSchedules(token);
      const n = r.marked_overdue ?? r.count ?? 0;
      toast(n > 0 ? `${n} cicilan ditandai menunggak` : "Tidak ada cicilan baru yang menunggak", "success");
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memproses", "error");
    } finally {
      setSweeping(false);
    }
  }

  function onAsOfChange(value: string) {
    if (!value) return;
    router.push(`/accounting/collection?as_of=${value}`);
  }

  const rows = applyFilter(report.rows, filter);
  const rateNum = parseFloat(report.collection_rate);
  const rateColor = rateNum >= 80 ? "text-success" : rateNum >= 50 ? "text-warning" : "text-danger";

  const counts = {
    all: report.rows.length,
    due_today: report.rows.filter((r) => r.status === "due_today").length,
    overdue: report.rows.filter((r) => r.status === "overdue").length,
    due_week: report.rows.filter((r) => r.due_this_week).length,
    scheduled: report.rows.filter((r) => r.status === "scheduled" && !r.due_this_week).length,
  };

  return (
    <div className="space-y-6">
      {/* Header: aksi + tanggal */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <Can roles={["owner", "accountant"]}>
          <Button size="sm" variant="secondary" onClick={handleSweep} loading={sweeping}>
            Tandai Menunggak (sweep)
          </Button>
        </Can>
      <div className="flex items-center gap-2">
        <label className="text-sm text-text-secondary">Per tanggal</label>
        <input
          type="date"
          defaultValue={asOf}
          onChange={(e) => onAsOfChange(e.target.value)}
          className="text-sm border border-border rounded-md px-2.5 py-1.5 bg-surface text-text-primary
            focus:outline-none focus:ring-2 focus:ring-accent/30"
        />
      </div>
      </div>

      {/* Kartu ringkasan */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-4">
        <SummaryCard label="Total Outstanding" value={report.total_piutang} />
        <SummaryCard
          label={`Jatuh Tempo HARI INI (${report.due_today_count})`}
          value={report.due_today_amount}
          tone={report.due_today_count > 0 ? "warning" : undefined}
        />
        <SummaryCard label="Menunggak (Overdue)" value={report.overdue} tone={report.overdue !== "0" ? "danger" : undefined} />
        <SummaryCard label="Jatuh Tempo Minggu Ini" value={report.due_this_week} tone={report.due_this_week !== "0" ? "warning" : undefined} />
        <Card>
          <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">Collection Rate</p>
          <p className={`text-lg font-bold tabular-nums ${rateColor}`}>{report.collection_rate}%</p>
          <p className="text-[11px] text-text-tertiary mt-0.5">
            <Rupiah value={report.total_collected} colorSign={false} /> /{" "}
            <Rupiah value={report.total_scheduled} colorSign={false} /> tertagih
          </p>
        </Card>
      </div>

      {/* Prediksi kas masuk (asumsi dibayar tepat waktu) */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        {[
          { label: "30 hari", v: report.expected_30 },
          { label: "60 hari", v: report.expected_60 },
          { label: "90 hari", v: report.expected_90 },
        ].map((e) => (
          <div key={e.label} className="rounded-lg border border-border bg-surface px-4 py-3">
            <p className="text-[11px] uppercase tracking-wide text-text-secondary">
              Prediksi Kas Masuk {e.label}
            </p>
            <p className="mt-1 text-base font-semibold tabular-nums text-success">
              <Rupiah value={e.v} colorSign={false} />
            </p>
          </div>
        ))}
      </div>

      {/* Aging bar */}
      <AgingBar report={report} />

      {/* Filter worklist */}
      {report.rows.length > 0 && (
        <div className="flex flex-wrap gap-2">
          <FilterTab active={filter === "all"} onClick={() => setFilter("all")} label={`Semua (${counts.all})`} />
          <FilterTab active={filter === "due_today"} onClick={() => setFilter("due_today")} label={`Hari Ini (${counts.due_today})`} tone="warning" />
          <FilterTab active={filter === "overdue"} onClick={() => setFilter("overdue")} label={`Menunggak (${counts.overdue})`} tone="danger" />
          <FilterTab active={filter === "due_week"} onClick={() => setFilter("due_week")} label={`Minggu Ini (${counts.due_week})`} tone="warning" />
          <FilterTab active={filter === "scheduled"} onClick={() => setFilter("scheduled")} label={`Mendatang (${counts.scheduled})`} />
        </div>
      )}

      {/* Worklist */}
      <Card padding="none">
        {report.rows.length === 0 ? (
          report.total_scheduled === "0" ? (
            <EmptyState
              icon={<CollectIcon />}
              title="Belum ada tagihan untuk ditagih"
              description="Daftar penagihan terisi dari jadwal cicilan pada kontrak penjualan. Buat kontrak & jadwal pembayaran di Unit & Penjualan agar tim finance punya daftar kerja."
              action={
                <Link href="/penjualan" className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors">
                  Ke Unit & Penjualan →
                </Link>
              }
            />
          ) : (
            // W-8 · BD-1 — layar ini hanya melihat piutang yang sudah diakui.
            // "Semua cicilan sudah diterima" adalah klaim yang salah selama
            // masih ada cicilan menunggak pada kontrak pra-BAST: cicilan itu
            // memang tidak pernah masuk ke sini. Kosongnya daftar kerja tidak
            // boleh dibaca sebagai tidak ada yang perlu ditagih.
            <EmptyState
              icon={<CheckIcon />}
              title="Tidak ada piutang outstanding 🎉"
              description={`Per ${asOf} tidak ada piutang yang perlu dikejar di layar ini. Collection rate ${report.collection_rate}%. Cicilan pada kontrak yang belum BAST tidak dihitung di sini — periksa Jadwal Penagihan sebelum menyimpulkan tidak ada tagihan.`}
              action={
                <Link
                  href="/accounting/billing-schedule"
                  className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
                >
                  Ke Jadwal Penagihan →
                </Link>
              }
            />
          )
        ) : rows.length === 0 ? (
          <EmptyState title="Tidak ada baris pada filter ini" description="Ubah filter untuk melihat tagihan lain." />
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
                <Th right>Telat (hari)</Th>
                <Th>Status</Th>
                <Th></Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((row, i) => (
                <TableRow key={`${row.source}-${row.ref_id ?? 0}-${row.contract_id}-${i}`}>
                  <Td>
                    <Badge variant={row.source === "realization" ? "warning" : "neutral"}>
                      {row.source === "realization" ? "Biaya Realisasi" : "Harga Rumah"}
                    </Badge>
                  </Td>
                  <Td>{row.buyer_name}</Td>
                  <Td mono>{row.unit_code}</Td>
                  <Td>{row.label || "—"}</Td>
                  <Td><Tanggal value={row.due_date} /></Td>
                  <Td right><Rupiah value={row.amount} colorSign={false} /></Td>
                  <Td right><Rupiah value={row.paid} colorSign={false} /></Td>
                  <Td right><Rupiah value={row.outstanding} colorSign={false} /></Td>
                  <Td right>{row.days_overdue > 0 ? <span className="text-danger font-medium">{row.days_overdue}</span> : "—"}</Td>
                  <Td><Badge variant={statusVariant[row.status]}>{statusLabel[row.status]}</Badge></Td>
                  <Td>
                    <div className="flex items-center gap-3 justify-end">
                      <ReminderActions row={row} />
                      {/* Uang biaya realisasi TIDAK boleh masuk lewat pintu
                          cicilan harga rumah — ia titipan, bukan pembayaran
                          harga. Barisnya diarahkan ke layar tagihan unit yang
                          punya alokasi per item. */}
                      {row.source === "realization" ? (
                        <Link
                          href={`/penjualan/${row.unit_id}/tagihan`}
                          className="text-xs text-accent hover:underline whitespace-nowrap"
                        >
                          Bayar di Tagihan →
                        </Link>
                      ) : (
                        <>
                          <Can roles={["owner", "accountant"]}>
                            <RecordPaymentButton
                              token={token}
                              contractId={row.contract_id}
                              outstanding={row.outstanding}
                              buyerName={row.buyer_name}
                              variant="link"
                              label="Bayar"
                            />
                          </Can>
                          <Link href={`/penjualan/${row.unit_id}/invoice`} className="text-xs text-accent hover:underline whitespace-nowrap">
                            Invoice
                          </Link>
                        </>
                      )}
                      <Link href={`/accounting/receivable/${row.contract_id}`} className="text-xs text-accent hover:underline whitespace-nowrap">
                        Statement →
                      </Link>
                    </div>
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  );
}

function applyFilter(rows: ARAgingRow[], filter: FilterKey): ARAgingRow[] {
  switch (filter) {
    case "due_today":
      return rows.filter((r) => r.status === "due_today");
    case "overdue":
      return rows.filter((r) => r.status === "overdue");
    case "due_week":
      return rows.filter((r) => r.due_this_week);
    case "scheduled":
      return rows.filter((r) => r.status === "scheduled" && !r.due_this_week);
    default:
      return rows;
  }
}

function FilterTab({ active, onClick, label, tone }: { active: boolean; onClick: () => void; label: string; tone?: "danger" | "warning" }) {
  const base = "px-3 py-1.5 rounded-md text-sm font-medium border transition-colors";
  const activeCls = tone === "danger"
    ? "bg-danger-bg border-danger/40 text-danger"
    : tone === "warning"
    ? "bg-warning-bg border-warning/40 text-warning"
    : "bg-accent/10 border-accent/40 text-accent";
  const idle = "bg-surface border-border text-text-secondary hover:text-accent hover:border-accent/40";
  return (
    <button onClick={onClick} className={`${base} ${active ? activeCls : idle}`}>
      {label}
    </button>
  );
}

function SummaryCard({ label, value, tone }: { label: string; value: string; tone?: "danger" | "warning" }) {
  const toneClass = tone === "danger" ? "border-danger/30 bg-danger-bg" : tone === "warning" ? "border-warning/30 bg-warning-bg" : "";
  const valueClass = tone === "danger" ? "text-danger" : tone === "warning" ? "text-warning" : "text-text-primary";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 shadow-sm ${toneClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-lg font-bold tabular-nums ${valueClass}`}>
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}

function CollectIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d="M3 7h18M3 12h18M3 17h12" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 13l4 4L19 7" />
    </svg>
  );
}


// ── PS-4: aksi reminder (WA/Email — template dari data baris, tanpa backend) ──

function reminderText(row: ARAgingRow): string {
  const due = new Date(row.due_date).toLocaleDateString("id-ID", { day: "numeric", month: "long", year: "numeric" });
  const amount = "Rp " + parseInt(row.outstanding, 10).toLocaleString("id-ID");
  const late = row.days_overdue > 0 ? ` (terlambat ${row.days_overdue} hari)` : "";
  return `Yth. ${row.buyer_name},\n\nMengingatkan pembayaran unit ${row.unit_code} sebesar ${amount} jatuh tempo ${due}${late}.\n\nMohon konfirmasi setelah pembayaran. Terima kasih.`;
}

function ReminderActions({ row }: { row: ARAgingRow }) {
  if (row.status === "paid") return null;
  const text = encodeURIComponent(reminderText(row));
  const phone = (row.buyer_phone ?? "").replace(/[^0-9]/g, "").replace(/^0/, "62");
  return (
    <span className="flex items-center gap-2">
      {phone ? (
        <a
          href={`https://wa.me/${phone}?text=${text}`}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs text-success hover:underline whitespace-nowrap"
          title="Kirim reminder WhatsApp"
        >
          WA
        </a>
      ) : (
        <span className="text-xs text-text-tertiary" title="Nomor telepon customer belum diisi">WA</span>
      )}
      {row.buyer_email ? (
        <a
          href={`mailto:${row.buyer_email}?subject=${encodeURIComponent(`Reminder Pembayaran Unit ${row.unit_code}`)}&body=${text}`}
          className="text-xs text-accent hover:underline"
          title="Kirim reminder email"
        >
          Email
        </a>
      ) : null}
    </span>
  );
}

// ── PS-4: visual umur piutang ─────────────────────────────────────────────────

function AgingBar({ report }: { report: ARAgingReport }) {
  const total = parseFloat(report.total_piutang);
  if (!total || total <= 0) return null;
  // Aging = severity BERJENJANG → eskalasi satu arah (sehat→kritis), bukan
  // rainbow. Sehat=hijau, menua=amber(warning), macet=merah(danger) menua.
  const segs = [
    { label: "Belum jatuh tempo", v: report.buckets.current, cls: "bg-success" },
    { label: "1–30 hari", v: report.buckets.b1_30, cls: "bg-warning/70" },
    { label: "31–60 hari", v: report.buckets.b31_60, cls: "bg-warning" },
    { label: "61–90 hari", v: report.buckets.b61_90, cls: "bg-danger/70" },
    { label: ">90 hari", v: report.buckets.b90_plus, cls: "bg-danger" },
  ].filter((s) => parseFloat(s.v.total) > 0);
  return (
    <div className="rounded-lg border border-border bg-surface px-4 py-3">
      <p className="text-[11px] uppercase tracking-wide text-text-secondary mb-2">Umur Piutang</p>
      <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-border-subtle">
        {segs.map((s) => (
          <div
            key={s.label}
            className={s.cls}
            style={{ width: `${(parseFloat(s.v.total) / total) * 100}%` }}
            title={`${s.label}: ${s.v.count} tagihan`}
          />
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-text-secondary">
        {segs.map((s) => (
          <span key={s.label} className="inline-flex items-center gap-1.5">
            <span className={`h-2 w-2 rounded-full ${s.cls}`} />
            {s.label}: <Rupiah value={s.v.total} colorSign={false} /> ({s.v.count})
          </span>
        ))}
      </div>
    </div>
  );
}
