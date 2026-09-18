"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Card } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { useToast } from "@/components/ui/Toast";
import { LegacyARPaymentModal } from "@/components/accounting/LegacyARPaymentModal";
import { DocumentNumberLink } from "@/components/documents/DocumentNumberLink";
import { ApiError } from "@/lib/api/client";
import { fetchLegacyReceivable, voidLegacyPayment } from "@/lib/api/legacyar";
import type {
  LegacyAuditEvent,
  LegacyDetail as Detail,
  LegacyPayment,
  LegacyReceivableView,
} from "@/lib/api/legacyar";

// Rincian satu piutang proyek lama.
//
// Yang paling penting di halaman ini bukan saldonya, melainkan RIWAYATNYA:
// dari berkas mana angka ini datang, siapa yang mengimpor, dan setiap
// pelunasan beserta nomor bukti kasnya. Piutang tanpa dokumen di baliknya
// adalah angka yang tidak bisa dipertahankan saat ditagih atau diaudit.

const EVENT_LABEL: Record<LegacyAuditEvent, string> = {
  imported: "Diimpor",
  paid: "Pembayaran",
  payment_void: "Pembayaran dibatalkan",
  customer_linked: "Ditautkan ke customer",
};

const EVENT_VARIANT: Record<LegacyAuditEvent, "neutral" | "success" | "danger" | "accent"> = {
  imported: "neutral",
  paid: "success",
  payment_void: "danger",
  customer_linked: "accent",
};

function isVoidLine(p: LegacyPayment): boolean {
  return p.voids_payment_id != null || parseFloat(p.amount) < 0;
}

