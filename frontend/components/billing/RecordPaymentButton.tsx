"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { useToast } from "@/components/ui/Toast";
import { previewCollectionPayment, recordCollectionPayment } from "@/lib/api/collection";
import { fetchCashBankAccounts } from "@/lib/api/ledger";
import { createShortfallInvoice } from "@/lib/api/billing";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { ApiError } from "@/lib/api/client";
import type { CollectionPreview, Account, TerminKind } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
  contractId: number;
  /** Sisa tagihan (string dari server) untuk default nominal. */
  outstanding: string;
  buyerName?: string;
  variant?: "primary" | "link";
  size?: "sm" | "md";
  label?: string;
  /** W-13: bila diisi (kontrak KPR ber-akad), opsi "Pencairan Dana Bank" muncul. */
  financingSourceId?: number;
  bankName?: string;
  /** W-13: dipanggil setelah penerimaan/invoice kekurangan tersimpan — agar
   *  parent (mis. UnitSalePanel) bisa menyegarkan saldo/riwayat miliknya
   *  sendiri, terlepas dari router.refresh() yang hanya menyentuh data server. */
  onSuccess?: () => void;
}

type Step = "input" | "preview" | "done";

// W-13: "Jenis Penerimaan" terstruktur — satu pintu masuk untuk semua tipe
// penerimaan (requirement B). "bank_disbursement" hanya muncul bila kontrak
// punya financing_source_id (KPR).
const KIND_OPTIONS: { value: TerminKind; label: string }[] = [
  { value: "dp", label: "DP (Uang Muka)" },
  { value: "installment", label: "Cicilan" },
  { value: "final_payment", label: "Pelunasan" },
  { value: "other", label: "Lainnya" },
];

function intPart(s: string) {
  return (s || "0").split(".")[0];
}
function isPositive(s: string) {
  return /^\d+$/.test(s) && Number(s) > 0;
}

