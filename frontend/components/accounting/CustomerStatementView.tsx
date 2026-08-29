import { Fragment } from "react";
import Link from "next/link";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Badge } from "@/components/ui/Badge";
import { EmptyState } from "@/components/ui/EmptyState";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { PrintInvoiceButton } from "@/components/billing/PrintInvoiceButton";
import { RecordPaymentButton } from "@/components/billing/RecordPaymentButton";
import { Can } from "@/components/ui/Can";
import type {
  CustomerStatement, PaymentStatus, ScheduleType, AllocationView, Invoice, InvoiceStatus,
  StatementExposure,
} from "@/lib/types/api";

interface Props {
  statement: CustomerStatement;
  asOf: string;
  token: string;
  allocations?: AllocationView[];
  /** R2 Statement 360: daftar invoice kontrak (best-effort dari server page). */
  invoices?: Invoice[];
}

const statusLabel: Record<PaymentStatus, string> = {
  scheduled: "Dijadwalkan",
  due_today: "Jatuh Tempo Hari Ini",
  overdue: "Menunggak",
  paid: "Lunas",
};

const statusVariant: Record<PaymentStatus, "success" | "warning" | "danger" | "neutral"> = {
  scheduled: "neutral",
  due_today: "warning",
  overdue: "danger",
  paid: "success",
};

const typeLabel: Record<ScheduleType, string> = {
  dp: "Uang Muka",
  installment: "Termin",
  final: "Pelunasan",
};

const paymentTypeLabel: Record<string, string> = {
  kpr: "KPR",
  tunai: "Tunai",
};

// R2 — label sumber pembayaran (perjalanan customer).
const sourceLabel: Record<string, string> = {
  booking_fee: "Booking Fee",
  unit_termin: "Termin Unit",
  collection: "Pembayaran",
  schedule_received: "Cicilan",
  kpr_disbursement: "Pencairan Bank",
};

const sourceVariant: Record<string, "success" | "warning" | "danger" | "neutral" | "accent"> = {
  booking_fee: "neutral",
  unit_termin: "neutral",
  collection: "neutral",
  schedule_received: "neutral",
  kpr_disbursement: "accent",
};

// R2 — label event timeline (audit trail lifecycle).
const eventLabel: Record<string, string> = {
  contract_signed: "Kontrak ditandatangani",
  dp_paid: "DP diterima",
  installment_paid: "Cicilan diterima",
  fully_paid: "Lunas",
  submitted_to_bank: "Pengajuan KPR ke bank",
  bank_approved: "SP3K terbit",
  bank_rejected: "Ditolak bank",
  akad: "Akad kredit",
  disbursed: "Dana bank cair",
  handed_over: "Serah terima (BAST)",
  converted: "Skema dikonversi",
  cancelled: "Dibatalkan",
};

const invStatusVariant: Record<InvoiceStatus, "success" | "warning" | "danger" | "neutral"> = {
  issued: "warning", paid: "success", overdue: "danger", cancelled: "neutral",
};
const invStatusLabel: Record<InvoiceStatus, string> = {
  issued: "Diterbitkan", paid: "Lunas", overdue: "Jatuh Tempo", cancelled: "Batal",
};
const invTypeLabel: Record<string, string> = {
  DP: "Uang Muka", TERMIN: "Termin", PELUNASAN: "Pelunasan", KEKURANGAN: "Kekurangan",
};

const schemeStateLabel: Record<string, string> = {
  signed: "Kontrak", dp_paid: "DP Dibayar", installment_running: "Cicilan Berjalan",
  submitted_to_bank: "Pengajuan KPR", bank_approved: "SP3K", bank_rejected: "Ditolak Bank",
  akad: "Akad Kredit", disbursed: "Dana Cair", fully_paid: "Lunas",
  handed_over: "Serah Terima", converted: "Dikonversi", cancelled: "Batal",
};

