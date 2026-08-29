"use client";

// Form pembayaran hutang vendor (W-11 Tahap 5).
//
// Dua langkah, dan langkah keduanya bukan hiasan:
//
//   Isi → Pratinjau → Simpan
//
// Pratinjau memanggil /ap/payments/preview, yang menjalankan SELURUH validasi,
// penguncian, alokasi, dan komposisi jurnal yang dijalankan pencatatan — lalu
// me-rollback. Angka yang terbaca di layar pratinjau karena itu bukan tiruan
// yang "seharusnya" setara: ia hasil jalur yang sama.
//
// Tidak ada satu pun angka uang di file ini yang dihitung sendiri. Sisa tagihan,
// alokasi, sisa sesudah bayar, dan baris jurnal seluruhnya dari server. Yang
// dijumlahkan layar hanya Σ alokasi yang sedang diketik operator — alat bantu
// isi, dan backend tetap yang menolak kalau tidak pas.
//
// Tidak ada tombol Uang Muka, Retensi, atau Pelepasan Retensi: backend menolak
// ketiganya pada tahap ini, dan tombol yang pasti gagal lebih buruk daripada
// tombol yang tidak ada.

import Link from "next/link";
import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { EmptyState } from "@/components/ui/EmptyState";
import { Input, Select, Textarea } from "@/components/ui/Input";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { Table, TableBody, TableHead, TableRow, Td, Th } from "@/components/ui/Table";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { Rupiah, formatRupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import type {
  APInvoiceView,
  APPaymentPreview,
  APPaymentResult,
  Vendor,
} from "@/lib/types/api";
import {
  AP_PAYMENT_STATUS_LABEL,
  allocationTotal,
  apPaymentStatusVariant,
  buildPaymentBody,
  eligibleInvoicesOf,
  emptyPaymentForm,
  hasPaymentErrors,
  newIdempotencyKey,
  validatePayment,
  type OverpaymentInfo,
  type PaymentErrors,
  type PaymentFormState,
} from "@/components/ap/paymentRules";
import {
  previewPaymentAction,
  recordPaymentAction,
} from "@/app/(app)/accounting/pembayaran-vendor/actions";

interface Props {
  token: string;
  canWrite: boolean;
  vendors: Vendor[];
  invoices: APInvoiceView[];
  today: string;
  initialVendorId?: string;
  initialInvoiceId?: string;
}

const NO_ERRORS: PaymentErrors = { fields: {}, allocations: {} };

export function APPaymentForm({
  token,
  canWrite,
  vendors,
  invoices,
  today,
  initialVendorId = "",
  initialInvoiceId = "",
}: Props) {
  const router = useRouter();

  const [form, setForm] = useState<PaymentFormState>(() => {
    const f = emptyPaymentForm(today, initialVendorId);
    // Datang dari tombol "Bayar" di detail tagihan: tagihan itu langsung
    // terpilih dalam mode eksplisit, tetapi nominalnya TIDAK diisi otomatis —
    // berapa yang dibayar adalah keputusan kasir, bukan bawaan layar.
    if (initialInvoiceId) {
      f.mode = "explicit";
      f.allocations = { [initialInvoiceId]: "" };
    }
    return f;
  });
  const [errors, setErrors] = useState<PaymentErrors>(NO_ERRORS);
  const [preview, setPreview] = useState<APPaymentPreview | null>(null);
  const [result, setResult] = useState<APPaymentResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [overpay, setOverpay] = useState<OverpaymentInfo | null>(null);
  const [confirm, setConfirm] = useState(false);
  // Kunci idempotensi lahir bersama pratinjau dan BERTAHAN selama pratinjau itu
  // masih berlaku. Percobaan simpan yang gagal karena jaringan tidak boleh
  // mengganti kunci — kalau diganti, permintaan pertama yang ternyata sempat
  // sampai akan menjadi pengeluaran kas kedua.
  const [idemKey, setIdemKey] = useState("");
  const [previewing, startPreview] = useTransition();
  const [saving, startSave] = useTransition();

  const eligible = useMemo(
    () => eligibleInvoicesOf(invoices, form.vendor_id),
    [invoices, form.vendor_id],
  );

  const activeVendors = vendors.filter((v) => v.is_active);
  // Vendor yang punya antrean bayar dipisahkan supaya operator tidak memilih
  // satu per satu untuk menemukan mana yang sebenarnya punya tagihan terbuka.
  const vendorsWithQueue = useMemo(() => {
    const ids = new Set(eligibleInvoicesOf(invoices, "").map((inv) => inv.vendor_id));
    return activeVendors.filter((v) => ids.has(v.id));
  }, [invoices, activeVendors]);

  function set<K extends keyof PaymentFormState>(k: K, v: PaymentFormState[K]) {
    // Setiap perubahan input membatalkan pratinjau: pratinjau yang tidak lagi
    // menggambarkan isi form adalah pratinjau yang menyesatkan.
    setPreview(null);
    setIdemKey("");
    setOverpay(null);
    setForm((f) => ({ ...f, [k]: v }));
  }

  function setAlloc(invoiceId: number, value: string) {
    setPreview(null);
    setIdemKey("");
    setOverpay(null);
    setForm((f) => ({
      ...f,
      allocations: { ...f.allocations, [String(invoiceId)]: value },
    }));
  }

  function switchVendor(vendorId: string) {
    setPreview(null);
    setIdemKey("");
    setOverpay(null);
    setErrors(NO_ERRORS);
    // Alokasi dikosongkan, bukan dibiarkan: tagihan milik vendor lama tidak
    // boleh ikut terkirim ke pembayaran vendor baru — backend menolaknya
    // (ErrInvoiceWrongVendor), dan lebih baik tidak sampai ke sana.
    setForm((f) => ({ ...f, vendor_id: vendorId, allocations: {} }));
  }

  function doPreview() {
    const e = validatePayment(form, eligible);
    setErrors(e);
    if (hasPaymentErrors(e)) return;

    setError(null);
    setOverpay(null);
    startPreview(async () => {
      const res = await previewPaymentAction(buildPaymentBody(form));
      if (!res.ok) {
        setPreview(null);
        setError(res.error);
        setOverpay(res.overpayment ?? null);
        return;
      }
      setPreview(res.data);
      setIdemKey(newIdempotencyKey());
    });
  }

  function doSave() {
    startSave(async () => {
      setError(null);
      setOverpay(null);
      const res = await recordPaymentAction(buildPaymentBody(form), idemKey);
      setConfirm(false);
      if (!res.ok) {
        setError(res.error);
        setOverpay(res.overpayment ?? null);
        return;
      }
      setResult(res.data);
      setPreview(null);
      setForm(emptyPaymentForm(today));
      setErrors(NO_ERRORS);
      setIdemKey("");
      router.refresh();
    });
  }

  if (!canWrite) {
    return (
      <Card>
        <p className="text-sm text-text-secondary">
          Peran Anda hanya bisa melihat. Pembayaran hutang vendor mengeluarkan uang
          dari kas perusahaan dan memerlukan peran Pemilik atau Accounting.
        </p>
      </Card>
    );
  }

  // ── Hasil posting ──
  if (result) {
    return (
      <ResultPanel
        result={result}
        onAgain={() => {
          setResult(null);
          setForm(emptyPaymentForm(today));
        }}
      />
    );
  }

  return (
    <div className="space-y-6">
      {error && (
        <Card className="border-danger/30 bg-danger-bg/50">
          <p className="text-sm font-medium text-danger">Ditolak backend</p>
          <p className="mt-0.5 text-sm text-text-primary">{error}</p>
          {overpay && (
            <dl className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
              <Amount label="Sisa tagihan menurut buku" value={overpay.outstanding} />
              <Amount label="Yang diminta" value={overpay.requested} />
              <Amount label="Kelebihan" value={overpay.excess} strong />
              {overpay.invoice_number && (
                <div className="sm:col-span-3">
                  <p className="text-xs text-text-secondary">
                    Tagihan yang terlampaui: {overpay.invoice_number}
                  </p>
                </div>
              )}
              <div className="sm:col-span-3">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => set("amount", overpay.outstanding)}
                >
                  Ubah jumlah bayar menjadi {formatRupiah(overpay.outstanding)}
                </Button>
              </div>
            </dl>
          )}
        </Card>
      )}

      {/* ── Langkah 2: pratinjau ── */}
      {preview ? (
        <PreviewPanel
          preview={preview}
          busy={saving}
          onBack={() => {
            setPreview(null);
            setIdemKey("");
          }}
          onSave={() => setConfirm(true)}
        />
      ) : (
        /* ── Langkah 1: isi ── */
        <Card>
          <CardHeader>
            <CardTitle>Bayar Tagihan Vendor</CardTitle>
            <p className="mt-0.5 text-xs text-text-secondary">
              Uang keluar dari kas/bank perusahaan dan bukti kas keluar (BKK) terbit
              otomatis dalam transaksi yang sama. Sisa tagihan berkurang karena alokasi
              pembayaran bertambah — bukan karena ada kolom yang ditulis ulang.
            </p>
          </CardHeader>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Select
              label="Vendor"
              required
              value={form.vendor_id}
              onChange={(e) => switchVendor(e.target.value)}
              error={errors.fields.vendor_id}
            >
              <option value="">Pilih vendor…</option>
              {vendorsWithQueue.length > 0 && (
                <optgroup label="Punya tagihan terbuka">
                  {vendorsWithQueue.map((v) => (
                    <option key={v.id} value={String(v.id)}>
                      {v.name}
                    </option>
                  ))}
                </optgroup>
              )}
              {activeVendors.filter((v) => !vendorsWithQueue.includes(v)).length > 0 && (
                <optgroup label="Tanpa tagihan terbuka">
                  {activeVendors
                    .filter((v) => !vendorsWithQueue.includes(v))
                    .map((v) => (
                      <option key={v.id} value={String(v.id)}>
                        {v.name}
                      </option>
                    ))}
                </optgroup>
              )}
            </Select>

            <Input
              label="Tanggal bayar"
              type="date"
              required
              value={form.payment_date}
              onChange={(e) => set("payment_date", e.target.value)}
              error={errors.fields.payment_date}
            />

            <CashBankSelect
              token={token}
              label="Sumber dana"
              required
              value={form.cash_account_code}
              onChange={(code) => set("cash_account_code", code)}
            />

            <RupiahInput
              label="Jumlah bayar"
              required
              value={form.amount}
              onChange={(v) => set("amount", v)}
              error={errors.fields.amount}
              showErrorWhenEmpty
            />

            <Textarea
              label="Keterangan"
              wrapperClassName="md:col-span-2"
              rows={2}
              value={form.description}
              onChange={(e) => set("description", e.target.value)}
              placeholder="Mis. pembayaran termin 2 pekerjaan struktur"
            />
          </div>

          {errors.fields.cash_account_code && (
            <p className="mt-2 text-xs text-danger">{errors.fields.cash_account_code}</p>
          )}

          {/* ── Alokasi ── */}
          <div className="mt-6">
            <span className="text-sm font-medium text-text-primary">Alokasi ke tagihan</span>
            <div className="mt-2 flex flex-wrap gap-2">
              <ModeChip
                active={form.mode === "auto"}
                onClick={() => set("mode", "auto")}
                title="Otomatis"
                desc="Jatuh tempo paling tua dilunasi lebih dulu"
              />
              <ModeChip
                active={form.mode === "explicit"}
                onClick={() => set("mode", "explicit")}
                title="Pilih sendiri"
                desc="Tentukan nominal per tagihan; jumlahnya harus pas"
              />
            </div>
            {errors.fields.allocations && (
              <p className="mt-2 text-xs text-danger">{errors.fields.allocations}</p>
            )}
          </div>

          <div className="mt-4">
            <EligibleInvoiceTable
              vendorChosen={!!form.vendor_id}
              invoices={eligible}
              mode={form.mode}
              allocations={form.allocations}
              allocErrors={errors.allocations}
              onAlloc={setAlloc}
            />
          </div>

          {form.mode === "explicit" && eligible.length > 0 && (
            <div className="mt-3 flex items-baseline justify-between rounded-lg border border-border bg-border-subtle px-4 py-2.5">
              <span className="text-xs text-text-secondary">
                Jumlah alokasi yang sedang diisi (alat bantu — yang mengikat tetap
                perhitungan backend)
              </span>
              <span className="font-mono text-sm font-semibold text-text-primary">
                {formatRupiah(allocationTotal(form))}
              </span>
            </div>
          )}

          <div className="mt-6 flex justify-end gap-2">
            <Link href="/accounting/pembayaran-vendor">
              <Button variant="ghost">Batal</Button>
            </Link>
            <Button onClick={doPreview} loading={previewing} disabled={previewing}>
              Lihat Pratinjau
            </Button>
          </div>
        </Card>
      )}

      <ConfirmModal
        open={confirm}
        title="Simpan pembayaran ini?"
        confirmLabel="Ya, keluarkan uang"
        loading={saving}
        onCancel={() => setConfirm(false)}
        onConfirm={doSave}
      >
        <p>
          Uang sebesar <strong>{formatRupiah(preview?.amount ?? form.amount)}</strong> akan
          keluar dari <strong>{preview?.cash_account_name || form.cash_account_code}</strong>,
          dan bukti kas keluar (BKK) terbit dengan nomor berikutnya.
        </p>
        <p className="mt-2">
          Jurnalnya langsung diposting dan tidak bisa diubah maupun dihapus — koreksinya
          hanya lewat pembalikan.
        </p>
      </ConfirmModal>
    </div>
  );
}

