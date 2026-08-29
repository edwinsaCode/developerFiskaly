"use client";

// Detail tagihan vendor (W-11).
//
// Aksi mengikuti DUA status sekaligus, dan keduanya milik backend:
//   draft                      → Posting
//   posted                     → Balikkan
//   posted + masih bersisa     → Bayar (menuju layar pembayaran)
//   reversed                   → tidak ada aksi (append-only; jejaknya tetap)
//
// Tombol Bayar muncul berdasarkan `payment_status` dari server, bukan dari
// perbandingan angka di layar. Tidak ada tombol Uang Muka, Retensi, atau
// Pelepasan Retensi: ketiganya ditolak backend pada tahap ini, dan tombol yang
// pasti gagal lebih buruk daripada tombol yang tidak ada.

import Link from "next/link";
import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Input } from "@/components/ui/Input";
import { Table, TableBody, TableHead, TableRow, Td, Th } from "@/components/ui/Table";
import { useToast } from "@/components/ui/Toast";
import { Rupiah, formatRupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import type {
  APInvoicePaymentRow,
  APInvoiceView,
  Project,
  Vendor,
} from "@/lib/types/api";
import {
  AP_STATUS_LABEL,
  apStatusHint,
  apStatusVariant,
  type APScope,
} from "@/components/ap/apRules";
import {
  AP_PAYMENT_STATUS_LABEL,
  apPaymentStatusHint,
  apPaymentStatusVariant,
  eligibleForPayment,
} from "@/components/ap/paymentRules";
import { APInvoicePaymentHistory } from "@/components/ap/APInvoicePaymentHistory";
import {
  postInvoiceAction,
  reverseInvoiceAction,
} from "@/app/(app)/accounting/hutang/actions";

interface Props {
  invoice: APInvoiceView;
  vendor?: Vendor;
  project?: Project;
  canWrite: boolean;
  today: string;
  paymentRows: APInvoicePaymentRow[];
  paymentRowsError?: string;
}

export function APInvoiceDetail({
  invoice,
  vendor,
  project,
  canWrite,
  today,
  paymentRows,
  paymentRowsError,
}: Props) {
  const router = useRouter();
  const { toast } = useToast();

  const [confirmPost, setConfirmPost] = useState(false);
  const [confirmReverse, setConfirmReverse] = useState(false);
  const [reverseDate, setReverseDate] = useState(today);
  const [error, setError] = useState<string | null>(null);
  const [busy, startAction] = useTransition();

  const inv = invoice;

  // Tagihan tanpa proyek adalah overhead: bebannya masuk periode berjalan, bukan
  // realisasi RAB. Seluruh salinan di bawah bercabang dari sini.
  const scope: APScope = inv.project_id ? "proyek" : "overhead";

  // Boleh-tidaknya dibayar dijawab status dari server, bukan perbandingan angka
  // di layar — aturannya sama persis dengan yang menyaring antrean bayar.
  const bisaDibayar = eligibleForPayment(inv);

  function doPost() {
    startAction(async () => {
      setError(null);
      const res = await postInvoiceAction(inv.id);
      setConfirmPost(false);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      toast("Tagihan diposting — kewajiban vendor sudah diakui", "success");
      router.refresh();
    });
  }

  function doReverse() {
    startAction(async () => {
      setError(null);
      const res = await reverseInvoiceAction(inv.id, reverseDate || undefined);
      setConfirmReverse(false);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      toast("Tagihan dibalik lewat jurnal pembalik", "success");
      router.refresh();
    });
  }

  return (
    <div className="space-y-6">
      {error && (
        <div className="rounded-lg border border-danger/30 bg-danger-bg/50 p-3">
          <p className="text-sm font-medium text-danger">Ditolak backend</p>
          <p className="mt-0.5 text-sm text-text-primary">{error}</p>
        </div>
      )}

      {/* ── Ringkasan ── */}
      <Card>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-lg font-semibold text-text-primary">
                {inv.invoice_number}
              </h2>
              <Badge variant={apStatusVariant(inv.status)}>
                {AP_STATUS_LABEL[inv.status]}
              </Badge>
            </div>
            <p className="mt-1 max-w-prose text-xs text-text-secondary">
              {apStatusHint(inv.status, scope)}
            </p>
          </div>

          {canWrite && (
            <div className="flex gap-2">
              {inv.status === "draft" && (
                <Button onClick={() => setConfirmPost(true)} disabled={busy}>
                  Posting Tagihan
                </Button>
              )}
              {bisaDibayar && (
                <Link href={`/accounting/pembayaran-vendor/baru?invoice=${inv.id}`}>
                  <Button>Bayar Tagihan</Button>
                </Link>
              )}
              {inv.status === "posted" && (
                <Button
                  variant="danger"
                  onClick={() => setConfirmReverse(true)}
                  disabled={busy}
                >
                  Balikkan
                </Button>
              )}
            </div>
          )}
        </div>

        <dl className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Amount label="DPP (nilai pekerjaan)" value={inv.dpp_amount} />
          <Amount label="PPN Masukan" value={inv.ppn_amount} />
          <Amount label="Total Hutang Vendor" value={inv.payable_amount} strong />
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
              Jatuh Tempo
            </dt>
            <dd className="mt-0.5 text-sm text-text-primary">
              <Tanggal value={inv.due_date} />
            </dd>
          </div>
        </dl>
      </Card>

      {/* ── Posisi pembayaran ── */}
      <Card>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle>Posisi Pembayaran</CardTitle>
            <p className="mt-0.5 max-w-prose text-xs text-text-secondary">
              {apPaymentStatusHint(inv.payment_status)}
            </p>
          </div>
          <Badge variant={apPaymentStatusVariant(inv.payment_status)}>
            {AP_PAYMENT_STATUS_LABEL[inv.payment_status]}
          </Badge>
        </div>

        <dl className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Amount label="Total Hutang" value={inv.payable_amount} />
          <Amount label="Sudah Dibayar" value={inv.paid_amount} />
          <Amount label="Sisa" value={inv.outstanding} strong />
        </dl>

        <p className="mt-3 text-[11px] text-text-tertiary">
          Ketiganya dihitung backend dari alokasi pembayaran setiap kali tagihan ini
          dibaca. Tidak ada kolom &quot;terbayar&quot; yang disimpan dan bisa menyimpang
          dari jurnalnya.
        </p>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* ── Vendor ── */}
        <Card>
          <CardHeader>
            <CardTitle>Vendor</CardTitle>
          </CardHeader>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field
              label="Nama"
              value={inv.vendor_name || vendor?.name || `#${inv.vendor_id}`}
            />
            <Field label="Status pajak" value={inv.vendor_is_pkp ? "PKP" : "Non-PKP"} />
            <Field label="NPWP" value={vendor?.npwp || "—"} mono />
            <Field label="Kontak" value={vendor?.phone || vendor?.email || "—"} />
            <Field label="Bank" value={vendor?.bank_name || "—"} />
            <Field label="No. rekening" value={vendor?.bank_account || "—"} mono />
          </dl>
          <div className="mt-3">
            <Link
              href="/accounting/vendor"
              className="text-xs text-accent hover:underline"
            >
              Lihat di Master Vendor →
            </Link>
          </div>
        </Card>

        {/* ── Tagihan ── */}
        <Card>
          <CardHeader>
            <CardTitle>Informasi Tagihan</CardTitle>
          </CardHeader>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nomor tagihan vendor" value={inv.invoice_number} mono />
            <Field label="Uraian" value={inv.description || "—"} />
            <div>
              <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
                Tanggal tagihan
              </dt>
              <dd className="mt-0.5 text-sm text-text-primary">
                <Tanggal value={inv.invoice_date} />
              </dd>
            </div>
            <div>
              <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
                Jatuh tempo
              </dt>
              <dd className="mt-0.5 text-sm text-text-primary">
                <Tanggal value={inv.due_date} />
              </dd>
            </div>
            <Field
              label="Proyek"
              value={
                inv.project_id
                  ? project?.name ?? `#${inv.project_id}`
                  : "Overhead (tanpa proyek)"
              }
            />
            <Field label="No. faktur pajak" value={inv.faktur_pajak_number || "—"} mono />
            {inv.posted_at && (
              <div>
                <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
                  Diposting
                </dt>
                <dd className="mt-0.5 text-sm text-text-primary">
                  <Tanggal value={inv.posted_at} />
                </dd>
              </div>
            )}
          </dl>
        </Card>
      </div>

      {/* ── Baris biaya ── */}
      <Card padding="none">
        <div className="border-b border-border p-4">
          <p className="text-sm font-semibold text-text-primary">Baris Biaya</p>
          <p className="mt-0.5 text-xs text-text-secondary">
            {inv.status !== "posted"
              ? "Baris ini belum menjadi realisasi apa pun; ia baru terbaca setelah tagihan diposting."
              : scope === "overhead"
                ? "Baris ini menjadi beban periode berjalan — tidak menambah realisasi RAB dan tidak masuk HPP. PPN Masukan tidak ikut — ia aset, bukan biaya."
                : "Baris ini terbaca sebagai realisasi RAB proyek. PPN Masukan tidak ikut — ia aset, bukan biaya."}
          </p>
        </div>
        {!inv.lines || inv.lines.length === 0 ? (
          <p className="p-4 text-sm text-text-secondary">Tidak ada baris biaya.</p>
        ) : (
          <Table>
            <TableHead>
              <tr>
                <Th>Kategori</Th>
                <Th>Pembebanan</Th>
                <Th>Unit</Th>
                <Th>Item RAB</Th>
                <Th>Uraian</Th>
                <Th right>Nominal</Th>
              </tr>
            </TableHead>
            <TableBody>
              {inv.lines.map((ln) => (
                <TableRow key={ln.id}>
                  <Td>{ln.category}</Td>
                  <Td>{ln.cost_tier}</Td>
                  <Td>{ln.unit_id ? `#${ln.unit_id}` : "—"}</Td>
                  <Td>{ln.budget_item_id ? `#${ln.budget_item_id}` : "—"}</Td>
                  <Td>{ln.description}</Td>
                  <Td right mono>
                    <Rupiah value={ln.amount} colorSign={false} />
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <div className="flex items-baseline justify-between border-t border-border px-4 py-3">
          <span className="text-xs text-text-secondary">
            DPP tagihan (jumlah seluruh baris biaya, ditetapkan backend)
          </span>
          <span className="font-mono text-sm font-semibold text-text-primary">
            {formatRupiah(inv.dpp_amount)}
          </span>
        </div>
      </Card>

      {/* ── Riwayat pembayaran ── */}
      <APInvoicePaymentHistory
        rows={paymentRows}
        invoiceStatus={inv.status}
        loadError={paymentRowsError}
      />

      {/* ── Jejak jurnal ── */}
      <Card>
        <CardHeader>
          <CardTitle>Jejak Jurnal</CardTitle>
        </CardHeader>
        <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
              Jurnal pengakuan
            </dt>
            <dd className="mt-0.5 text-sm">
              {inv.status === "draft" ? (
                <span className="text-text-secondary">
                  Masih draft — jurnalnya belum diposting, jadi belum terbaca laporan
                  mana pun.
                </span>
              ) : (
                <Link
                  href={`/accounting/jurnal/${inv.journal_entry_id}`}
                  className="text-accent hover:underline"
                >
                  Jurnal #{inv.journal_entry_id}
                </Link>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
              Jurnal pembalik
            </dt>
            <dd className="mt-0.5 text-sm">
              {inv.reversal_journal_entry_id ? (
                <Link
                  href={`/accounting/jurnal/${inv.reversal_journal_entry_id}`}
                  className="text-accent hover:underline"
                >
                  Jurnal #{inv.reversal_journal_entry_id}
                </Link>
              ) : (
                <span className="text-text-tertiary">—</span>
              )}
            </dd>
          </div>
        </dl>
      </Card>

      {/* ── Konfirmasi ── */}
      <ConfirmModal
        open={confirmPost}
        title="Posting tagihan vendor?"
        confirmLabel="Ya, akui kewajiban"
        loading={busy}
        onCancel={() => setConfirmPost(false)}
        onConfirm={doPost}
      >
        <p>
          Saldo Hutang Usaha akan bertambah{" "}
          <strong>{formatRupiah(inv.payable_amount)}</strong>
          {scope === "overhead"
            ? " dan biayanya menjadi beban periode berjalan — tidak menambah realisasi RAB."
            : " dan biayanya mulai terbaca sebagai realisasi RAB."}
        </p>
        <p className="mt-2">
          Jurnal yang sudah diposting tidak bisa diubah maupun dihapus — koreksinya
          hanya lewat pembalikan.
        </p>
      </ConfirmModal>

      <ConfirmModal
        open={confirmReverse}
        title="Balikkan tagihan ini?"
        confirmLabel="Ya, balikkan"
        variant="danger"
        loading={busy}
        onCancel={() => setConfirmReverse(false)}
        onConfirm={doReverse}
      >
        <p>
          Sebuah jurnal pembalik akan terbit. Tagihan aslinya tidak dihapus dan tidak
          diedit — keduanya tetap terlihat di buku sebagai jejak.
        </p>
        <div className="mt-3">
          <Input
            label="Tanggal pembalikan"
            type="date"
            value={reverseDate}
            onChange={(e) => setReverseDate(e.target.value)}
            hint="Kosongkan untuk memakai tanggal hari ini menurut server."
          />
        </div>
      </ConfirmModal>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">{label}</dt>
      <dd className={`mt-0.5 text-sm text-text-primary ${mono ? "font-mono" : ""}`}>
        {value}
      </dd>
    </div>
  );
}

function Amount({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">{label}</dt>
      <dd
        className={`mt-0.5 font-mono text-sm ${
          strong ? "font-bold text-text-primary" : "text-text-primary"
        }`}
      >
        <Rupiah value={value} colorSign={false} />
      </dd>
    </div>
  );
}
