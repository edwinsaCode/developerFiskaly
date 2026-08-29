"use client";

// Detail pembayaran hutang vendor (W-11 Tahap 5).
//
// Satu aksi saja: Balikkan — dan hanya untuk pembayaran yang belum dibalik.
// Tidak ada tombol Ubah dan tidak ada Hapus: kas yang sudah keluar dan BKK yang
// sudah bernomor tidak bisa ditarik kembali dari buku. Koreksinya jurnal
// pembalik, dan keduanya tetap terlihat.
//
// `is_reversed` yang menentukan tombol itu tampil datang dari server. Layar
// tidak menyimpulkannya sendiri dari ada-tidaknya `reversed_at` — dua sumber
// untuk satu keadaan adalah cara tombol berbahaya muncul di saat yang salah.

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
import type { APPaymentView, Vendor } from "@/lib/types/api";
import { reversePaymentAction } from "@/app/(app)/accounting/pembayaran-vendor/actions";
import { PrintPaymentReceiptButton } from "@/components/ap/PrintPaymentReceiptButton";

interface Props {
  token: string;
  payment: APPaymentView;
  vendor?: Vendor;
  canWrite: boolean;
  today: string;
}

export function APPaymentDetail({ token, payment, vendor, canWrite, today }: Props) {
  const router = useRouter();
  const { toast } = useToast();

  const [confirmReverse, setConfirmReverse] = useState(false);
  const [reverseDate, setReverseDate] = useState(today);
  const [error, setError] = useState<string | null>(null);
  const [busy, startAction] = useTransition();

  const p = payment;
  const allocations = p.allocations ?? [];

  function doReverse() {
    startAction(async () => {
      setError(null);
      const res = await reversePaymentAction(p.id, reverseDate || undefined);
      setConfirmReverse(false);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      toast("Pembayaran dibalik — hutang vendor kembali berdiri", "success");
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
              <h2 className="font-mono text-lg font-semibold text-text-primary">
                {p.document_number}
              </h2>
              {p.is_reversed ? (
                <Badge variant="danger">Dibalik</Badge>
              ) : (
                <Badge variant="success">Berlaku</Badge>
              )}
            </div>
            <p className="mt-1 max-w-prose text-xs text-text-secondary">
              {p.is_reversed
                ? "Pembayaran ini sudah dibalik lewat jurnal pembalik. Uangnya kembali ke kas menurut buku dan hutang vendornya berdiri lagi — tetapi baris ini tetap ada, karena BKK-nya pernah terbit."
                : "Bukti kas keluar untuk pembayaran hutang vendor. Uang sudah keluar dari kas/bank dan hutang usaha berkurang sebesar alokasinya."}
            </p>
          </div>

          <div className="flex items-center gap-2">
            <PrintPaymentReceiptButton token={token} paymentId={p.id} />
            {canWrite && !p.is_reversed && (
              <Button variant="danger" onClick={() => setConfirmReverse(true)} disabled={busy}>
                Balikkan Pembayaran
              </Button>
            )}
          </div>
        </div>

        <dl className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Amount label="Jumlah bayar" value={p.amount} strong />
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
              Tanggal bayar
            </dt>
            <dd className="mt-0.5 text-sm text-text-primary">
              <Tanggal value={p.payment_date} />
            </dd>
          </div>
          <Field label="Sumber dana" value={p.cash_account_code} mono />
          <Field label="Vendor" value={p.vendor_name || vendor?.name || `#${p.vendor_id}`} />
        </dl>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* ── Vendor ── */}
        <Card>
          <CardHeader>
            <CardTitle>Vendor Penerima</CardTitle>
          </CardHeader>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nama" value={p.vendor_name || vendor?.name || `#${p.vendor_id}`} />
            <Field label="Status pajak" value={vendor ? (vendor.is_pkp ? "PKP" : "Non-PKP") : "—"} />
            <Field label="Bank" value={vendor?.bank_name || "—"} />
            <Field label="No. rekening" value={vendor?.bank_account || "—"} mono />
          </dl>
        </Card>

        {/* ── Dokumen ── */}
        <Card>
          <CardHeader>
            <CardTitle>Dokumen & Jurnal</CardTitle>
          </CardHeader>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Nomor BKK" value={p.document_number} mono />
            <Field label="Uraian" value={p.description || "—"} />
            <div>
              <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
                Jurnal pengeluaran
              </dt>
              <dd className="mt-0.5 text-sm">
                <Link
                  href={`/accounting/jurnal/${p.journal_entry_id}`}
                  className="text-accent hover:underline"
                >
                  Jurnal #{p.journal_entry_id}
                </Link>
              </dd>
            </div>
            <div>
              <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
                Dibalik pada
              </dt>
              <dd className="mt-0.5 text-sm text-text-primary">
                {p.reversed_at ? (
                  <Tanggal value={p.reversed_at} />
                ) : (
                  <span className="text-text-tertiary">—</span>
                )}
              </dd>
            </div>
          </dl>
          <p className="mt-3 text-[11px] text-text-tertiary">
            Satu pengeluaran kas = satu dokumen bernomor (INV-DOC-1). Nomor di atas
            terbit dalam transaksi yang sama dengan jurnalnya, jadi tidak ada
            pengeluaran tanpa bukti dan tidak ada bukti tanpa pengeluaran.
          </p>
        </Card>
      </div>

      {/* ── Alokasi ── */}
      <Card padding="none">
        <div className="border-b border-border p-4">
          <p className="text-sm font-semibold text-text-primary">Alokasi ke Tagihan</p>
          <p className="mt-0.5 text-xs text-text-secondary">
            Inilah yang membuat sisa tagihan berkurang. Tidak ada kolom &quot;terbayar&quot;
            yang ditulis ulang di tagihan — sisanya dihitung dari baris-baris ini setiap
            kali tagihan dibaca.
          </p>
          {/* Sisa sebelum/sesudah sengaja TIDAK ditampilkan di layar ini. Backend
              hanya mengetahuinya saat pembayaran dihitung (pratinjau & hasil
              posting); untuk pembayaran yang sudah tersimpan ia tidak
              merekonstruksi posisi historis. Mencetak nilai kosong sebagai
              "Rp 0" akan membuat layar mengarang angka — persis yang dilarang.
              Posisi terkini tagihan dibaca di detail tagihannya. */}
        </div>
        {allocations.length === 0 ? (
          <p className="p-4 text-sm text-text-secondary">
            Pembayaran ini tidak punya alokasi tagihan.
          </p>
        ) : (
          <>
            <Table>
              <TableHead>
                <tr>
                  <Th>Nomor Tagihan</Th>
                  <Th>Jatuh Tempo</Th>
                  <Th right>Dibayar</Th>
                </tr>
              </TableHead>
              <TableBody>
                {allocations.map((a) => (
                  <TableRow key={a.invoice_id}>
                    <Td>
                      <Link
                        href={`/accounting/hutang/${a.invoice_id}`}
                        className="font-medium text-accent hover:underline"
                      >
                        {a.invoice_number}
                      </Link>
                    </Td>
                    <Td>
                      <Tanggal value={a.due_date} />
                    </Td>
                    <Td right mono>
                      <Rupiah value={a.amount} colorSign={false} />
                    </Td>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <div className="flex items-baseline justify-between border-t border-border px-4 py-3">
              <span className="text-xs text-text-secondary">
                Jumlah pembayaran (ditetapkan backend)
              </span>
              <span className="font-mono text-sm font-semibold text-text-primary">
                {formatRupiah(p.amount)}
              </span>
            </div>
            <p className="border-t border-border px-4 py-3 text-xs text-text-secondary">
              Sisa tagihan sesudah pembayaran ini tidak dicetak di sini — posisi tagihan
              bergerak setiap kali ada pembayaran atau pembalikan lain. Bukalah nomor
              tagihannya untuk membaca sisa yang berlaku sekarang.
            </p>
          </>
        )}
      </Card>

      {/* ── Konfirmasi pembalikan ── */}
      <ConfirmModal
        open={confirmReverse}
        title="Balikkan pembayaran ini?"
        confirmLabel="Ya, balikkan"
        variant="danger"
        loading={busy}
        onCancel={() => setConfirmReverse(false)}
        onConfirm={doReverse}
      >
        <p>Setelah dibalik, tiga hal terjadi sekaligus:</p>
        <ul className="mt-2 list-disc space-y-1 pl-5">
          <li>
            Kas/bank <strong>{p.cash_account_code}</strong> bertambah kembali{" "}
            <strong>{formatRupiah(p.amount)}</strong> lewat jurnal pembalik.
          </li>
          <li>
            Hutang ke <strong>{p.vendor_name || `#${p.vendor_id}`}</strong> berdiri lagi:
            seluruh alokasi di atas dilepas, dan tagihan yang tadinya lunas kembali
            bersisa.
          </li>
          <li>
            BKK <strong>{p.document_number}</strong> tetap ada dan tetap terbaca — nomornya
            tidak didaur ulang, dan pembayaran ini tidak bisa dibalik dua kali.
          </li>
        </ul>
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