export function CustomerStatementView({ statement, asOf, token, allocations = [], invoices = [] }: Props) {
  const s = statement;
  const hasSchedules = s.schedules.length > 0;
  const remainingPositive = s.remaining_balance !== "0";

  // FE-2 · P4 — breakdown alokasi sub-ledger: kelompokkan per cicilan + saldo
  // kredit + pembayaran langsung. 'direct' (pasca-BAST, tak cocok jadwal tapi
  // sudah mengurangi piutang/pembiayaan riil) TIDAK boleh masuk buyerCredits —
  // itu bukan dana bebas yang bisa dipakai ulang, beda dari 'buyer_credit'
  // (lihat AllocationTypeDirect di backend/internal/sale/payment_allocation.go).
  const allocBySchedule = new Map<number, AllocationView[]>();
  const buyerCredits: AllocationView[] = [];
  const directAllocs: AllocationView[] = [];
  for (const a of allocations) {
    if (a.allocation_type === "buyer_credit") {
      buyerCredits.push(a);
    } else if (a.allocation_type === "direct" || a.payment_schedule_id == null) {
      directAllocs.push(a);
    } else {
      const list = allocBySchedule.get(a.payment_schedule_id) ?? [];
      list.push(a);
      allocBySchedule.set(a.payment_schedule_id, list);
    }
  }

  return (
    <div className="space-y-6">
      {/* Identitas buyer & kontrak */}
      <Card>
        <div className="flex items-start justify-between gap-4 flex-wrap">
          <div>
            <h1 className="display-lg text-2xl text-text-primary">{s.buyer_name}</h1>
            <p className="text-sm text-text-secondary mt-0.5">
              NIK {s.buyer_id} · Unit{" "}
              <Link href={`/penjualan/${s.unit_id}`} className="text-accent hover:underline font-mono">
                #{s.unit_id}
              </Link>
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="neutral">{paymentTypeLabel[s.payment_type] ?? s.payment_type}</Badge>
            {s.scheme_state && (
              <Badge variant={s.scheme_state === "handed_over" || s.scheme_state === "fully_paid" ? "success" : "accent"}>
                {schemeStateLabel[s.scheme_state] ?? s.scheme_state}
              </Badge>
            )}
            {s.is_pkp && <Badge variant="accent">PKP / PPN</Badge>}
            {remainingPositive && (
              <Can roles={["owner", "accountant"]}>
                <RecordPaymentButton
                  token={token}
                  contractId={s.contract_id}
                  outstanding={s.remaining_balance}
                  buyerName={s.buyer_name}
                />
              </Can>
            )}
          </div>
        </div>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mt-4 pt-4 border-t border-border text-sm">
          <Field label="Tgl Kontrak"><Tanggal value={s.contract_date} /></Field>
          <Field label="Nilai Kontrak"><Rupiah value={s.contract_value} colorSign={false} /></Field>
          {s.summary && s.summary.discount !== "0" ? (
            <Field label="Diskon"><Rupiah value={s.summary.discount} colorSign={false} /></Field>
          ) : (
            <Field label="DPP (sebelum PPN)"><Rupiah value={s.dpp_amount} colorSign={false} /></Field>
          )}
          <Field label="Per Tanggal">{asOf}</Field>
        </div>
        {/* R2: konteks pembiayaan KPR di header */}
        {s.payment_type === "kpr" && (s.bank_kpr || s.loan_amount) && (
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mt-3 pt-3 border-t border-border text-sm">
            {s.bank_kpr && <Field label="Bank KPR">{s.bank_kpr}</Field>}
            {s.loan_amount && (
              <Field label="Plafon Kredit"><Rupiah value={s.loan_amount} colorSign={false} /></Field>
            )}
          </div>
        )}
      </Card>

      {/* Kartu ringkasan */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <SummaryCard label="Nilai Kontrak" value={s.contract_value} />
        <SummaryCard label="Sudah Dibayar" value={s.total_paid} tone="success" />
        <SummaryCard label="Sisa Tagihan" value={s.remaining_balance} tone={remainingPositive ? undefined : "success"} />
        <SummaryCard label={`Menunggak (${s.overdue_count})`} value={s.total_overdue} tone={s.total_overdue !== "0" ? "danger" : undefined} />
      </div>

      {/* W-4 — Total Tagihan Customer */}
      {s.exposure && <ExposureCard exposure={s.exposure} unitId={s.unit_id} asOf={asOf} />}

      {/* R2 — Riwayat Pembayaran (perjalanan uang per sumber) */}
      {(s.payments?.length ?? 0) > 0 && (
        <Card padding="none">
          <CardHeader className="px-5 pt-5">
            <CardTitle>Riwayat Pembayaran</CardTitle>
          </CardHeader>
          <Table>
            <TableHead>
              <TableRow>
                <Th>Tanggal</Th>
                <Th>Sumber</Th>
                <Th>Keterangan</Th>
                <Th right>Nominal</Th>
                <Th>Kwitansi</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {s.payments!.map((p) => (
                <TableRow key={p.termin_id}>
                  <Td><Tanggal value={p.date} /></Td>
                  <Td>
                    <Badge variant={sourceVariant[p.source] ?? "neutral"}>
                      {sourceLabel[p.source] ?? p.source}
                    </Badge>
                    {/* R4: fee di luar harga unit — transparan tapi terpisah */}
                    {p.counts_toward_price === false && (
                      <span className="ml-1.5 rounded-full bg-border-subtle px-1.5 py-0.5 text-[10px] text-text-tertiary">
                        di luar harga
                      </span>
                    )}
                  </Td>
                  <Td className="text-text-secondary text-xs max-w-[260px] truncate">
                    {p.reference || "—"}
                  </Td>
                  <Td right>
                    <span
                      className={
                        p.counts_toward_price === false
                          ? "text-text-tertiary"
                          : p.source === "kpr_disbursement"
                            ? "text-success font-semibold"
                            : ""
                      }
                    >
                      <Rupiah value={p.amount} colorSign={false} />
                    </span>
                  </Td>
                  <Td>
                    <PrintReceiptButton
                      token={token}
                      terminId={p.termin_id}
                      notes={`${sourceLabel[p.source] ?? p.source} — ${s.buyer_name}`}
                    />
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-border-subtle/30">
            <span className="text-sm font-semibold">
              Total Diterima
              <span className="ml-1.5 font-normal text-xs text-text-tertiary">
                (pembayaran harga — di luar harga tidak dihitung)
              </span>
            </span>
            <span className="font-bold tabular-nums text-sm text-success">
              <Rupiah value={s.total_paid} colorSign={false} />
            </span>
          </div>
        </Card>
      )}

      {/* R2 — Invoice kontrak (dokumen tagihan resmi) */}
      {invoices.length > 0 && (
        <Card padding="none">
          <CardHeader className="px-5 pt-5">
            <CardTitle>Invoice</CardTitle>
          </CardHeader>
          <Table>
            <TableHead>
              <TableRow>
                <Th>Nomor</Th>
                <Th>Jenis</Th>
                <Th>Terbit</Th>
                <Th>Jatuh Tempo</Th>
                <Th right>Nominal</Th>
                <Th>Status</Th>
                <Th>Cetak</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {invoices.map((inv) => (
                <TableRow key={inv.id}>
                  <Td className="font-mono text-xs">{inv.invoice_number}</Td>
                  <Td>
                    <Badge variant={inv.invoice_type === "KEKURANGAN" ? "warning" : "neutral"}>
                      {invTypeLabel[inv.invoice_type] ?? inv.invoice_type}
                    </Badge>
                  </Td>
                  <Td><Tanggal value={inv.issue_date} /></Td>
                  <Td><Tanggal value={inv.due_date} /></Td>
                  <Td right><Rupiah value={inv.amount} colorSign={false} /></Td>
                  <Td><Badge variant={invStatusVariant[inv.status]}>{invStatusLabel[inv.status]}</Badge></Td>
                  <Td><PrintInvoiceButton token={token} invoiceId={inv.id} /></Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}

      {/* Jadwal pembayaran */}
      <Card padding="none">
        <CardHeader className="px-5 pt-5">
          <CardTitle>Jadwal Pembayaran</CardTitle>
        </CardHeader>
        {!hasSchedules ? (
          <EmptyState
            icon={
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
                strokeLinecap="round" strokeLinejoin="round">
                <path d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
              </svg>
            }
            title="Belum ada jadwal cicilan"
            description="Kontrak ini belum memiliki jadwal pembayaran. Buat jadwal cicilan (DP, termin, pelunasan) di halaman unit agar tagihan buyer terlacak dan dapat ditagih."
            action={
              <Link
                href={`/penjualan/${s.unit_id}`}
                className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
              >
                Buka Unit →
              </Link>
            }
          />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <Th>#</Th>
                <Th>Jenis</Th>
                <Th>Jatuh Tempo</Th>
                <Th right>Nominal</Th>
                {/* T-5: pembayaran sebagian harus terlihat — angka Sisa di sini
                    memakai definisi yang sama persis dengan AR Aging. */}
                <Th right>Dibayar</Th>
                <Th right>Sisa</Th>
                <Th>Status</Th>
                <Th>Diterima</Th>
                <Th>Kwitansi</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {s.schedules.map((line) => {
                const allocs = allocBySchedule.get(line.schedule_id) ?? [];
                return (
                  <Fragment key={line.schedule_id}>
                    <TableRow>
                      <Td>{line.installment_number}</Td>
                      <Td>{typeLabel[line.type] ?? line.type}</Td>
                      <Td>
                        <Tanggal value={line.due_date} />
                        {line.overdue && (
                          <span className="text-danger text-xs ml-1">(+{line.days_overdue}h)</span>
                        )}
                      </Td>
                      <Td right><Rupiah value={line.amount} colorSign={false} /></Td>
                      <Td right><Rupiah value={line.paid} colorSign={false} /></Td>
                      <Td right className={line.overdue ? "text-danger font-medium" : undefined}>
                        <Rupiah value={line.outstanding} colorSign={false} />
                      </Td>
                      <Td><Badge variant={statusVariant[line.status]}>{statusLabel[line.status]}</Badge></Td>
                      <Td>{line.received_at ? <Tanggal value={line.received_at} /> : "—"}</Td>
                      <Td>
                        {line.status === "paid" && line.termin_payment_id ? (
                          <PrintReceiptButton
                            token={token}
                            terminId={line.termin_payment_id}
                            notes={`${typeLabel[line.type] ?? line.type} #${line.installment_number} — ${s.buyer_name}`}
                          />
                        ) : (
                          <span className="text-text-tertiary text-xs">—</span>
                        )}
                      </Td>
                    </TableRow>
                    {allocs.length > 0 && (
                      <TableRow>
                        <Td className="!py-1.5 text-text-secondary" colSpan={9}>
                          <span className="text-xs">Dibayar oleh: </span>
                          <span className="inline-flex flex-wrap gap-1.5 align-middle">
                            {allocs.map((a) => (
                              <span
                                key={a.id}
                                className="inline-flex items-center gap-1 rounded bg-border-subtle/50 px-1.5 py-0.5 text-xs tabular-nums"
                                title={a.receipt_number ? `Kwitansi ${a.receipt_number}` : undefined}
                              >
                                <Rupiah value={a.amount} colorSign={false} />
                                <span className="text-text-tertiary">·</span>
                                <Tanggal value={a.termin_date} />
                                {a.receipt_number && (
                                  <span className="text-text-tertiary font-mono">{a.receipt_number}</span>
                                )}
                              </span>
                            ))}
                          </span>
                        </Td>
                      </TableRow>
                    )}
                  </Fragment>
                );
              })}
            </TableBody>
          </Table>
        )}
        {hasSchedules && (
          <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-border-subtle/30">
            <span className="text-sm font-semibold">Total Dijadwalkan</span>
            <span className="font-bold tabular-nums text-sm">
              <Rupiah value={s.total_scheduled} colorSign={false} />
            </span>
          </div>
        )}
      </Card>

      {/* FE-2 · P4 — Saldo kredit buyer (kelebihan bayar belum teralokasi ke cicilan) */}
      {buyerCredits.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Saldo Kredit Buyer</CardTitle>
          </CardHeader>
          <p className="text-sm text-text-secondary mb-3">
            Kelebihan pembayaran yang belum dialokasikan ke cicilan mana pun. Otomatis dipakai untuk menutup
            tagihan berikutnya.
          </p>
          <ul className="divide-y divide-border text-sm">
            {buyerCredits.map((a) => (
              <li key={a.id} className="flex items-center justify-between py-2">
                <span className="text-text-secondary inline-flex items-center gap-2">
                  <Tanggal value={a.termin_date} />
                  {a.receipt_number && (
                    <span className="text-text-tertiary font-mono text-xs">{a.receipt_number}</span>
                  )}
                </span>
                <span className="font-semibold tabular-nums text-success">
                  <Rupiah value={a.amount} colorSign={false} />
                </span>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {/* Pembayaran pasca-BAST yang tak cocok satu jadwal cicilan pun, tapi
          sudah mengurangi piutang/pembiayaan riil langsung (BUKAN kelebihan
          bayar — jangan digabung ke Saldo Kredit Buyer). */}
      {directAllocs.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Pembayaran Langsung</CardTitle>
          </CardHeader>
          <p className="text-sm text-text-secondary mb-3">
            Tidak cocok jadwal cicilan manapun, tapi sudah mengurangi piutang/pembiayaan secara langsung — bukan
            kelebihan bayar dan tidak menjadi saldo kredit.
          </p>
          <ul className="divide-y divide-border text-sm">
            {directAllocs.map((a) => (
              <li key={a.id} className="flex items-center justify-between py-2">
                <span className="text-text-secondary inline-flex items-center gap-2">
                  <Tanggal value={a.termin_date} />
                  {a.receipt_number && (
                    <span className="text-text-tertiary font-mono text-xs">{a.receipt_number}</span>
                  )}
                </span>
                <span className="font-semibold tabular-nums">
                  <Rupiah value={a.amount} colorSign={false} />
                </span>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {/* R2 — Perjalanan Kontrak (timeline milestone, audit trail) */}
      {(s.timeline?.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Perjalanan Kontrak</CardTitle>
          </CardHeader>
          <ol className="relative border-l border-border ml-2 space-y-4 pt-1">
            {s.timeline!.map((ev) => (
              <li key={ev.id} className="ml-4">
                <span className="absolute -left-[5px] mt-1.5 h-2.5 w-2.5 rounded-full bg-accent" />
                <div className="flex items-baseline gap-2 flex-wrap">
                  <p className="text-sm font-medium text-text-primary">
                    {eventLabel[ev.event] ?? ev.event}
                  </p>
                  <span className="text-xs text-text-tertiary">
                    <Tanggal value={ev.event_date} />
                  </span>
                </div>
                {ev.notes && <p className="text-xs text-text-secondary mt-0.5">{ev.notes}</p>}
              </li>
            ))}
          </ol>
        </Card>
      )}

      <p className="text-xs text-text-tertiary">
        &quot;Sudah Dibayar&quot; adalah total penerimaan kas (termin) atas unit ini — dasar perhitungan sisa
        tagihan. Baris &quot;Dibayar oleh&quot; di bawah tiap cicilan menampilkan penerimaan (dan nomor kwitansi)
        yang menutup cicilan tersebut — bersumber dari sub-ledger alokasi pembayaran.
      </p>
    </div>
  );
}

// ExposureCard menjawab satu pertanyaan yang dulu tidak punya jawaban di layar
// mana pun: "customer ini masih harus bayar berapa, semuanya?". Kartu di
// atasnya bicara tentang KONTRAK (harga rumah); kartu ini bicara tentang ORANG
// yang ditagih — termasuk biaya realisasi yang bukan bagian harga rumah.
//
// Semua angka datang jadi dari server (mesin yang sama dengan AR Aging).
// Tidak ada penjumlahan di sini; kalau layar boleh menjumlahkan sendiri, cepat
// atau lambat layar ini dan halaman piutang akan berbeda.
function ExposureCard({
  exposure,
  unitId,
  asOf,
}: {
  exposure: StatementExposure;
  unitId: number;
  asOf: string;
}) {
  const menunggak = exposure.total_overdue !== "0";
  return (
    <Card>
      <CardHeader>
        <CardTitle>Total Tagihan Customer</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Harga rumah dan biaya realisasi, per {asOf}.
        </p>
      </CardHeader>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mt-3">
        <div>
          <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">Harga Rumah</p>
          <p className="text-lg font-bold tabular-nums text-text-primary">
            <Rupiah value={exposure.house_outstanding} colorSign={false} />
          </p>
          <p className="text-[11px] text-text-tertiary mt-0.5">
            Menunggak <Rupiah value={exposure.house_overdue} colorSign={false} />
          </p>
        </div>

        <div>
          <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">Biaya Realisasi</p>
          {exposure.realization_available ? (
            <>
              <p className="text-lg font-bold tabular-nums text-text-primary">
                <Rupiah value={exposure.realization_outstanding} colorSign={false} />
              </p>
              <p className="text-[11px] text-text-tertiary mt-0.5">
                Menunggak <Rupiah value={exposure.realization_overdue} colorSign={false} />{" "}
                <Link href={`/penjualan/${unitId}/tagihan`} className="text-accent hover:underline">
                  · rincian
                </Link>
              </p>
            </>
          ) : (
            // Menampilkan Rp0 di sini berarti berbohong dengan percaya diri:
            // nol dan "tidak diketahui" adalah dua hal yang sangat berbeda
            // ketika seseorang memutuskan apakah customer boleh serah terima.
            <>
              <p className="text-lg font-bold tabular-nums text-text-tertiary">—</p>
              <p className="text-[11px] text-text-tertiary mt-0.5">Tidak dapat dibaca</p>
            </>
          )}
        </div>

        <div className={`rounded-lg px-3 py-2 -mx-1 ${menunggak ? "bg-danger-bg" : "bg-border-subtle/40"}`}>
          <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">Total Ditagih</p>
          <p className={`text-lg font-bold tabular-nums ${menunggak ? "text-danger" : "text-text-primary"}`}>
            <Rupiah value={exposure.total_outstanding} colorSign={false} />
          </p>
          <p className="text-[11px] text-text-tertiary mt-0.5">
            Menunggak <Rupiah value={exposure.total_overdue} colorSign={false} />
            {!exposure.realization_available && " (biaya realisasi belum terhitung)"}
          </p>
        </div>
      </div>
    </Card>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-xs text-text-secondary mb-0.5">{label}</p>
      <p className="text-text-primary">{children}</p>
    </div>
  );
}

function SummaryCard({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: "success" | "danger";
}) {
  const toneClass =
    tone === "danger" ? "border-danger/30 bg-danger-bg" : tone === "success" ? "border-success/30 bg-success-bg" : "";
  const valueClass =
    tone === "danger" ? "text-danger" : tone === "success" ? "text-success" : "text-text-primary";
  return (
    <div className={`bg-surface border border-border rounded-lg px-4 py-3 shadow-sm ${toneClass}`}>
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-lg font-bold tabular-nums ${valueClass}`}>
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}
