"use client";

// PRODUCT SPRINT — Milestone Pembiayaan KPR. Gate Akad (pengakuan pendapatan+
// HPP) skema KPR = state "akad"; tanpa UI ini kontrak KPR tidak pernah bisa
// Akad dari aplikasi (gap E2E). Backend: POST /sale-contracts/{id}/scheme-events
// (Increment 3) + POST /units/{id}/akad (Temuan #7).

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { fetchContractByUnit, applySchemeEvent, listTermins } from "@/lib/api/sale";
import { fetchReceiptByTermin } from "@/lib/api/billing";
import { PrintReceiptButton } from "@/components/billing/PrintReceiptButton";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import type { SaleContract } from "@/lib/types/api";
import { AkadForm } from "./AkadForm";
import { FINANCING_STEPS as STEPS, stepIndex } from "./financingSteps";

// Satu pencairan bank = satu KWD. Baris ini bertahan setelah reload — toast
// sukses hilang, dokumennya tidak boleh ikut hilang dari layar.
interface DisbursementRow {
  terminId: number;
  date: string;
  amount: string;
  receiptNumber?: string;
}

// Aksi manual berikutnya per state (event backend yang diizinkan).
// W-13: pencairan TIDAK lagi jadi aksi milestone — dicatat via satu pintu
// masuk "+ Catat Penerimaan" (RecordPaymentButton di UnitSalePanel). Stepper
// di bawah murni status, bukan trigger transaksi finansial (requirement G).
const NEXT_ACTIONS: Record<string, { event: string; label: string; variant?: "secondary" }[]> = {
  signed: [{ event: "submitted_to_bank", label: "Ajukan ke Bank" }],
  dp_paid: [{ event: "submitted_to_bank", label: "Ajukan ke Bank" }],
  submitted_to_bank: [
    { event: "bank_approved", label: "SP3K Terbit" },
    { event: "bank_rejected", label: "Bank Menolak", variant: "secondary" },
  ],
  bank_approved: [{ event: "akad", label: "Catat Akad Kredit" }],
  bank_rejected: [{ event: "submitted_to_bank", label: "Ajukan Ulang" }],
  akad: [],
  disbursed: [],
};

