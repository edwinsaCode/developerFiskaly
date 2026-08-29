"use client";

import { useEffect, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { todayLocalStr } from "@/lib/date";
import type {
  BudgetItem,
  ExpenseListItem,
  ExpenseScope,
  ExpenseType,
  FixedAsset,
  FixedAssetCategory,
  JournalPreviewLine,
  Project,
  Unit,
} from "@/lib/types/api";
import {
  isFixedAssetPreview,
  isFixedAssetResult,
  type ExpenseBody,
  type ExpenseCreateResult,
  type ExpensePreviewResult,
} from "@/lib/api/expense";
import { Input, Select } from "@/components/ui/Input";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import {
  previewExpenseAction,
  createExpenseAction,
  fetchProjectContextAction,
} from "@/app/(app)/accounting/pengeluaran/actions";

type PurchaseType = "expense" | "fixed_asset";

// Kategori biaya proyek — 4 kategori yang dikapitalisasi. Nilai ini adalah
// taksonomi domain backend; label saja yang milik UI.
const PROJECT_CATEGORIES = [
  { value: "land", label: "Tanah" },
  { value: "hard", label: "Hard Cost (Konstruksi)" },
  { value: "soft", label: "Soft Cost (Perizinan, Desain)" },
  { value: "financing", label: "Biaya Pendanaan" },
] as const;

interface Props {
  token: string;
  canWrite: boolean;
  projects: Project[];
  expenseTypes: ExpenseType[];
  fixedAssetCategories: FixedAssetCategory[];
}

interface FormState {
  purchaseType: PurchaseType;
  scope: ExpenseScope;
  date: string;
  amount: string;
  bank_account_code: string;
  vendor: string;
  description: string;
  expense_type_id: string;
  project_id: string;
  unit_id: string;
  budget_item_id: string;
  category: string;
  // Jenis Pembelian = Aset Tetap
  fixed_asset_category_id: string;
  asset_name: string;
  residual_value: string;
  useful_life_months: string;
}

function initialForm(purchaseType: PurchaseType = "expense"): FormState {
  return {
    purchaseType,
    scope: "operasional",
    date: todayLocalStr(),
    amount: "",
    bank_account_code: "",
    vendor: "",
    description: "",
    expense_type_id: "",
    project_id: "",
    unit_id: "",
    budget_item_id: "",
    category: "",
    fixed_asset_category_id: "",
    asset_name: "",
    residual_value: "0",
    useful_life_months: "",
  };
}

export function ExpenseForm({ token, canWrite, projects, expenseTypes, fixedAssetCategories }: Props) {
  const router = useRouter();
  const { toast } = useToast();
  const [form, setForm] = useState<FormState>(initialForm());
  const [errors, setErrors] = useState<Partial<Record<keyof FormState, string>>>({});
  const [showConfirm, setShowConfirm] = useState(false);
  const [preview, setPreview] = useState<ExpensePreviewResult | null>(null);
  const [result, setResult] = useState<ExpenseCreateResult | null>(null);
  const [previewing, startPreview] = useTransition();
  const [saving, startSave] = useTransition();

  const isFixedAsset = form.purchaseType === "fixed_asset";
  const activeCategories = fixedAssetCategories.filter((c) => c.is_active);
  const selectedCategory = activeCategories.find(
    (c) => String(c.id) === form.fixed_asset_category_id,
  );

  // Konteks proyek (unit + item RAB) dimuat saat proyek dipilih.
  const [units, setUnits] = useState<Unit[]>([]);
  const [budgetItems, setBudgetItems] = useState<BudgetItem[]>([]);
  const [budgetPlanLabel, setBudgetPlanLabel] = useState("");
  const [loadingCtx, setLoadingCtx] = useState(false);

  const set = (k: keyof FormState, v: string) => setForm((f) => ({ ...f, [k]: v }));

  const activeTypes = expenseTypes.filter((t) => t.is_active);
  const selectedType = activeTypes.find((t) => String(t.id) === form.expense_type_id);
  const isProjectScope = form.scope === "proyek";

  // INV-EXP-2 datang dari backend sebagai `allows_project_tag`. Kalau jenis yang
  // dipilih tidak boleh di-tag proyek, tag yang terlanjur terisi dibersihkan —
  // supaya user tidak menekan Tinjau lalu ditolak tanpa mengerti sebabnya.
  const typeAllowsProjectTag = selectedType?.allows_project_tag ?? true;
  useEffect(() => {
    if (!isProjectScope && !typeAllowsProjectTag && form.project_id) {
      setForm((f) => ({ ...f, project_id: "", unit_id: "", budget_item_id: "" }));
    }
  }, [typeAllowsProjectTag, isProjectScope, form.project_id]);

  useEffect(() => {
    const pid = parseInt(form.project_id, 10);
    if (!pid) {
      setUnits([]);
      setBudgetItems([]);
      setBudgetPlanLabel("");
      return;
    }
    let cancelled = false;
    setLoadingCtx(true);
    fetchProjectContextAction(pid)
      .then((ctx) => {
        if (cancelled) return;
        setUnits(ctx.units);
        setBudgetItems(ctx.budgetItems);
        setBudgetPlanLabel(ctx.budgetPlanLabel);
      })
      .catch(() => {
        if (!cancelled) {
          setUnits([]);
          setBudgetItems([]);
          setBudgetPlanLabel("");
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingCtx(false);
      });
    return () => {
      cancelled = true;
    };
  }, [form.project_id]);

  function switchScope(scope: ExpenseScope) {
    setErrors({});
    setForm((f) => ({
      ...f,
      scope,
      // Field yang hanya bermakna di scope lain dikosongkan — bukan disembunyikan
      // sambil tetap ikut terkirim.
      expense_type_id: scope === "proyek" ? "" : f.expense_type_id,
      category: scope === "operasional" ? "" : f.category,
      unit_id: "",
      budget_item_id: "",
      project_id: scope === "proyek" ? f.project_id : "",
    }));
  }

  // switchPurchaseType: toggle "Jenis Pembelian". Field khusus sisi lain
  // dikosongkan supaya tidak ikut terkirim diam-diam kalau user berpindah
  // bolak-balik sebelum menekan Tinjau.
  function switchPurchaseType(purchaseType: PurchaseType) {
    setErrors({});
    setForm((f) => ({
      ...f,
      purchaseType,
      fixed_asset_category_id: purchaseType === "fixed_asset" ? f.fixed_asset_category_id : "",
      asset_name: purchaseType === "fixed_asset" ? f.asset_name : "",
      residual_value: purchaseType === "fixed_asset" ? f.residual_value : "0",
      useful_life_months: purchaseType === "fixed_asset" ? f.useful_life_months : "",
    }));
  }

  // Memilih kategori mengisi masa manfaat default-nya — kategori sudah
  // mengikat akun aset/akumulasi/beban, jadi masa manfaat bawaannya juga
  // masuk akal sebagai titik awal (masih bisa diubah per aset).
  function selectCategory(id: string) {
    const cat = activeCategories.find((c) => String(c.id) === id);
    setForm((f) => ({
      ...f,
      fixed_asset_category_id: id,
      useful_life_months: cat ? String(cat.default_useful_life_months) : f.useful_life_months,
    }));
  }

  function validate(): boolean {
    const e: Partial<Record<keyof FormState, string>> = {};
    const amtErr = validateRupiah(form.amount);
    if (amtErr) e.amount = amtErr;
    if (!form.date) e.date = "Tanggal wajib diisi";
    if (!form.bank_account_code) e.bank_account_code = "Pilih rekening kas/bank sumber dana";
    if (!form.vendor.trim()) e.vendor = "Penerima / pihak wajib diisi";
    if (!form.description.trim()) e.description = "Deskripsi wajib diisi";

    if (isFixedAsset) {
      if (!form.fixed_asset_category_id) e.fixed_asset_category_id = "Pilih kategori aset";
      if (!form.asset_name.trim()) e.asset_name = "Nama aset wajib diisi";
      const life = parseInt(form.useful_life_months, 10);
      if (!form.useful_life_months || Number.isNaN(life) || life < 1) {
        e.useful_life_months = "Masa manfaat minimal 1 bulan";
      }
      // Nilai residu opsional dan sah bila nol (paling umum) — validateRupiah
      // dibangun untuk field nominal wajib dan menolak "0", jadi hanya jalankan
      // untuk nilai positif yang benar-benar diisi pengguna.
      if (form.residual_value && form.residual_value !== "0") {
        const residErr = validateRupiah(form.residual_value);
        if (residErr) e.residual_value = residErr;
        else if (form.amount && !amtErr && Number(form.residual_value) > Number(form.amount)) {
          e.residual_value = "Nilai residu tidak boleh melebihi harga perolehan";
        }
      }
    } else if (form.scope === "operasional") {
      if (!form.expense_type_id) e.expense_type_id = "Pilih jenis pengeluaran";
    } else {
      if (!form.project_id) e.project_id = "Pilih proyek";
      if (!form.category) e.category = "Pilih kategori biaya proyek";
    }
    setErrors(e);
    return Object.keys(e).length === 0;
  }

  function buildBody(): ExpenseBody {
    const num = (s: string) => (s ? parseInt(s, 10) : undefined);
    const body: ExpenseBody = {
      purchase_type: form.purchaseType,
      scope: form.scope,
      date: form.date,
      amount: form.amount,
      payment_method: "bank",
      bank_account_code: form.bank_account_code,
      vendor: form.vendor.trim(),
      description: form.description.trim(),
    };
    if (isFixedAsset) {
      body.fixed_asset_category_id = num(form.fixed_asset_category_id);
      body.asset_name = form.asset_name.trim();
      body.residual_value = form.residual_value || "0";
      body.useful_life_months = num(form.useful_life_months);
      body.project_id = num(form.project_id);
      return body;
    }
    if (form.scope === "operasional") {
      body.expense_type_id = num(form.expense_type_id);
      // Tag proyek opsional — cost center, BUKAN realisasi RAB.
      body.project_id = num(form.project_id);
    } else {
      body.project_id = num(form.project_id);
      body.category = form.category;
      body.unit_id = num(form.unit_id);
      body.budget_item_id = num(form.budget_item_id);
      // Memilih unit menjadikan biaya ini biaya langsung unit tersebut.
      body.cost_tier = form.unit_id ? "direct" : "shared";
    }
    return body;
  }

  function handleTinjau() {
    if (!validate()) return;
    const body = buildBody();
    startPreview(async () => {
      try {
        const res = await previewExpenseAction(body);
        setPreview(res);
        setShowConfirm(true);
      } catch (err) {
        toast(`Tinjauan gagal: ${err instanceof Error ? err.message : "Kesalahan server"}`, "error");
      }
    });
  }

  function handleConfirm() {
    startSave(async () => {
      try {
        const item = await createExpenseAction(buildBody());
        setShowConfirm(false);
        setPreview(null);
        setResult(item);
        setForm(initialForm(form.purchaseType));
        router.refresh();
      } catch (err) {
        setShowConfirm(false);
        toast(`Gagal menyimpan: ${err instanceof Error ? err.message : "Kesalahan server"}`, "error");
      }
    });
  }

  if (!canWrite) {
    return (
      <Card>
        <p className="text-sm text-text-secondary">
          Peran Anda hanya bisa melihat. Pencatatan pengeluaran memerlukan peran
          Pemilik atau Accounting.
        </p>
      </Card>
    );
  }

  return (
    <>
      {result && isFixedAssetResult(result) && (
        <FixedAssetSuccessPanel asset={result} onDismiss={() => setResult(null)} />
      )}
      {result && !isFixedAssetResult(result) && (
        <SuccessPanel item={result} onDismiss={() => setResult(null)} />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Catat Pengeluaran</CardTitle>
          <p className="text-xs text-text-secondary mt-0.5">
            Uang keluar dari kas/bank perusahaan. Bukti kas keluar (BKK) terbit
            otomatis dan jurnal langsung diposting — tidak bisa diubah setelahnya.
          </p>
        </CardHeader>

        {/* ── Jenis Pembelian: Pengeluaran biasa atau Aset Tetap ── */}
        <div className="mb-5">
          <span className="text-sm font-medium text-text-primary">Jenis Pembelian</span>
          <div className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-3">
            <ScopeCard
              active={!isFixedAsset}
              onClick={() => switchPurchaseType("expense")}
              title="Pengeluaran Biasa"
              desc="Beban periode berjalan atau biaya proyek — jalur yang sudah ada."
            />
            <ScopeCard
              active={isFixedAsset}
              onClick={() => switchPurchaseType("fixed_asset")}
              title="Aset Tetap"
              desc="Kendaraan, peralatan, dll. Dicatat sebagai aset dan disusutkan tiap bulan — bukan beban langsung."
            />
          </div>
        </div>

        {/* ── Scope: menentukan seluruh sisa form (khusus Pengeluaran Biasa) ── */}
        {!isFixedAsset && (
          <div className="mb-5">
            <span className="text-sm font-medium text-text-primary">Jenis Transaksi</span>
            <div className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-3">
              <ScopeCard
                active={form.scope === "operasional"}
                onClick={() => switchScope("operasional")}
                title="Operasional / Kantor"
                desc="Gaji, sewa, listrik, ATK, transport. Langsung menjadi beban periode berjalan."
              />
              <ScopeCard
                active={isProjectScope}
                onClick={() => switchScope("proyek")}
                title="Biaya Proyek"
                desc="Material, vendor, perizinan. Dikapitalisasi ke persediaan dan menjadi HPP saat unit diserahterimakan."
              />
            </div>
          </div>
        )}

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* ── Jenis Pembelian: Aset Tetap ── */}
          {isFixedAsset && (
            <>
              <Select
                label="Kategori Aset"
                required
                value={form.fixed_asset_category_id}
                onChange={(e) => selectCategory(e.target.value)}
                error={errors.fixed_asset_category_id}
              >
                <option value="">Pilih kategori aset...</option>
                {activeCategories.map((c) => (
                  <option key={c.id} value={String(c.id)}>{c.name}</option>
                ))}
              </Select>
              <div className="flex items-end pb-1">
                {selectedCategory ? (
                  <p className="text-[11px] text-text-tertiary">
                    Akun aset{" "}
                    <span className="font-mono text-text-secondary">{selectedCategory.asset_account_code}</span>,
                    akumulasi penyusutan{" "}
                    <span className="font-mono text-text-secondary">{selectedCategory.accumulated_depreciation_account_code}</span>,
                    beban penyusutan{" "}
                    <span className="font-mono text-text-secondary">{selectedCategory.depreciation_expense_account_code}</span>.
                  </p>
                ) : activeCategories.length === 0 ? (
                  <p className="text-[11px] text-danger">
                    Belum ada kategori aset tetap aktif. Hubungi admin untuk menambahkan
                    kategori terlebih dahulu.
                  </p>
                ) : null}
              </div>

              <Input
                label="Nama Aset"
                required
                placeholder="mis. Laptop Dell Latitude, Toyota Avanza B 1234 XY"
                value={form.asset_name}
                onChange={(e) => set("asset_name", e.target.value)}
                error={errors.asset_name}
              />

              <Input
                label="Masa Manfaat (bulan)"
                required
                type="number"
                min={1}
                value={form.useful_life_months}
                onChange={(e) => set("useful_life_months", e.target.value)}
                error={errors.useful_life_months}
                hint={selectedCategory ? `Bawaan kategori: ${selectedCategory.default_useful_life_months} bulan` : undefined}
              />

              <RupiahInput
                label="Nilai Residu (opsional)"
                value={form.residual_value}
                onChange={(v) => set("residual_value", v)}
                error={errors.residual_value}
                hint="Perkiraan nilai sisa di akhir masa manfaat. Kosongkan = 0"
              />
            </>
          )}

          {/* ── Scope operasional ── */}
          {!isFixedAsset && !isProjectScope && (
            <>
              <Select
                label="Jenis Pengeluaran"
                required
                value={form.expense_type_id}
                onChange={(e) => set("expense_type_id", e.target.value)}
                error={errors.expense_type_id}
              >
                <option value="">Pilih jenis pengeluaran...</option>
                {activeTypes.map((t) => (
                  <option key={t.id} value={String(t.id)}>{t.name}</option>
                ))}
              </Select>
              <div className="flex items-end pb-1">
                {selectedType ? (
                  <p className="text-[11px] text-text-tertiary">
                    Akan didebit ke akun{" "}
                    <span className="font-mono text-text-secondary">{selectedType.expense_account_code}</span>.
                    Akun ditentukan master jenis pengeluaran, bukan diketik di sini.
                  </p>
                ) : activeTypes.length === 0 ? (
                  <p className="text-[11px] text-danger">
                    Belum ada jenis pengeluaran aktif. Tambahkan di master jenis pengeluaran
                    terlebih dahulu.
                  </p>
                ) : null}
              </div>
            </>
          )}

          {/* ── Scope proyek ── */}
          {!isFixedAsset && isProjectScope && (
            <>
              <Select
                label="Proyek"
                required
                value={form.project_id}
                onChange={(e) => set("project_id", e.target.value)}
                error={errors.project_id}
              >
                <option value="">Pilih proyek...</option>
                {projects.map((p) => (
                  <option key={p.id} value={String(p.id)}>{p.name}</option>
                ))}
              </Select>

              <Select
                label="Kategori Biaya"
                required
                value={form.category}
                onChange={(e) => set("category", e.target.value)}
                error={errors.category}
              >
                <option value="">Pilih kategori...</option>
                {PROJECT_CATEGORIES.map((c) => (
                  <option key={c.value} value={c.value}>{c.label}</option>
                ))}
              </Select>
            </>
          )}

          <RupiahInput
            label={isFixedAsset ? "Harga Perolehan" : "Nominal"}
            required
            value={form.amount}
            onChange={(v) => set("amount", v)}
            error={errors.amount}
            // `errors.amount` hanya terisi setelah tombol "Tinjau & Simpan"
            // ditekan, jadi nominal kosong harus tetap terbaca sebagai error —
            // kalau ditelan, tombolnya terasa mati tanpa alasan (F-5).
            showErrorWhenEmpty
            hint="Rupiah bulat, tanpa sen"
          />

          <div>
            <CashBankSelect
              token={token}
              label="Sumber Dana (Kas / Bank)"
              required
              value={form.bank_account_code}
              onChange={(code) => set("bank_account_code", code)}
            />
            {errors.bank_account_code && (
              <p className="mt-1 text-xs text-danger">{errors.bank_account_code}</p>
            )}
          </div>

          <Input
            label="Tanggal"
            required
            type="date"
            value={form.date}
            onChange={(e) => set("date", e.target.value)}
            error={errors.date}
          />

          <Input
            label="Penerima / Pihak"
            required
            placeholder={isProjectScope || isFixedAsset ? "Nama vendor atau kontraktor" : "Nama penerima, mis. PLN"}
            value={form.vendor}
            onChange={(e) => set("vendor", e.target.value)}
            error={errors.vendor}
          />

          <div className="md:col-span-2">
            <Input
              label="Deskripsi"
              required
              placeholder="Uraian singkat pengeluaran"
              value={form.description}
              onChange={(e) => set("description", e.target.value)}
              error={errors.description}
            />
          </div>
        </div>

        {/* ── Pembebanan proyek: dua hal berbeda yang sering dikira sama ── */}
        {!isFixedAsset && isProjectScope && form.project_id && (
          <div className="mt-5 rounded-lg border border-border bg-border-subtle/30 p-4 space-y-4">
            <div>
              <p className="text-sm font-semibold text-text-primary">Pembebanan Rinci (opsional)</p>
              <p className="text-[11px] text-text-tertiary mt-0.5">
                {loadingCtx ? "Memuat unit & RAB proyek…" : "Keduanya opsional dan berbeda tujuan — lihat penjelasan di bawah."}
              </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <Select
                  label="Unit (opsional)"
                  value={form.unit_id}
                  onChange={(e) => set("unit_id", e.target.value)}
                  disabled={units.length === 0}
                >
                  <option value="">— Biaya bersama proyek —</option>
                  {units.map((u) => (
                    <option key={u.id} value={String(u.id)}>{u.code} ({u.unit_type})</option>
                  ))}
                </Select>
                <p className="mt-1 text-[11px] text-text-tertiary">
                  {units.length === 0
                    ? "Proyek ini belum punya unit. Biaya dicatat sebagai biaya bersama."
                    : "Memilih unit menjadikannya biaya langsung unit tersebut. Dikosongkan = biaya bersama yang dialokasikan ke semua unit."}
                </p>
              </div>

              <div>
                <Select
                  label="Item RAB (opsional)"
                  value={form.budget_item_id}
                  onChange={(e) => set("budget_item_id", e.target.value)}
                  disabled={budgetItems.length === 0}
                >
                  <option value="">— Tanpa tautan RAB —</option>
                  {budgetItems.map((b) => (
                    <option key={b.id} value={String(b.id)}>
                      {b.description || b.subcategory || b.category}
                    </option>
                  ))}
                </Select>
                <p className="mt-1 text-[11px] text-text-tertiary">
                  {budgetItems.length === 0
                    ? "Proyek ini belum punya RAB aktif, jadi biaya tidak bisa ditautkan ke item RAB."
                    : `RAB aktif: ${budgetPlanLabel}.`}
                </p>
              </div>
            </div>

            {/* BD-2 — perbedaan yang paling sering salah dipahami, dijelaskan
                di tempat keputusannya diambil, bukan di dokumentasi terpisah. */}
            <div className="rounded-md border border-accent/25 bg-accent-light/40 p-3 text-[11px] leading-relaxed text-text-secondary">
              <p>
                <strong className="text-text-primary">Proyek</strong> menentukan biaya ini
                milik proyek mana — muncul di laporan proyek dan menjadi HPP unit.
              </p>
              <p className="mt-1">
                <strong className="text-text-primary">Item RAB</strong> adalah hal yang
                berbeda: hanya biaya yang ditautkan ke item RAB yang terhitung sebagai
                <strong> realisasi anggaran</strong> pada laporan RAB vs Realisasi.
                Memilih proyek saja <strong>tidak</strong> membuatnya terhitung sebagai realisasi.
              </p>
            </div>
          </div>
        )}

        {/* ── Tag proyek untuk aset tetap (opsional) ── */}
        {isFixedAsset && (
          <div className="mt-5 rounded-lg border border-border bg-border-subtle/30 p-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Select
                label="Tag Proyek (opsional)"
                value={form.project_id}
                onChange={(e) => set("project_id", e.target.value)}
              >
                <option value="">— Aset kantor umum —</option>
                {projects.map((p) => (
                  <option key={p.id} value={String(p.id)}>{p.name}</option>
                ))}
              </Select>
              <p className="text-[11px] text-text-tertiary self-end pb-1 leading-relaxed">
                Tag ini hanya untuk pelaporan per proyek. Aset tetap tidak dikapitalisasi
                sebagai persediaan proyek dan tidak menjadi HPP unit.
              </p>
            </div>
          </div>
        )}

        {/* ── Tag proyek untuk pengeluaran operasional ── */}
        {!isFixedAsset && !isProjectScope && (
          <div className="mt-5 rounded-lg border border-border bg-border-subtle/30 p-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Select
                label="Tag Proyek (opsional)"
                value={form.project_id}
                onChange={(e) => set("project_id", e.target.value)}
                disabled={!typeAllowsProjectTag}
              >
                <option value="">— Biaya kantor umum —</option>
                {projects.map((p) => (
                  <option key={p.id} value={String(p.id)}>{p.name}</option>
                ))}
              </Select>
              <p className="text-[11px] text-text-tertiary self-end pb-1 leading-relaxed">
                {typeAllowsProjectTag ? (
                  <>
                    Tag ini hanya untuk pelaporan per proyek. Biaya tetap menjadi
                    <strong> beban periode</strong>, tidak dikapitalisasi, dan{" "}
                    <strong>tidak</strong> terhitung sebagai realisasi RAB.
                  </>
                ) : (
                  <>
                    Jenis <strong>{selectedType?.name}</strong> memakai akun{" "}
                    <span className="font-mono">{selectedType?.expense_account_code}</span>{" "}
                    yang termasuk taksonomi biaya proyek. Jika di-tag proyek, laporan RAB
                    akan menghitungnya sebagai realisasi anggaran. Catat lewat{" "}
                    <strong>Biaya Proyek</strong> bila memang biaya proyek.
                  </>
                )}
              </p>
            </div>
          </div>
        )}

        <div className="mt-6 flex justify-end">
          <Button onClick={handleTinjau} loading={previewing}>
            Tinjau &amp; Simpan
          </Button>
        </div>
      </Card>

      <ConfirmModal
        open={showConfirm}
        title={isFixedAsset ? "Konfirmasi Perolehan Aset Tetap" : "Konfirmasi Pengeluaran"}
        confirmLabel="Simpan &amp; Posting"
        onConfirm={handleConfirm}
        onCancel={() => {
          setShowConfirm(false);
          setPreview(null);
        }}
        loading={saving}
      >
        <div className="space-y-4 text-sm text-text-secondary">
          <p>
            Jurnal berikut akan <strong>diposting permanen</strong> dan bukti kas keluar
            (BKK) diterbitkan. Koreksi setelah ini hanya bisa lewat jurnal pembalik.
          </p>

          {preview && isFixedAssetPreview(preview) && (
            <div className="rounded border border-border bg-surface p-3 space-y-3">
              <div>
                <p className="text-xs font-semibold text-text-primary mb-2 uppercase tracking-wide">
                  Pratinjau Jurnal
                  <span className="ml-1 font-normal normal-case text-text-tertiary">
                    — dihitung backend, bukan frontend
                  </span>
                </p>
                <div className="flex items-center gap-2 text-sm py-0.5">
                  <span className="w-5 text-xs font-bold shrink-0 text-success">D</span>
                  <span className="font-mono text-xs text-text-tertiary w-16 shrink-0">{preview.debit_account_code}</span>
                  <span className="flex-1 text-text-primary">Aset Tetap — {form.asset_name}</span>
                  <span className="font-medium tabular-nums text-text-primary">
                    Rp {parseInt(preview.amount, 10).toLocaleString("id-ID")}
                  </span>
                </div>
                <div className="flex items-center gap-2 text-sm py-0.5 pl-4">
                  <span className="w-5 text-xs font-bold shrink-0 text-danger">K</span>
                  <span className="font-mono text-xs text-text-tertiary w-16 shrink-0">{preview.credit_account_code}</span>
                  <span className="flex-1 text-text-primary">Sumber dana</span>
                  <span className="font-medium tabular-nums text-text-primary">
                    Rp {parseInt(preview.amount, 10).toLocaleString("id-ID")}
                  </span>
                </div>
              </div>
              <div className="rounded-md border border-accent/25 bg-accent-light/40 p-3 text-[11px] leading-relaxed text-text-secondary">
                <p>
                  Nilai disusutkan (harga perolehan − residu):{" "}
                  <strong className="text-text-primary">
                    Rp {parseInt(preview.depreciable_amount, 10).toLocaleString("id-ID")}
                  </strong>
                </p>
                <p className="mt-1">
                  Estimasi penyusutan bulanan (garis lurus, {preview.depreciation_schedule.length} bulan):{" "}
                  <strong className="text-text-primary">
                    Rp {parseInt(preview.monthly_depreciation_indicative, 10).toLocaleString("id-ID")}
                  </strong>{" "}
                  / bulan.
                </p>
              </div>
            </div>
          )}

          {preview && !isFixedAssetPreview(preview) && preview.lines.length > 0 && (
            <div className="rounded border border-border bg-surface p-3">
              <p className="text-xs font-semibold text-text-primary mb-2 uppercase tracking-wide">
                Pratinjau Jurnal
                <span className="ml-1 font-normal normal-case text-text-tertiary">
                  — dihitung backend, bukan frontend
                </span>
              </p>
              {preview.lines.map((line, i) => (
                <PreviewLine key={i} line={line} />
              ))}
            </div>
          )}

          <p className="text-xs text-text-tertiary">
            Penerima: <strong>{form.vendor}</strong> · Tanggal: <strong>{form.date}</strong>
          </p>
        </div>
      </ConfirmModal>
    </>
  );
}

// ── Sub-komponen ────────────────────────────────────────────────────────────

function ScopeCard({
  active, onClick, title, desc,
}: { active: boolean; onClick: () => void; title: string; desc: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`text-left rounded-lg border p-3 transition-colors ${
        active
          ? "border-accent bg-accent-light/50"
          : "border-border bg-surface hover:border-accent/40"
      }`}
    >
      <span className={`block text-sm font-semibold ${active ? "text-accent" : "text-text-primary"}`}>
        {title}
      </span>
      <span className="block text-[11px] text-text-tertiary mt-0.5 leading-relaxed">{desc}</span>
    </button>
  );
}

function PreviewLine({ line }: { line: JournalPreviewLine }) {
  const isCredit = Number(line.credit) > 0;
  const raw = isCredit ? line.credit : line.debit;
  const amount = raw ? `Rp ${parseInt(raw, 10).toLocaleString("id-ID")}` : "Rp —";
  return (
    <div className={`flex items-center gap-2 text-sm py-0.5 ${isCredit ? "pl-4" : ""}`}>
      <span className={`w-5 text-xs font-bold shrink-0 ${isCredit ? "text-danger" : "text-success"}`}>
        {isCredit ? "K" : "D"}
      </span>
      <span className="font-mono text-xs text-text-tertiary w-16 shrink-0">{line.account_code}</span>
      <span className="flex-1 text-text-primary">{line.account_name}</span>
      <span className="font-medium tabular-nums text-text-primary">{amount}</span>
    </div>
  );
}

// FixedAssetSuccessPanel: bentuk balasan create untuk purchase_type=fixed_asset
// adalah baris Register (FixedAsset), bukan ExpenseListItem — tidak ada nomor
// BKK/status di sini karena itu milik jalur cost_entries, bukan fixed_assets.
function FixedAssetSuccessPanel({
  asset, onDismiss,
}: { asset: FixedAsset; onDismiss: () => void }) {
  return (
    <Card className="border-success/40 bg-success-bg/40">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-success">Aset tetap tercatat &amp; jurnal diposting</p>
          <dl className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1 text-xs">
            <Fact label="Kode Aset" value={asset.asset_code} mono />
            <Fact label="Referensi Jurnal" value={`JE-${asset.acquisition_journal_id}`} mono />
            <Fact label="Nama Aset" value={asset.asset_name} />
            <Fact
              label="Harga Perolehan"
              value={`Rp ${parseInt(asset.acquisition_cost, 10).toLocaleString("id-ID")}`}
            />
            <Fact label="Masa Manfaat" value={`${asset.useful_life_months} bulan`} />
            <Fact label="Metode Penyusutan" value="Garis Lurus" />
          </dl>
          <a
            href="/accounting/aset-tetap"
            className="mt-3 inline-block text-xs font-medium text-accent hover:underline"
          >
            Lihat di Buku Aset Tetap →
          </a>
        </div>
        <button
          type="button"
          onClick={onDismiss}
          className="text-xs text-text-tertiary hover:text-text-primary shrink-0"
        >
          Tutup
        </button>
      </div>
    </Card>
  );
}

// SuccessPanel: bukti akuntansi ditampilkan langsung — nomor BKK, referensi
// jurnal, dan dampak proyek/RAB. User tidak perlu membuka modul lain untuk
// memastikan transaksinya benar-benar tercatat.
function SuccessPanel({ item, onDismiss }: { item: ExpenseListItem; onDismiss: () => void }) {
  return (
    <Card className="border-success/40 bg-success-bg/40">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-success">Pengeluaran tercatat &amp; jurnal diposting</p>
          <dl className="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1 text-xs">
            <Fact label="Bukti Kas Keluar" value={item.document_number || "—"} mono />
            <Fact label="Referensi Jurnal" value={`JE-${item.journal_entry_id}`} mono />
            <Fact
              label="Nominal"
              value={`Rp ${parseInt(item.amount, 10).toLocaleString("id-ID")}`}
            />
            <Fact label="Sumber Dana" value={item.bank_account_name || item.bank_account_code || "—"} />
            <Fact
              label="Proyek"
              value={item.project_name || "Biaya kantor umum (tanpa proyek)"}
            />
            <Fact
              label="Realisasi RAB"
              value={item.is_rab_realization ? "Ya — tertaut item RAB" : "Tidak tertaut item RAB"}
            />
          </dl>
          {item.document_number && (
            <a
              href="/accounting/dokumen"
              className="mt-3 inline-block text-xs font-medium text-accent hover:underline"
            >
              Lihat di Buku Dokumen →
            </a>
          )}
        </div>
        <button
          type="button"
          onClick={onDismiss}
          className="text-xs text-text-tertiary hover:text-text-primary shrink-0"
        >
          Tutup
        </button>
      </div>
    </Card>
  );
}

function Fact({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex gap-2">
      <dt className="text-text-tertiary shrink-0">{label}:</dt>
      <dd className={`text-text-primary font-medium truncate ${mono ? "font-mono" : ""}`}>{value}</dd>
    </div>
  );
}
