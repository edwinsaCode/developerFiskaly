"use client";

import { useEffect, useMemo, useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { Rupiah, formatRupiah } from "@/components/format/Rupiah";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { receiveLegacyPayment } from "@/lib/api/legacyar";
import type { LegacyReceivableView } from "@/lib/api/legacyar";
import { todayLocalStr } from "@/lib/date";

// Modal penerimaan pembayaran piutang proyek lama.
//
// Alokasinya EKSPLISIT per piutang, tidak pernah "kurangi total customer".
// Ketika satu nama punya dua sisa tagihan dari dua proyek lama, hanya admin
// yang tahu uang ini untuk yang mana; menebaknya berarti membuat rincian yang
// terlihat rapi tapi salah orang — dan salahnya baru ketahuan setahun kemudian
// saat ditagih.
//
// Lebih bayar DITOLAK. Dijaga di sini supaya admin melihatnya sebelum menekan
// tombol, dan dijaga lagi oleh server (422) karena penjaga di layar bukan
// penjaga.

interface Props {
  token: string;
  open: boolean;
  onClose: () => void;
  targets: LegacyReceivableView[];
  onDone: () => void;
}

const today = () => todayLocalStr();

function toNum(v: string): number {
  const n = parseFloat(v);
  return isNaN(n) ? 0 : n;
}

/** Sisa tagihan sebagai string integer, untuk mengisi input rupiah. */
function outstandingRaw(v: string): string {
  return String(Math.round(toNum(v)));
}

export function LegacyARPaymentModal({ token, open, onClose, targets, onDone }: Props) {
  const { toast } = useToast();
  const [amounts, setAmounts] = useState<Record<number, string>>({});
  const [cashAccount, setCashAccount] = useState("");
  const [date, setDate] = useState(today());
  const [notes, setNotes] = useState("");
  const [saving, setSaving] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);

  // Kunci idempotensi dibuat SEKALI saat modal dibuka dan dipakai ulang pada
  // percobaan berikutnya. Itulah yang membuat klik ganda atau retry setelah
  // jaringan putus tidak menghasilkan dua penerimaan kas — kalau dibuat ulang
  // tiap submit, kuncinya tidak menjaga apa pun.
  const [idemKey, setIdemKey] = useState("");

  useEffect(() => {
    if (!open) return;
    const seed: Record<number, string> = {};
    for (const t of targets) seed[t.id] = outstandingRaw(t.outstanding);
    setAmounts(seed);
    setDate(today());
    setNotes("");
    setServerError(null);
    setIdemKey(
      typeof crypto !== "undefined" && crypto.randomUUID
        ? crypto.randomUUID()
        : `lar-${Date.now()}-${Math.floor(Math.random() * 1e9)}`,
    );
  }, [open, targets]);

  const lines = useMemo(
    () =>
      targets.map((t) => {
        const raw = amounts[t.id] ?? "";
        const amt = raw ? parseInt(raw, 10) : 0;
        const out = toNum(t.outstanding);
        return {
          t,
          raw,
          amt,
          out,
          over: amt > out,
        };
      }),
    [targets, amounts],
  );

  const total = lines.reduce((s, l) => s + l.amt, 0);
  const anyOver = lines.some((l) => l.over);
  const canSubmit = total > 0 && !anyOver && cashAccount !== "" && date !== "" && !saving;

  async function submit() {
    setServerError(null);
    const allocations = lines
      .filter((l) => l.amt > 0)
      .map((l) => ({ receivable_id: l.t.id, amount: String(l.amt) }));
    if (allocations.length === 0) return;

    setSaving(true);
    try {
      const res = await receiveLegacyPayment(token, {
        allocations,
        cash_account_code: cashAccount,
        payment_date: date,
        notes: notes || undefined,
        idempotency_key: idemKey,
      });
      if (res.duplicate) {
        toast("Pembayaran ini sudah tercatat sebelumnya — tidak dicatat dua kali.", "info");
      } else {
        toast(
          `Pembayaran ${formatRupiah(res.total_amount)} tercatat${
            res.document_number ? ` — bukti ${res.document_number}` : ""
          }`,
          "success",
        );
      }
      onDone();
      onClose();
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Pembayaran gagal disimpan.";
      setServerError(msg);
      toast(msg, "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={saving ? () => {} : onClose}
      title="Terima Pembayaran Piutang Proyek Lama"
      size="lg"
      footer={
        <div className="flex items-center justify-between gap-4 w-full">
          <div className="text-sm">
            <span className="text-text-secondary">Total diterima </span>
            <span className="font-semibold text-text-primary tabular-nums">
              <Rupiah value={String(total)} colorSign={false} />
            </span>
          </div>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={onClose} disabled={saving}>
              Batal
            </Button>
            <Button onClick={submit} disabled={!canSubmit} loading={saving}>
              Simpan Pembayaran
            </Button>
          </div>
        </div>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-text-secondary">
          Uang yang diterima dialokasikan ke tagihan tertentu — bukan dikurangkan dari total
          customer. Jurnalnya: <span className="font-mono text-xs">Dr Kas/Bank</span> /{" "}
          <span className="font-mono text-xs">Cr {targets[0]?.control_account_code ?? "1-2000"}</span>
          . Bukti kas bernomor terbit otomatis.
        </p>

        <div className="rounded-xl border border-border divide-y divide-border overflow-hidden">
          {lines.map((l) => (
            <div key={l.t.id} className="p-4 space-y-2.5">
              <div className="flex items-baseline justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-sm font-medium text-text-primary truncate">
                    {l.t.customer_name}
                  </p>
                  <p className="text-xs text-text-tertiary truncate">
                    {l.t.source_label}
                    {l.t.external_ref ? ` · ${l.t.external_ref}` : ""}
                  </p>
                </div>
                <div className="text-right shrink-0">
                  <p className="text-xs text-text-secondary">Sisa tagihan</p>
                  <p className="text-sm font-medium text-text-primary">
                    <Rupiah value={l.t.outstanding} colorSign={false} />
                  </p>
                </div>
              </div>
              <div className="flex items-end gap-2">
                <RupiahInput
                  value={l.raw}
                  onChange={(v) => setAmounts((a) => ({ ...a, [l.t.id]: v }))}
                  className="flex-1"
                  error={l.over ? "Melebihi sisa tagihan" : undefined}
                  disabled={saving}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setAmounts((a) => ({ ...a, [l.t.id]: outstandingRaw(l.t.outstanding) }))}
                  disabled={saving}
                >
                  Lunas
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setAmounts((a) => ({ ...a, [l.t.id]: "" }))}
                  disabled={saving}
                >
                  Kosongkan
                </Button>
              </div>
            </div>
          ))}
        </div>

        {anyOver && (
          <p className="text-sm text-danger">
            Ada alokasi yang melebihi sisa tagihan. Kelebihan bayar pada piutang proyek lama tidak
            diterima — sisanya tidak punya tagihan untuk dituju.
          </p>
        )}

        <div className="grid grid-cols-1 gap-x-5 gap-y-4 sm:grid-cols-2">
          <CashBankSelect token={token} value={cashAccount} onChange={setCashAccount} required />
          <Input
            label="Tanggal Pembayaran"
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            disabled={saving}
          />
        </div>
        <Input
          label="Keterangan (opsional)"
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          placeholder="mis. transfer BCA a.n. Budi"
          disabled={saving}
        />

        {serverError && <p className="text-sm text-danger">{serverError}</p>}
      </div>
    </Modal>
  );
}