// ── Daftar tagihan yang bisa dibayar ────────────────────────────────────────

function EligibleInvoiceTable({
  vendorChosen,
  invoices,
  mode,
  allocations,
  allocErrors,
  onAlloc,
}: {
  vendorChosen: boolean;
  invoices: APInvoiceView[];
  mode: "auto" | "explicit";
  allocations: Record<string, string>;
  allocErrors: Record<string, string>;
  onAlloc: (invoiceId: number, value: string) => void;
}) {
  if (!vendorChosen) {
    return (
      <EmptyState
        title="Pilih vendor lebih dulu"
        description="Antrean tagihan yang bisa dibayar muncul setelah vendornya dipilih. Satu pembayaran hanya untuk satu vendor."
      />
    );
  }

  if (invoices.length === 0) {
    return (
      <EmptyState
        title="Tidak ada tagihan yang bisa dibayar"
        description="Vendor ini tidak punya tagihan terposting yang masih bersisa. Tagihan draft belum menjadi kewajiban siapa pun, jadi ia tidak bisa dibayar; tagihan yang sudah lunas tidak muncul lagi di sini."
        action={
          <Link href="/accounting/hutang">
            <Button variant="secondary">Lihat tagihan vendor</Button>
          </Link>
        }
      />
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <Table>
        <TableHead>
          <tr>
            <Th>Nomor Tagihan</Th>
            <Th>Jatuh Tempo</Th>
            <Th right>Total Hutang</Th>
            <Th right>Sisa</Th>
            <Th>Status Bayar</Th>
            {mode === "explicit" && <Th right>Dibayar Sekarang</Th>}
          </tr>
        </TableHead>
        <TableBody>
          {invoices.map((inv) => (
            <TableRow key={inv.id}>
              <Td>
                <Link
                  href={`/accounting/hutang/${inv.id}`}
                  className="font-medium text-accent hover:underline"
                >
                  {inv.invoice_number}
                </Link>
              </Td>
              <Td>
                <Tanggal value={inv.due_date} />
              </Td>
              <Td right mono>
                <Rupiah value={inv.payable_amount} colorSign={false} />
              </Td>
              <Td right mono>
                <Rupiah value={inv.outstanding} colorSign={false} />
              </Td>
              <Td>
                <Badge variant={apPaymentStatusVariant(inv.payment_status)}>
                  {AP_PAYMENT_STATUS_LABEL[inv.payment_status]}
                </Badge>
              </Td>
              {mode === "explicit" && (
                <Td right>
                  <RupiahInput
                    value={allocations[String(inv.id)] ?? ""}
                    onChange={(v) => onAlloc(inv.id, v)}
                    error={allocErrors[String(inv.id)]}
                    wrapperClassName="w-44"
                  />
                </Td>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {mode === "auto" && (
        <p className="border-t border-border px-4 py-2.5 text-[11px] text-text-tertiary">
          Mode otomatis: backend membagi uang ke tagihan di atas mulai dari jatuh tempo
          paling tua. Pembagian persisnya terlihat di pratinjau sebelum apa pun tersimpan.
        </p>
      )}
    </div>
  );
}

// ── Pratinjau ───────────────────────────────────────────────────────────────

function PreviewPanel({
  preview,
  busy,
  onBack,
  onSave,
}: {
  preview: APPaymentPreview;
  busy: boolean;
  onBack: () => void;
  onSave: () => void;
}) {
  return (
    <Card>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <CardTitle>Pratinjau Pembayaran</CardTitle>
          <p className="mt-0.5 max-w-prose text-xs text-text-secondary">
            Belum ada apa pun yang tersimpan. Seluruh angka di bawah dihitung backend
            lewat jalur yang sama dengan pencatatan, lalu dibatalkan.
          </p>
        </div>
        <Button variant="ghost" onClick={onBack} disabled={busy}>
          ← Ubah
        </Button>
      </div>

      <dl className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Field label="Vendor" value={preview.vendor_name} />
        <div>
          <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
            Tanggal bayar
          </dt>
          <dd className="mt-0.5 text-sm text-text-primary">
            <Tanggal value={preview.payment_date} />
          </dd>
        </div>
        <Field
          label="Sumber dana"
          value={`${preview.cash_account_code} · ${preview.cash_account_name}`}
        />
        <Amount label="Jumlah bayar" value={preview.amount} strong />
      </dl>

      <div className="mt-6">
        <p className="text-sm font-semibold text-text-primary">Alokasi</p>
        <div className="mt-2 overflow-x-auto rounded-lg border border-border">
          <Table>
            <TableHead>
              <tr>
                <Th>Nomor Tagihan</Th>
                <Th>Jatuh Tempo</Th>
                <Th right>Sisa Sebelum</Th>
                <Th right>Dibayar</Th>
                <Th right>Sisa Sesudah</Th>
              </tr>
            </TableHead>
            <TableBody>
              {preview.allocations.map((a) => (
                <TableRow key={a.invoice_id}>
                  <Td>{a.invoice_number}</Td>
                  <Td>
                    <Tanggal value={a.due_date} />
                  </Td>
                  <Td right mono>
                    <Rupiah value={a.outstanding_before} colorSign={false} />
                  </Td>
                  <Td right mono>
                    <Rupiah value={a.amount} colorSign={false} />
                  </Td>
                  <Td right mono>
                    <Rupiah value={a.outstanding_after} colorSign={false} />
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <p className="border-t border-border px-4 py-2.5 text-[11px] text-text-tertiary">
            Total sisa seluruh tagihan vendor ini menurut buku:{" "}
            <span className="font-mono text-text-secondary">
              {formatRupiah(preview.total_outstanding)}
            </span>
            . Angka inilah yang menjelaskan kenapa hanya sebagian uang terpakai bila
            memang begitu.
          </p>
        </div>
      </div>

      <div className="mt-6">
        <p className="text-sm font-semibold text-text-primary">
          Jurnal yang akan terbentuk
        </p>
        <div className="mt-2 overflow-x-auto rounded-lg border border-border">
          <Table>
            <TableHead>
              <tr>
                <Th>Akun</Th>
                <Th>Uraian</Th>
                <Th right>Debit</Th>
                <Th right>Kredit</Th>
              </tr>
            </TableHead>
            <TableBody>
              {preview.lines.map((ln, i) => (
                <TableRow key={`${ln.account_code}-${i}`}>
                  <Td>
                    <span className="font-mono text-xs text-text-secondary">
                      {ln.account_code}
                    </span>{" "}
                    {ln.account_name}
                  </Td>
                  <Td>{ln.description}</Td>
                  <Td right mono>
                    {ln.debit && ln.debit !== "0" ? (
                      <Rupiah value={ln.debit} colorSign={false} />
                    ) : (
                      <span className="text-text-tertiary">—</span>
                    )}
                  </Td>
                  <Td right mono>
                    {ln.credit && ln.credit !== "0" ? (
                      <Rupiah value={ln.credit} colorSign={false} />
                    ) : (
                      <span className="text-text-tertiary">—</span>
                    )}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <p className="mt-2 text-[11px] text-text-tertiary">
          Bukti kas keluar (BKK) akan terbit otomatis dengan nomor berikutnya, dalam
          transaksi yang sama dengan jurnal ini.
        </p>
      </div>

      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" onClick={onBack} disabled={busy}>
          Batal
        </Button>
        <Button onClick={onSave} disabled={busy}>
          Simpan Pembayaran
        </Button>
      </div>
    </Card>
  );
}

// ── Hasil ───────────────────────────────────────────────────────────────────

function ResultPanel({
  result,
  onAgain,
}: {
  result: APPaymentResult;
  onAgain: () => void;
}) {
  return (
    <Card className="border-success/30 bg-success-bg/40">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-success">
            {result.replayed ? "Pembayaran ini sudah tercatat" : "Pembayaran tercatat"}
          </p>
          <h2 className="display-lg mt-1 font-mono text-xl text-text-primary">
            {result.document_number}
          </h2>
          <p className="mt-1 max-w-prose text-xs text-text-secondary">
            {result.replayed
              ? "Permintaan yang sama pernah dikirim sebelumnya, jadi backend mengembalikan pembayaran yang itu juga. Tidak ada pengeluaran kas kedua dan tidak ada BKK kedua."
              : "Uang sudah keluar dan jurnalnya terposting. Simpan nomor BKK di atas untuk arsip fisik."}
          </p>
        </div>
        <Badge variant="success">
          <Rupiah value={result.payment.amount} colorSign={false} />
        </Badge>
      </div>

      <dl className="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div>
          <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
            Tanggal bayar
          </dt>
          <dd className="mt-0.5 text-sm text-text-primary">
            <Tanggal value={result.payment.payment_date} />
          </dd>
        </div>
        <Field label="Sumber dana" value={result.payment.cash_account_code} />
        <div>
          <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">
            Jurnal
          </dt>
          <dd className="mt-0.5 text-sm">
            <Link
              href={`/accounting/jurnal/${result.journal_entry_id}`}
              className="text-accent hover:underline"
            >
              Jurnal #{result.journal_entry_id}
            </Link>
          </dd>
        </div>
      </dl>

      <div className="mt-5 overflow-x-auto rounded-lg border border-border bg-surface">
        <Table>
          <TableHead>
            <tr>
              <Th>Tagihan yang terbayar</Th>
              <Th right>Sisa Sebelum</Th>
              <Th right>Dibayar</Th>
              <Th right>Sisa Sesudah</Th>
            </tr>
          </TableHead>
          <TableBody>
            {result.allocations.map((a) => (
              <TableRow key={a.invoice_id}>
                <Td>
                  <Link
                    href={`/accounting/hutang/${a.invoice_id}`}
                    className="font-medium text-accent hover:underline"
                  >
                    {a.invoice_number}
                  </Link>
                </Td>
                {/* Jawaban replay tidak membawa sisa sebelum/sesudah: backend tidak
                    menghitung ulang posisi tagihan untuk pembayaran yang sudah
                    tersimpan. Nilai kosong dicetak "—", bukan "Rp 0" — layar tidak
                    boleh mengarang angka yang tidak dikirim backend. */}
                <Td right mono>
                  {a.outstanding_before ? (
                    <Rupiah value={a.outstanding_before} colorSign={false} />
                  ) : (
                    <span className="text-text-secondary">—</span>
                  )}
                </Td>
                <Td right mono>
                  <Rupiah value={a.amount} colorSign={false} />
                </Td>
                <Td right mono>
                  {a.outstanding_after ? (
                    <Rupiah value={a.outstanding_after} colorSign={false} />
                  ) : (
                    <span className="text-text-secondary">—</span>
                  )}
                </Td>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="mt-5 flex flex-wrap justify-end gap-2">
        <Button variant="ghost" onClick={onAgain}>
          Bayar Lagi
        </Button>
        <Link href={`/accounting/pembayaran-vendor/${result.payment.id}`}>
          <Button>Buka Detail Pembayaran</Button>
        </Link>
      </div>
    </Card>
  );
}

// ── Potongan kecil ──────────────────────────────────────────────────────────

function ModeChip({
  active,
  onClick,
  title,
  desc,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  desc: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-lg border px-4 py-2.5 text-left transition-colors ${
        active
          ? "border-accent bg-accent/5"
          : "border-border bg-surface hover:border-accent/40"
      }`}
    >
      <span
        className={`block text-sm font-medium ${
          active ? "text-accent" : "text-text-primary"
        }`}
      >
        {title}
      </span>
      <span className="mt-0.5 block text-[11px] text-text-secondary">{desc}</span>
    </button>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">{label}</dt>
      <dd className="mt-0.5 text-sm text-text-primary">{value}</dd>
    </div>
  );
}

function Amount({
  label,
  value,
  strong,
}: {
  label: string;
  value: string;
  strong?: boolean;
}) {
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
