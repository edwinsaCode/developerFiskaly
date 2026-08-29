"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import type { CostCategory, PaymentMethod, ProjectPhase, Unit, BudgetItem, JournalPreviewLine } from "@/lib/types/api";
import type { CreateCostEntryBody } from "@/lib/api/cost";
import { Input, Select } from "@/components/ui/Input";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { useToast } from "@/components/ui/Toast";
import { todayLocalStr } from "@/lib/date";
import {
  previewCostEntryAction,
  createAndPostCostEntryAction,
} from "@/app/(app)/proyek/[id]/biaya/actions";

// ── Kategori (4 valid untuk CostEntry — backend menolak marketing/other) ──────

const COST_CATEGORIES: { value: CostCategory; label: string }[] = [
  { value: "land",      label: "Tanah" },
  { value: "hard",      label: "Hard Cost (Konstruksi)" },
  { value: "soft",      label: "Soft Cost" },
  { value: "financing", label: "Pendanaan" },
];

// ── Props ─────────────────────────────────────────────────────────────────────

interface CostEntryFormProps {
  token: string;
  projectId: number;
  phases: ProjectPhase[];
  units: Unit[];
  budgetItems: BudgetItem[];
}

// ── Tipe form internal ─────────────────────────────────────────────────────────

interface FormState {
  category: CostCategory | "";
  amount: string;
  payment_method: PaymentMethod | "";
  bank_account_code: string;
  date: string;
  vendor: string;
  description: string;
  phase_id: string;
  unit_id: string;
  budget_item_id: string;
}

const INITIAL_FORM: FormState = {
  category: "",
  amount: "",
  payment_method: "",
  bank_account_code: "",
  date: todayLocalStr(),
  vendor: "",
  description: "",
  phase_id: "",
  unit_id: "",
  budget_item_id: "",
};

// ── Komponen ──────────────────────────────────────────────────────────────────