export function RecordPaymentButton({
  token, contractId, outstanding, buyerName, variant = "primary", size = "md",
  label = "Catat Pembayaran", financingSourceId, bankName, onSuccess,
}: Props) {
  const { toast } = useToast();
  const router = useRouter();

  const [open, setOpen] = useState(false);
  const [step, setStep] = useState<Step>("input");

  const [date, setDate] = useState(() => todayLocalStr());
  const [amount, setAmount] = useState(() => intPart(outstanding));
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [bankCode, setBankCode] = useState("");
  const [reference, setReference] = useState("");
  const [notes, setNotes] = useState("");
  const [kind, setKind] = useState<TerminKind>("other");
  const [installmentNo, setInstallmentNo] = useState("");

  const [preview, setPreview] = useState<CollectionPreview | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [idemKey, setIdemKey] = useState("");
  const [done, setDone] = useState<
    { receiptNumber?: string; terminId?: number; remainingBalance: string } | null
  >(null);
  const [shortfallBusy, setShortfallBusy] = useState(false);

  const isDisbursement = kind === "bank_disbursement";

  function openModal() {
    setStep("input");
    setDate(todayLocalStr());
    setAmount(intPart(outstanding));
    setBankCode("");
    setReference("");
    setNotes("");
    setKind("other");
    setInstallmentNo("");
    setPreview(null);
    setDone(null);
    setIdemKey(typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : `${contractId}-${Date.now()}`);
    setOpen(true);
  }

  // Rekening tujuan dari COA (cash/bank aktif).
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    fetchCashBankAccounts(token)
      .then((accs) => {
        if (cancelled) return;
        setAccounts(accs);
        if (accs.length > 0) setBankCode((prev) => prev || accs[0].code);
      })
      .catch(() => {
        if (!cancelled) toast("Gagal memuat daftar rekening", "error");
      });
    return () => { cancelled = true; };
  }, [open, token, toast]);

  const bankAccounts = accounts.filter((a) => a.account_category === "bank");
  const cashAccounts = accounts.filter((a) => a.account_category === "cash");
  const canProceed = isPositive(amount) && !!bankCode && !!date;

  // Langkah 1 → 2: ambil pratinjau (alokasi + jurnal + saldo kredit) lalu tampilkan.
  async function goPreview() {
    if (!canProceed) return;
    setLoadingPreview(true);
    try {
      setPreview(await previewCollectionPayment(token, contractId, amount, bankCode));
      setStep("preview");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memuat pratinjau", "error");
    } finally {
      setLoadingPreview(false);
    }
  }

  async function handleSubmit() {
    if (!preview?.valid) return;
    setSubmitting(true);
    try {
      const res = await recordCollectionPayment(token, {
        contract_id: contractId,
        amount,
        date: new Date(`${date}T12:00:00`).toISOString(),
        bank_account_code: bankCode,
        reference,
        notes: notes || (isDisbursement ? `Pencairan bank${bankName ? ` ${bankName}` : ""}` : undefined),
        idempotency_key: idemKey,
        kind,
        installment_no: kind === "installment" && installmentNo ? Number(installmentNo) : undefined,
        // Diabaikan/diturunkan otomatis backend bila kind bukan bank_disbursement.
        financing_source_id: isDisbursement ? financingSourceId : undefined,
      });
      setDone({
        receiptNumber: res.receipt_number,
        terminId: res.termin_id,
        remainingBalance: res.remaining_balance,
      });
      setStep("done");
      toast(
        isDisbursement
          ? `Pencairan tercatat${res.receipt_number ? ` — dokumen ${res.receipt_number}` : ""}.`
          : `Pembayaran tercatat. Kwitansi ${res.receipt_number ?? "—"}.`,
        "success",
      );
      onSuccess?.();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mencatat pembayaran", "error");
    } finally {
      setSubmitting(false);
    }
  }

  // D-3: sisa realisasi/kekurangan pasca-pencairan → invoice resmi ke Piutang
  // Customer (bukan auto — keputusan admin, sama seperti DisbursementModal lama).
  async function handleShortfall() {
    setShortfallBusy(true);
    try {
      const inv = await createShortfallInvoice(token, contractId);
      toast(`Invoice kekurangan ${inv.invoice_number} dibuat.`, "success");
      setOpen(false);
      onSuccess?.();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat invoice kekurangan", "error");
    } finally {
      setShortfallBusy(false);
    }
  }

  // S8: kelebihan TRANSAKSI INI = overpayment_unapplied (bukan saldo kanonik).
  const hasCredit = preview ? preview.unapplied_is_credit && isPositive(preview.overpayment_unapplied) : false;
  const hasDirectUnapplied = preview ? !preview.unapplied_is_credit && isPositive(preview.overpayment_unapplied) : false;
  const remainingAfter = done ? Number((done.remainingBalance || "0").split(".")[0]) : 0;
  const title =
    step === "done" ? (isDisbursement ? "Pencairan Tercatat" : "Pembayaran Tercatat")
    : step === "preview" ? "Tinjau Pembayaran"
    : `Catat Penerimaan${buyerName ? ` — ${buyerName}` : ""}`;

  const trigger =
    variant === "link" ? (
      <button onClick={openModal} className="text-xs text-accent hover:underline whitespace-nowrap">{label}</button>
    ) : (
      <Button onClick={openModal} size={size}>{label}</Button>
    );

  return (
    <>
      {trigger}
      <Modal open={open} onClose={() => setOpen(false)} title={title} size="md" footer={renderFooter()}>
        {step === "input" && renderInput()}
        {step === "preview" && preview && renderPreview()}
        {step === "done" && done && (
          <div className="space-y-3">
            <div className="flex items-center gap-3 bg-success-bg border border-success/30 rounded-lg px-4 py-3 text-sm">
              <span className="text-success text-lg">✓</span>
              <div>
                <p className="font-medium text-success">
                  {isDisbursement ? "Dokumen" : "Kwitansi"} {done.receiptNumber ?? "—"}
                </p>
                <p className="text-xs text-text-secondary">
                  {isDisbursement
                    ? "Jurnal & dokumen pencairan sudah dibuat. Ini bukti internal — bukan kwitansi customer."
                    : "Jurnal & kwitansi sudah dibuat. Sisa tagihan & statement diperbarui."}
                </p>
              </div>
            </div>
            {done.terminId && (
              <div className="flex items-center justify-between gap-3 rounded-lg border border-border px-4 py-3">
                <span className="text-sm text-text-secondary">
                  {isDisbursement ? "Sisa terutang customer" : "Sisa tagihan"}
                </span>
                <span className="text-base font-bold tabular-nums">
                  <Rupiah value={done.remainingBalance} colorSign={false} />
                </span>
              </div>
            )}
            {done.terminId && (
              <PrintReceiptButton
                token={token}
                terminId={done.terminId}
                variant="button"
                label={isDisbursement ? "Cetak KWD" : "Cetak Kwitansi"}
              />
            )}
            {isDisbursement && remainingAfter > 0 && (
              <p className="text-xs text-text-secondary">
                Masih ada kekurangan pasca-pencairan. Bank sudah selesai pada nilai cairnya —
                sisa ini otomatis di Piutang Customer. Buat invoice kekurangan agar tagihan resmi terbit.
              </p>
            )}
          </div>
        )}
      </Modal>
    </>
  );

  function renderFooter() {
    if (step === "done") {
      return (
        <>
          <Button variant="secondary" onClick={() => setOpen(false)}>Tutup</Button>
          {isDisbursement && remainingAfter > 0 && (
            <Button onClick={handleShortfall} loading={shortfallBusy}>Buat Invoice Kekurangan</Button>
          )}
        </>
      );
    }
    if (step === "preview") {
      return (
        <>
          <Button variant="ghost" onClick={() => setStep("input")} disabled={submitting}>Kembali</Button>
          <Button onClick={handleSubmit} loading={submitting} disabled={submitting || !preview?.valid}>
            {hasCredit ? "Ya, Simpan (dgn Saldo Kredit)" : "Simpan Pembayaran"}
          </Button>
        </>
      );
    }
    return (
      <>
        <Button variant="ghost" onClick={() => setOpen(false)}>Batal</Button>
        <Button onClick={goPreview} loading={loadingPreview} disabled={!canProceed || loadingPreview}>
          Lanjut ke Pratinjau →
        </Button>
      </>
    );
  }

  function renderInput() {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="Jenis Penerimaan">
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value as TerminKind)}
              className={inputCls}
            >
              {KIND_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
              {financingSourceId && (
                <option value="bank_disbursement">Pencairan Dana Bank (KPR)</option>
              )}
            </select>
          </Field>
          {kind === "installment" ? (
            <Field label="Cicilan ke-">
              <input
                type="number" min={1} value={installmentNo}
                onChange={(e) => setInstallmentNo(e.target.value)}
                placeholder="opsional" className={inputCls}
              />
            </Field>
          ) : (
            <Field label="Tanggal Pembayaran">
              <input type="date" value={date} onChange={(e) => setDate(e.target.value)} className={inputCls} />
            </Field>
          )}
        </div>
        {kind === "installment" && (
          <Field label="Tanggal Pembayaran">
            <input type="date" value={date} onChange={(e) => setDate(e.target.value)} className={inputCls} />
          </Field>
        )}
        {isDisbursement && bankName && (
          <p className="text-xs text-text-secondary">Bank penyalur: <strong>{bankName}</strong></p>
        )}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="Rekening / Kas Tujuan">
            {accounts.length === 0 ? (
              <p className="text-xs text-text-tertiary py-2">Memuat rekening…</p>
            ) : (
              <select value={bankCode} onChange={(e) => setBankCode(e.target.value)} className={inputCls}>
                {bankAccounts.length > 0 && (
                  <optgroup label="Bank">
                    {bankAccounts.map((a) => <option key={a.code} value={a.code}>{a.name}</option>)}
                  </optgroup>
                )}
                {cashAccounts.length > 0 && (
                  <optgroup label="Kas">
                    {cashAccounts.map((a) => <option key={a.code} value={a.code}>{a.name}</option>)}
                  </optgroup>
                )}
              </select>
            )}
          </Field>
        </div>
        <RupiahInput label="Nominal Pembayaran" value={amount} onChange={setAmount} required hint={`Sisa tagihan: ${formatRp(outstanding)}`} />
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="Referensi Pembayaran">
            <input value={reference} onChange={(e) => setReference(e.target.value)} placeholder="No. transfer / bukti" className={inputCls} />
          </Field>
          <Field label="Catatan">
            <input value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="opsional" className={inputCls} />
          </Field>
        </div>
        <p className="text-xs text-text-tertiary">Pembayaran belum dicatat. Anda akan meninjau dulu sebelum disimpan.</p>
      </div>
    );
  }

  function renderPreview() {
    if (!preview) return null;
    return (
      <div className="space-y-4">
        {/* Total */}
        <div className="bg-surface border border-border rounded-lg px-4 py-3">
          <p className="text-xs text-text-secondary uppercase tracking-wide">Total Diterima</p>
          <p className="text-2xl font-bold tabular-nums text-text-primary"><Rupiah value={preview.amount} colorSign={false} /></p>
        </div>

        {/* Invalid (mis. kelebihan bayar setelah BAST) */}
        {!preview.valid && (
          <div className="bg-danger-bg border border-danger/30 rounded-lg px-4 py-3 text-sm text-danger">
            ⚠ {preview.reason ?? "Pembayaran tidak dapat diproses."}
          </div>
        )}

        {/* Dialokasikan ke Tagihan */}
        <div>
          <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">Akan Digunakan Untuk</p>
          {preview.applied.length === 0 ? (
            <p className="text-sm text-text-tertiary">
              {hasDirectUnapplied
                ? "Tidak ada jadwal cicilan yang cocok — seluruhnya langsung mengurangi piutang/pembiayaan."
                : "Tidak ada tagihan terbuka — seluruhnya menjadi saldo kredit."}
            </p>
          ) : (
            <div className="border border-border rounded-lg divide-y divide-border-subtle">
              {preview.applied.map((a) => (
                <div key={a.schedule_id} className="flex items-center justify-between px-3 py-2 text-sm">
                  <div className="flex items-center gap-2">
                    <span className={a.fully_paid ? "text-success" : "text-warning"}>{a.fully_paid ? "✓" : "△"}</span>
                    <span className="text-text-primary">{a.label}</span>
                    <span className="text-text-tertiary text-xs">jatuh tempo <Tanggal value={a.due_date} /></span>
                  </div>
                  <div className="text-right">
                    <span className="tabular-nums"><Rupiah value={a.amount} colorSign={false} /></span>
                    <span className={`ml-2 text-xs ${a.fully_paid ? "text-success" : "text-warning"}`}>{a.fully_paid ? "Lunas" : "Sebagian"}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Saldo Kredit Buyer (S8: proyeksi saldo kanonik setelah pembayaran) */}
        <div className={`flex items-center justify-between rounded-lg px-4 py-3 border ${hasCredit ? "border-warning/40 bg-warning-bg" : "border-border bg-border-subtle/30"}`}>
          <span className="text-sm font-medium text-text-secondary">Saldo Kredit Buyer</span>
          <span className={`text-base font-bold tabular-nums ${hasCredit ? "text-warning" : "text-text-primary"}`}><Rupiah value={preview.buyer_credit} colorSign={false} /></span>
        </div>
        {hasCredit && preview.valid && (
          <div className="bg-warning-bg border border-warning/30 rounded-lg px-4 py-3 text-sm text-warning">
            ⚠ Pembayaran lebih besar dari tagihan yang tersedia. Sisa <strong><Rupiah value={preview.overpayment_unapplied} colorSign={false} /></strong> akan disimpan sebagai <strong>saldo kredit buyer</strong> dan bisa dipakai untuk tagihan berikutnya.
          </div>
        )}
        {hasDirectUnapplied && preview.valid && (
          <div className="bg-border-subtle/30 border border-border rounded-lg px-4 py-3 text-sm text-text-secondary">
            Sisa <strong><Rupiah value={preview.overpayment_unapplied} colorSign={false} /></strong> tidak cocok satu jadwal cicilan pun, tapi jurnal di atas sudah mengurangi piutang/pembiayaan secara langsung — <strong>bukan</strong> kelebihan bayar dan tidak menjadi saldo kredit.
          </div>
        )}

        {/* Pratinjau jurnal */}
        <div className="border border-border rounded-lg p-3 bg-border-subtle/30">
          <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">Pratinjau Jurnal</p>
          <table className="w-full text-sm">
            <tbody>
              {preview.lines.map((l, i) => (
                <tr key={i}>
                  <td className="py-1 text-text-secondary"><span className="font-mono text-xs">{l.account_code}</span> {l.account_name}</td>
                  <td className="py-1 text-right tabular-nums">{l.debit !== "0" ? <Rupiah value={l.debit} colorSign={false} /> : ""}</td>
                  <td className="py-1 text-right tabular-nums">{l.credit !== "0" ? <Rupiah value={l.credit} colorSign={false} /> : ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="text-xs text-text-tertiary mt-2 pt-2 border-t border-border">
            {isDisbursement
              ? "Pencairan dana bank — bukan penerimaan dari customer."
              : preview.is_bast ? "Setelah Akad → Piutang Usaha" : "Sebelum Akad → Uang Muka Penjualan"}
          </p>
        </div>
      </div>
    );
  }
}

function formatRp(v: string) {
  const n = Number((v || "0").split(".")[0]);
  return "Rp " + (isNaN(n) ? "0" : n.toLocaleString("id-ID"));
}

const inputCls =
  "w-full rounded border border-border bg-surface text-sm text-text-primary px-3 py-2 outline-none focus:border-accent focus:ring-1 focus:ring-accent/30";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-xs text-text-secondary mb-1">{label}</label>
      {children}
    </div>
  );
}