export function LegacyARDetail({ token, initial }: { token: string; initial: Detail }) {
  const router = useRouter();
  const { toast } = useToast();
  const [detail, setDetail] = useState(initial);
  const [payOpen, setPayOpen] = useState(false);
  const [voidTarget, setVoidTarget] = useState<LegacyPayment | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);

  const r = detail.receivable;
  const voided = new Set(
    detail.payments.filter((p) => p.voids_payment_id != null).map((p) => p.voids_payment_id!),
  );

  async function reload() {
    const d = await fetchLegacyReceivable(token, r.id);
    setDetail(d);
    router.refresh();
  }

  async function doVoid() {
    if (!voidTarget) return;
    setBusy(true);
    try {
      await voidLegacyPayment(token, voidTarget.id, reason);
      toast("Pembayaran dibatalkan dengan jurnal pembalik.", "success");
      setVoidTarget(null);
      setReason("");
      await reload();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : "Pembatalan gagal.", "error");
    } finally {
      setBusy(false);
    }
  }

  // Modal pembayaran bekerja dengan bentuk baris daftar; detail dipetakan ke
  // bentuk itu supaya hanya ada SATU jalur pembayaran di seluruh aplikasi.
  const asRow: LegacyReceivableView = {
    ...r,
    outstanding: detail.outstanding,
    payment_count: detail.payments.length,
  };

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="text-lg font-semibold text-text-primary">{r.customer_name}</h2>
              <p className="text-sm text-text-secondary">
                {r.source_label}
                {r.external_ref ? ` · ${r.external_ref}` : ""}
              </p>
            </div>
            <Badge
              variant={
                r.status === "paid" ? "success" : r.status === "open" ? "warning" : "neutral"
              }
            >
              {r.status === "paid" ? "Lunas" : r.status === "open" ? "Belum Lunas" : "Dihapusbukukan"}
            </Badge>
          </div>

          <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
            <Field label="Nilai asli" money={r.original_amount} />
            <Field label="Sudah dibayar" money={r.paid_amount} />
            <Field label="Sisa tagihan" money={detail.outstanding} strong />
            <Field label="Posisi per" date={r.as_of_date} />
            {r.due_date ? (
              <Field label="Jatuh tempo" date={r.due_date} />
            ) : (
              <Field label="Jatuh tempo" text="—" />
            )}
            <Field label="Akun kontrol" text={r.control_account_code} mono />
            {r.phone && <Field label="Telepon" text={r.phone} />}
            {r.email && <Field label="Email" text={r.email} />}
            {r.notes && <Field label="Catatan" text={r.notes} />}
          </dl>

          {r.status === "open" && (
            <div className="mt-5">
              <Button onClick={() => setPayOpen(true)}>Terima Pembayaran</Button>
            </div>
          )}
        </Card>

        <Card>
          <h3 className="text-sm font-semibold text-text-primary">Asal Data</h3>
          {detail.batch ? (
            <dl className="mt-3 space-y-2 text-sm">
              <Field label="Berkas" text={detail.batch.file_name} />
              <Field label="Diimpor" date={detail.batch.committed_at ?? detail.batch.created_at} />
              <Field label="Posisi" date={detail.batch.as_of_date} />
              <Field
                label="Jurnal saat impor"
                text={detail.batch.opening_journal_id ? `#${detail.batch.opening_journal_id} (saldo awal)` : "Tidak ada"}
              />
              {detail.batch.skip_reason && (
                <Field label="Alasan selisih" text={detail.batch.skip_reason} />
              )}
            </dl>
          ) : (
            <p className="mt-2 text-sm text-text-tertiary">Batch asal tidak ditemukan.</p>
          )}
          <p className="mt-4 text-xs text-text-tertiary">
            Impor tidak membuat jurnal piutang — saldonya sudah ada di buku besar sebagai saldo
            awal. Yang berjurnal hanya pelunasan di bawah.
          </p>
        </Card>
      </div>

      <Card padding="none">
        <div className="border-b border-border p-4">
          <h3 className="text-sm font-semibold text-text-primary">Riwayat Pembayaran</h3>
        </div>
        {detail.payments.length === 0 ? (
          <EmptyState
            title="Belum ada pembayaran"
            description="Setiap pembayaran menurunkan sisa tagihan dan saldo piutang di buku besar, serta menerbitkan bukti kas bernomor."
          />
        ) : (
          <Table>
            <TableHead>
              <tr>
                <Th>Tanggal</Th>
                <Th>Bukti</Th>
                <Th>Kas / Bank</Th>
                <Th>Jurnal</Th>
                <Th>Kwitansi</Th>
                <Th right>Jumlah</Th>
                <Th>Keterangan</Th>
                <Th>Aksi</Th>
              </tr>
            </TableHead>
            <TableBody>
              {detail.payments.map((p) => {
                const isVoid = isVoidLine(p);
                const wasVoided = voided.has(p.id);
                return (
                  <TableRow key={p.id} subtle={isVoid || wasVoided}>
                    <Td>
                      <Tanggal value={p.payment_date} />
                    </Td>
                    <Td mono>{p.document_number ?? "—"}</Td>
                    <Td mono>{p.cash_account_code}</Td>
                    <Td mono>
                      <Link
                        href={`/accounting/jurnal/${p.journal_entry_id}`}
                        className="text-accent hover:underline"
                      >
                        #{p.journal_entry_id}
                      </Link>
                    </Td>
                    <Td mono>
                      {p.receipt_number ? (
                        <DocumentNumberLink token={token} number={p.receipt_number} />
                      ) : (
                        "—"
                      )}
                    </Td>
                    <Td right>
                      <Rupiah value={p.amount} />
                    </Td>
                    <Td>
                      {isVoid ? (
                        <Badge variant="danger">Pembalik</Badge>
                      ) : wasVoided ? (
                        <Badge variant="neutral">Dibatalkan</Badge>
                      ) : (
                        p.notes || "—"
                      )}
                    </Td>
                    <Td>
                      {!isVoid && !wasVoided ? (
                        <button
                          type="button"
                          onClick={() => setVoidTarget(p)}
                          className="text-danger hover:underline"
                        >
                          Batalkan
                        </button>
                      ) : (
                        "—"
                      )}
                    </Td>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </Card>

      <Card padding="none">
        <div className="border-b border-border p-4">
          <h3 className="text-sm font-semibold text-text-primary">Jejak Audit</h3>
        </div>
        <Table>
          <TableHead>
            <tr>
              <Th>Waktu</Th>
              <Th>Kejadian</Th>
              <Th right>Jumlah</Th>
              <Th right>Sisa Setelahnya</Th>
              <Th>Keterangan</Th>
            </tr>
          </TableHead>
          <TableBody>
            {detail.audits.map((a) => (
              <TableRow key={a.id}>
                <Td>
                  <Tanggal value={a.created_at} />
                </Td>
                <Td>
                  <Badge variant={EVENT_VARIANT[a.event] ?? "neutral"}>
                    {EVENT_LABEL[a.event] ?? a.event}
                  </Badge>
                </Td>
                <Td right>
                  <Rupiah value={a.amount} />
                </Td>
                <Td right>
                  <Rupiah value={a.outstanding_after} colorSign={false} />
                </Td>
                <Td>{a.detail || "—"}</Td>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>

      <LegacyARPaymentModal
        token={token}
        open={payOpen}
        onClose={() => setPayOpen(false)}
        targets={[asRow]}
        onDone={reload}
      />

      <Modal
        open={voidTarget !== null}
        onClose={() => setVoidTarget(null)}
        title="Batalkan Pembayaran"
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setVoidTarget(null)} disabled={busy}>
              Batal
            </Button>
            <Button
              variant="danger"
              onClick={doVoid}
              loading={busy}
              disabled={busy || reason.trim().length < 5}
            >
              Batalkan Pembayaran
            </Button>
          </>
        }
      >
        <p className="text-sm text-text-secondary">
          Pembayaran tidak dihapus. Sistem membuat jurnal pembalik dan sisa tagihan kembali naik —
          riwayatnya tetap utuh supaya bisa dijelaskan saat diaudit.
        </p>
        <div className="mt-3">
          <Input
            label="Alasan pembatalan"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="mis. salah alokasi, uang dikembalikan"
          />
        </div>
      </Modal>
    </div>
  );
}

function Field({
  label,
  text,
  money,
  date,
  mono,
  strong,
}: {
  label: string;
  text?: string;
  money?: string;
  // Tanggal dari API datang sebagai stempel waktu penuh ("2025-12-31T00:00:00
  // +08:00"). Yang dibaca admin harus tanggal, bukan stempelnya.
  date?: string;
  mono?: boolean;
  strong?: boolean;
}) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-text-secondary">{label}</dt>
      <dd
        className={`mt-0.5 ${mono ? "font-mono text-xs" : "text-sm"} ${
          strong ? "font-semibold text-text-primary" : "text-text-primary"
        }`}
      >
        {money !== undefined ? (
          <Rupiah value={money} colorSign={false} />
        ) : date !== undefined ? (
          <Tanggal value={date} />
        ) : (
          text
        )}
      </dd>
    </div>
  );
}