export function CostEntryForm({ token, projectId, phases, units, budgetItems }: CostEntryFormProps) {
  const router = useRouter();
  const { toast } = useToast();
  const [form, setForm] = useState<FormState>(INITIAL_FORM);
  const [errors, setErrors] = useState<Partial<Record<keyof FormState, string>>>({});
  const [showConfirm, setShowConfirm] = useState(false);
  const [previewLines, setPreviewLines] = useState<JournalPreviewLine[] | null>(null);
  const [previewing, startPreview] = useTransition();
  const [saving, startSave] = useTransition();

  const set = (k: keyof FormState, v: string) => setForm(f => ({ ...f, [k]: v }));

  // ── Validasi (sisi klien — backend tetap penjaga utama) ───────────────────

  function validate(): boolean {
    const e: Partial<Record<keyof FormState, string>> = {};
    if (!form.category)        e.category       = "Pilih kategori";
    const amtErr = validateRupiah(form.amount);
    if (amtErr)                e.amount         = amtErr;
    if (!form.payment_method)  e.payment_method = "Pilih metode pembayaran";
    if (form.payment_method === "bank" && !form.bank_account_code.trim())
                               e.bank_account_code = "Pilih rekening kas/bank sumber dana";
    if (!form.date)            e.date           = "Tanggal wajib diisi";
    if (!form.vendor.trim())   e.vendor         = "Vendor wajib diisi";
    if (!form.description.trim()) e.description = "Deskripsi wajib diisi";
    setErrors(e);
    return Object.keys(e).length === 0;
  }

  // ── Tinjau: panggil preview endpoint, baru buka modal konfirmasi ──────────

  function handleTinjau() {
    if (!validate()) return;
    const body = buildBody();
    startPreview(async () => {
      try {
        const result = await previewCostEntryAction(projectId, body);
        setPreviewLines(result.lines);
        setShowConfirm(true);
      } catch (err) {
        toast(`Preview gagal: ${err instanceof Error ? err.message : "Kesalahan server"}`, "error");
      }
    });
  }

  // ── Konfirmasi: simpan + posting ─────────────────────────────────────────

  function handleConfirm() {
    startSave(async () => {
      try {
        await createAndPostCostEntryAction(projectId, buildBody());
        setShowConfirm(false);
        setPreviewLines(null);
        setForm(INITIAL_FORM);
        router.refresh();
        toast("Biaya berhasil dicatat & jurnal diposting", "success");
      } catch (err) {
        setShowConfirm(false);
        toast(`Gagal simpan: ${err instanceof Error ? err.message : "Kesalahan server"}`, "error");
      }
    });
  }

  function buildBody(): CreateCostEntryBody {
    return {
      category:          form.category as CostCategory,
      amount:            form.amount,
      payment_method:    form.payment_method as PaymentMethod,
      bank_account_code: form.payment_method === "bank" ? form.bank_account_code.trim() : undefined,
      date:              form.date,
      vendor:            form.vendor.trim(),
      description:       form.description.trim(),
      phase_id:          form.phase_id      ? parseInt(form.phase_id, 10)      : null,
      unit_id:           form.unit_id       ? parseInt(form.unit_id, 10)       : null,
      budget_item_id:    form.budget_item_id ? parseInt(form.budget_item_id, 10) : null,
    };
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Catat Biaya Proyek</CardTitle>
          <p className="text-xs text-text-secondary mt-0.5">
            Setelah disimpan, jurnal diposting otomatis dan tidak bisa diubah.
          </p>
        </CardHeader>

        {/* Catatan kategori — sejak W-10 beban operasional punya halamannya
            sendiri, jadi tidak ada lagi alasan mengarahkan user ke Jurnal Umum. */}
        <div className="mb-5 px-4 py-3 rounded-lg bg-accent-light/50 border border-accent/25 text-xs text-text-secondary">
          Form ini untuk biaya yang <strong>dikapitalisasi</strong> ke proyek (tanah,
          konstruksi, soft cost, pendanaan). Beban operasional seperti gaji, sewa, dan
          listrik dicatat di{" "}
          <a href="/accounting/pengeluaran" className="font-medium text-accent hover:underline">
            Keuangan → Pengeluaran
          </a>
          .
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Select
            label="Kategori"
            required
            value={form.category}
            onChange={e => set("category", e.target.value)}
            error={errors.category}
          >
            <option value="">Pilih kategori biaya...</option>
            {COST_CATEGORIES.map(c => (
              <option key={c.value} value={c.value}>{c.label}</option>
            ))}
          </Select>

          <RupiahInput
            label="Jumlah"
            required
            value={form.amount}
            onChange={v => set("amount", v)}
            error={errors.amount}
            hint="Rupiah bulat, tanpa sen"
          />

          <Select
            label="Metode Pembayaran"
            required
            value={form.payment_method}
            onChange={e => set("payment_method", e.target.value)}
            error={errors.payment_method}
            hint="Pembayaran via hutang usaha belum tersedia — lihat catatan di bawah."
          >
            <option value="">Pilih metode...</option>
            <option value="bank">Bank / Kas</option>
            {/* W-10 §9: hutang usaha dinonaktifkan, TIDAK dihapus diam-diam.
                T-9: backend kini MENOLAKNYA di rute ini juga (HTTP 400), sama
                seperti /expenses — belum ada satu pun layar yang bisa melunasi
                2-1000, jadi saldo yang lahir dari sini akan menggantung di
                neraca. Opsi ini dibuka kembali saat modul Hutang Usaha (W-11)
                ada; pemetaan akunnya di domain sengaja dipertahankan utuh. */}
            <option value="payable" disabled>
              Hutang Usaha (belum tersedia)
            </option>
          </Select>

          {form.payment_method === "bank" ? (
            <div>
              {/* T-5: rekening sumber dana dipilih dari COA (kas/bank aktif),
                  bukan diketik. Backend memang menolak akun yang bukan kas/bank,
                  tapi menolak setelah user mengetik kode yang salah bukan
                  perbaikan — pilihannya yang harus benar sejak awal. */}
              <CashBankSelect
                token={token}
                label="Sumber Dana (Kas / Bank)"
                required
                value={form.bank_account_code}
                onChange={code => set("bank_account_code", code)}
              />
              {errors.bank_account_code && (
                <p className="mt-1 text-xs text-danger">{errors.bank_account_code}</p>
              )}
            </div>
          ) : (
            <div className="hidden md:block" />
          )}

          <Input
            label="Tanggal"
            required
            type="date"
            value={form.date}
            onChange={e => set("date", e.target.value)}
            error={errors.date}
          />

          <Input
            label="Vendor / Pihak"
            required
            placeholder="Nama vendor atau kontraktor"
            value={form.vendor}
            onChange={e => set("vendor", e.target.value)}
            error={errors.vendor}
          />

          <div className="md:col-span-2">
            <Input
              label="Deskripsi"
              required
              placeholder="Uraian biaya secara singkat"
              value={form.description}
              onChange={e => set("description", e.target.value)}
              error={errors.description}
            />
          </div>

          {phases.length > 0 && (
            <Select
              label="Fase (opsional)"
              value={form.phase_id}
              onChange={e => set("phase_id", e.target.value)}
            >
              <option value="">— Semua Fase —</option>
              {phases.map(p => <option key={p.id} value={String(p.id)}>{p.name}</option>)}
            </Select>
          )}

          {units.length > 0 && (
            <Select
              label="Unit (opsional)"
              value={form.unit_id}
              onChange={e => set("unit_id", e.target.value)}
            >
              <option value="">— Tidak spesifik unit —</option>
              {units.map(u => (
                <option key={u.id} value={String(u.id)}>{u.code} ({u.unit_type})</option>
              ))}
            </Select>
          )}
          {units.length > 0 && (
            <p className="-mt-2 text-[11px] text-text-tertiary">
              Memilih unit menjadikan biaya ini <strong>biaya langsung</strong> unit tersebut
              (masuk HPP saat serah terima). Hanya unit produk properti yang bisa dipilih.
            </p>
          )}

          {budgetItems.length > 0 && (
            <Select
              label="Link ke Item RAB (opsional)"
              value={form.budget_item_id}
              onChange={e => set("budget_item_id", e.target.value)}
            >
              <option value="">— Tanpa link RAB —</option>
              {budgetItems.map(b => (
                <option key={b.id} value={String(b.id)}>
                  #{b.id} — {b.description || b.subcategory || b.category}
                </option>
              ))}
            </Select>
          )}
        </div>

        <div className="mt-6 flex justify-end">
          <Button onClick={handleTinjau} loading={previewing}>
            Tinjau &amp; Simpan
          </Button>
        </div>
      </Card>

      {/* Modal konfirmasi — preview jurnal dari backend */}
      <ConfirmModal
        open={showConfirm}
        title="Konfirmasi Catat Biaya"
        confirmLabel="Simpan & Posting"
        onConfirm={handleConfirm}
        onCancel={() => { setShowConfirm(false); setPreviewLines(null); }}
        loading={saving}
      >
        <div className="space-y-4 text-sm text-text-secondary">
          <p>
            Jurnal berikut akan <strong>diposting secara permanen</strong>.
            Setelah diposting, entri tidak bisa diubah atau dihapus.
          </p>

          {previewLines && previewLines.length > 0 && (
            <div className="rounded border border-border bg-surface p-3">
              <p className="text-xs font-semibold text-text-primary mb-2 uppercase tracking-wide">
                Preview Jurnal
                <span className="ml-1 font-normal normal-case text-text-tertiary">
                  — dihitung oleh backend, bukan frontend
                </span>
              </p>
              {previewLines.map((line, i) => (
                <BackendJournalLine key={i} line={line} isCredit={parseFloat(line.credit) > 0} />
              ))}
            </div>
          )}

          <p className="text-xs text-text-tertiary">
            Vendor: <strong>{form.vendor}</strong> | Tanggal: <strong>{form.date}</strong>
          </p>
        </div>
      </ConfirmModal>
    </>
  );
}

// ── BackendJournalLine ────────────────────────────────────────────────────────
// Menampilkan satu baris jurnal persis seperti yang dikembalikan backend.
// Frontend tidak tahu-menahu soal kode akun — hanya menampilkan data dari response.

function BackendJournalLine({ line, isCredit }: { line: JournalPreviewLine; isCredit: boolean }) {
  const amount = isCredit ? line.credit : line.debit;
  const formattedAmount = amount
    ? `Rp ${parseInt(amount, 10).toLocaleString("id-ID")}`
    : "Rp —";

  return (
    <div className={`flex items-center gap-2 text-sm py-0.5 ${isCredit ? "pl-4" : ""}`}>
      <span className={`w-5 text-xs font-bold shrink-0 ${isCredit ? "text-danger" : "text-success"}`}>
        {isCredit ? "K" : "D"}
      </span>
      <span className="font-mono text-xs text-text-tertiary w-16 shrink-0">{line.account_code}</span>
      <span className="flex-1 text-text-primary">{line.account_name}</span>
      <span className="font-medium tabular-nums text-text-primary">{formattedAmount}</span>
    </div>
  );
}