export function FinancingMilestones({ unitId, token, onPaymentRecorded, refreshKey }: {
  unitId: number;
  token: string;
  /** R1: pencairan tercatat → parent refresh saldo/jadwal tanpa reload. */
  onPaymentRecorded?: () => void;
  /** W-13: naikkan dari parent setelah "+ Catat Penerimaan" agar daftar KWD ikut segar. */
  refreshKey?: number;
}) {
  const router = useRouter();
  const { toast } = useToast();
  const [contract, setContract] = useState<SaleContract | null>(null);
  const [busy, setBusy] = useState(false);
  const [showAkadForm, setShowAkadForm] = useState(false);
  const [disbursements, setDisbursements] = useState<DisbursementRow[]>([]);

  const load = useCallback(async () => {
    try {
      setContract(await fetchContractByUnit(token, unitId));
    } catch {
      setContract(null);
    }
  }, [token, unitId]);

  useEffect(() => {
    load();
  }, [load]);

  // Bukti pencairan (KWD) yang sudah terbit. Sumbernya termin unit dengan
  // payment_source=kpr_disbursement — SoT yang sama dengan yang dipakai backend
  // untuk menentukan tipe kwitansi; nomornya dibaca (bukan diterbitkan) lewat
  // GET /termins/{id}/receipt. Gagal baca = baris tidak muncul, tidak pernah
  // menggagalkan panel milestone.
  const loadDisbursements = useCallback(async () => {
    try {
      const { data } = await listTermins(token, unitId);
      const rows = (data ?? []).filter((t) => t.payment_source === "kpr_disbursement");
      const withReceipts = await Promise.all(
        rows.map(async (t): Promise<DisbursementRow> => {
          const rec = await fetchReceiptByTermin(token, t.id).catch(() => null);
          return {
            terminId: t.id,
            date: t.date,
            amount: t.amount,
            receiptNumber: rec?.receipt_number,
          };
        }),
      );
      setDisbursements(withReceipts);
    } catch {
      setDisbursements([]);
    }
  }, [token, unitId]);

  useEffect(() => {
    if (contract?.payment_type === "kpr") loadDisbursements();
  }, [contract?.payment_type, loadDisbursements, refreshKey]);

  if (!contract || contract.payment_type !== "kpr" || !contract.scheme_state) return null;

  const state = contract.scheme_state;
  const idx = stepIndex(state);
  const actions = NEXT_ACTIONS[state] ?? [];
  const rejected = state === "bank_rejected";

  async function act(event: string, label: string) {
    if (!contract) return;
    if (event === "akad") {
      // Temuan #7: Akad Kredit sekarang mengakui pendapatan+HPP (bukan
      // sekadar transisi state) — butuh input finansial, lewat AkadForm.
      setShowAkadForm(true);
      return;
    }
    setBusy(true);
    try {
      await applySchemeEvent(token, contract.id, {
        event,
        financing_source_id: contract.financing_source_id,
      });
      toast(`${label} tercatat`, "success");
      await load();
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mencatat milestone", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <div className="flex items-center justify-between flex-wrap gap-2 mb-3">
        <p className="text-sm font-semibold">
          Pembiayaan KPR
          {rejected && <Badge variant="danger" className="ml-2">Ditolak Bank</Badge>}
        </p>
        <div className="flex gap-2">
          {actions.map((a) => (
            <Button key={a.event} size="sm" variant={a.variant} loading={busy}
              onClick={() => act(a.event, a.label)}>
              {a.label}
            </Button>
          ))}
        </div>
      </div>

      {/* Progress stepper */}
      <div className="flex items-center">
        {STEPS.map((s, i) => {
          const done = !rejected && i <= idx;
          const isCurrent = !rejected && i === idx;
          return (
            <div key={s.state} className="flex items-center flex-1 last:flex-none">
              <div className="flex flex-col items-center">
                <div
                  className={`flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-semibold transition-colors
                    ${done ? "bg-accent text-white" : "bg-border-subtle text-text-tertiary"}
                    ${isCurrent ? "ring-2 ring-accent/30" : ""}`}
                >
                  {done && i < idx ? "✓" : i + 1}
                </div>
                <p className={`mt-1 text-[10px] whitespace-nowrap ${done ? "text-text-primary font-medium" : "text-text-tertiary"}`}>
                  {s.label}
                </p>
              </div>
              {i < STEPS.length - 1 && (
                <div className={`mx-1.5 mb-4 h-0.5 flex-1 rounded ${!rejected && i < idx ? "bg-accent" : "bg-border-subtle"}`} />
              )}
            </div>
          );
        })}
      </div>

      <p className="mt-2 text-[11px] text-text-tertiary">
        Pendapatan &amp; HPP diakui saat <strong>Akad Kredit</strong> dicatat. Serah terima fisik
        terpisah, bisa kapan saja setelahnya. Pencairan dana bank (bertahap/sekaligus) dicatat
        lewat <strong>+ Catat Penerimaan</strong> — pilih jenis &quot;Pencairan Dana Bank&quot;.
      </p>

      {/* Bukti pencairan — nomor KWD tetap terlihat setelah reload, dan bisa
          dicetak ulang tanpa berpindah layar. */}
      {disbursements.length > 0 && (
        <div className="mt-3 border-t border-border pt-3">
          <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-text-secondary">
            Bukti Pencairan (KWD)
          </p>
          <ul className="space-y-1.5">
            {disbursements.map((d) => (
              <li key={d.terminId} className="flex flex-wrap items-center justify-between gap-2 text-sm">
                <span className="flex items-center gap-2">
                  <span className="font-mono text-xs text-text-secondary">
                    {d.receiptNumber ?? "—"}
                  </span>
                  <span className="text-xs text-text-tertiary">
                    <Tanggal value={d.date} />
                  </span>
                </span>
                <span className="flex items-center gap-3">
                  <span className="font-medium tabular-nums">
                    <Rupiah value={d.amount} colorSign={false} />
                  </span>
                  <PrintReceiptButton token={token} terminId={d.terminId} label="Cetak KWD" />
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {contract && (
        <AkadForm
          open={showAkadForm}
          onClose={() => setShowAkadForm(false)}
          unitId={unitId}
          listPrice={contract.dpp_amount}
          token={token}
          onBeforeSubmit={async () => {
            // GateAkad KPR mensyaratkan state SUDAH "akad" saat RecordAkad
            // dipanggil — pindahkan state dulu (bank_approved → akad), baru
            // recordAkad jalan mengakui pendapatan+HPP.
            await applySchemeEvent(token, contract.id, {
              event: "akad",
              financing_source_id: contract.financing_source_id,
            });
          }}
          onSubmitted={() => { load(); onPaymentRecorded?.(); }}
        />
      )}
    </div>
  );
}
