// ── Ledger: Chart of Accounts ─────────────────────────────────────────────────

export type AccountType    = "asset" | "liability" | "equity" | "revenue" | "expense";
export type NormalBalance  = "debit" | "credit";
export type AccountCategory = "cash" | "bank" | "other_asset" | "";

export interface Account {
  id: number;
  code: string;
  name: string;
  type: AccountType;
  normal_balance: NormalBalance;
  is_system: boolean;
  is_active: boolean;
  account_category?: AccountCategory;
  description?: string;
  created_at: string;
  updated_at: string;
}

// ── Ledger: Journal Entry ─────────────────────────────────────────────────────

export interface JournalLine {
  id: number;
  journal_entry_id: number;
  account_id: number;
  account?: Account;
  debit: string;
  credit: string;
  project_id?: number;
  phase_id?: number;
  unit_id?: number;
  description?: string;
  created_at: string;
}

export type JournalSource = "manual" | "system" | "reversal" | "cost" | "sale" | "tax" | "termin" | "opening_balance";

export interface JournalEntry {
  id: number;
  date: string;
  description: string;
  reference?: string;
  posted_at?: string;
  is_reversing: boolean;
  reverses_id?: number;
  source: JournalSource;
  created_by?: number;
  lines?: JournalLine[];
  // S9/R-9: total dihitung backend (endpoint detail) — FE tidak menjumlah lagi.
  total_debit?: string;
  total_credit?: string;
  // W-3.6 (INV-DOC-1): bukti bernomor yang membuktikan jurnal ini. Kosong =
  // jurnal non-kas, saldo awal (satu-satunya kas yang dikecualikan), atau draft
  // yang belum diposting — nomor lahir saat posting, bukan saat draft dibuat.
  document_number?: string;
  document_type_code?: string;
  document_type_name?: string;
  document_issued_at?: string;
  created_at: string;
  updated_at: string;
}

// JournalSummary is what the list endpoint returns (aggregate totals, no lines).
export interface JournalSummary {
  id: number;
  date: string;
  description: string;
  reference?: string;
  posted_at?: string;
  is_reversing: boolean;
  reverses_id?: number;
  source: JournalSource;
  created_by?: number;
  total_debit: string;
  total_credit: string;
  // W-3.6: nomor bukti, ikut di ringkasan lewat LEFT JOIN — bukan fetch per baris.
  document_number?: string;
  created_at: string;
  updated_at: string;
}

// ── Ledger: Accounting Period ─────────────────────────────────────────────────

export type PeriodStatus = "open" | "closed";

export interface AccountingPeriod {
  id?: number;  // absent for virtual (auto-merged) periods
  year: number;
  month: number;
  status: PeriodStatus;
  closed_at?: string;
  closed_by?: number;
  created_at?: string;
  updated_at?: string;
}

// ── Tutup Buku Tahunan (Year-End Closing) ─────────────────────────────────────

export interface ClosingAccountLine {
  code: string;
  name: string;
  amount: string;
}

export interface ClosingPreview {
  year: number;
  as_of: string;
  already_closed: boolean;
  can_close: boolean;
  total_revenue: string;
  total_expense: string;
  net_income: string; // positif = laba; negatif = rugi
  revenue_accounts: ClosingAccountLine[];
  expense_accounts: ClosingAccountLine[];
}

export interface ClosingResult {
  year: number;
  as_of: string;
  total_revenue: string;
  total_expense: string;
  net_income: string;
  income_summary_journal_id: number;
  retained_earnings_journal_id?: number;
}

// ── Auth ──────────────────────────────────────────────────────────────────────

/** Role sistem. Cocok dengan domain.Role di backend (internal/domain/role.go). */
export type Role = "owner" | "accountant" | "marketing" | "viewer";

export interface User {
  id: number;
  tenant_id: number;
  email: string;
  role: Role;
  /** Nama lengkap. Kosong untuk user yang dibuat sebelum W-12 — jangan dikarang. */
  name?: string;
  /** Nama untuk ditampilkan; backend sudah mem-fallback ke email bila nama kosong. */
  display_name?: string;
  tenant_name?: string;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export interface Session {
  user: User;
}

// ── Proyek ────────────────────────────────────────────────────────────────────

export type ProjectStatus = "planning" | "active" | "selling" | "completed";
export type PhaseStatus   = "planning" | "active" | "completed";
// Increment 6 — Unit Lifecycle: 9 status (blueprint §9). Tiga nilai lama tetap.
export type UnitStatus =
  | "available"
  | "booked"
  | "reserved"
  | "ppjb"
  | "sold"
  | "occupied"
  | "hold"
  | "blocked"
  | "maintenance";

export interface Project {
  id: number;
  name: string;
  status: ProjectStatus;
  start_date?: string;
  land_area: string;
  notes?: string;
  created_at: string;
  updated_at: string;
}

export interface ProjectPhase {
  id: number;
  project_id: number;
  name: string;
  description?: string;
  target_units: number;
  status: PhaseStatus;
  created_at: string;
  updated_at: string;
}

export interface Unit {
  id: number;
  project_id: number;
  phase_id?: number;
  code: string;
  unit_type: string;      // kode product type (master Katalog Produk)
  type_label?: string;    // label komersial, mis. "36/72"
  saleable_area: string;
  /**
   * Luas tanah unit (m²), TERPISAH dari saleable_area (luas bangunan).
   * Default "0.0000" — tidak backfill otomatis; diisi eksplisit admin lewat
   * updateUnitLandArea. 0 berarti belum diisi, bukan error.
   */
  land_area: string;
  list_price: string;
  status: UnitStatus;
  buyer_ref?: string;
  /**
   * Nama pemegang unit — diturunkan backend dari kontrak → booking aktif →
   * buyer_ref (lihat backend/internal/project/buyer_name.go). Kosong berarti
   * unit belum dipesan siapa pun; JANGAN menyusun ulang dari `buyer_ref`
   * karena `buyer_ref` baru terisi saat BAST.
   */
  buyer_name?: string;
  /** "contract" | "booking" | "unit" — asal `buyer_name`, untuk label layar. */
  buyer_source?: string;
  sale_date?: string;
  sale_price?: string;
  created_at: string;
  updated_at: string;
}

// ── RAB (Budget) ──────────────────────────────────────────────────────────────
// Semua angka uang = string dari API; frontend TIDAK menghitung.

export type BudgetPlanStatus = "draft" | "active" | "superseded";

// 6 kategori untuk BudgetItem (RAB). marketing + other = expense-only (bukan Persediaan).
export type BudgetCategory =
  | "land"
  | "construction"
  | "soft"
  | "financing"
  | "marketing"
  | "other";

export interface BudgetPlan {
  id: number;
  project_id: number;
  phase_id?: number;
  version: number;
  label: string;
  status: BudgetPlanStatus;
  notes?: string;
  approved_at?: string;
  approved_by?: string;
  created_at: string;
  updated_at: string;
}

export interface BudgetItem {
  id: number;
  budget_plan_id: number;
  category: BudgetCategory;
  subcategory?: string;
  description?: string;
  budgeted_amount: string;
  created_at: string;
  updated_at: string;
}

export interface RABvsRealisasiRow {
  category: BudgetCategory;
  budgeted: string;
  realisasi: string;
  selisih: string;
  persen_realisasi: string;
}

export interface RABvsRealisasiReport {
  plan_id: number;
  plan_label: string;
  plan_version: number;
  project_id: number;
  phase_id?: number;
  rows: RABvsRealisasiRow[];
  total_budgeted: string;
  total_realisasi: string;
  total_selisih: string;
  // S9/R-9: pemakaian anggaran + status dihitung backend.
  usage_pct: string;
  status: "sehat" | "waspada" | "over";
}

// ── Biaya (Cost Entry) ────────────────────────────────────────────────────────
// 4 kategori valid untuk CostEntry (backend menolak marketing/other di endpoint ini).
// marketing/other adalah expense-only, dicatat melalui jurnal manual terpisah.
export type CostCategory = "land" | "hard" | "soft" | "financing";
export type PaymentMethod = "bank" | "payable";

export interface CostEntry {
  id: number;
  project_id: number;
  unit_id?: number;
  phase_id?: number;
  category: CostCategory;
  amount: string;
  payment_method: PaymentMethod;
  bank_account_code?: string;
  date: string;
  vendor: string;
  description: string;
  journal_entry_id: number;
  budget_item_id?: number;
  created_at: string;
  updated_at: string;
}

// ── Preview Jurnal (dari backend /cost-entries/preview) ──────────────────────
// Frontend TIDAK menghitung kode akun — backend mengembalikan baris jurnal yang
// tepat (kode, nama, debit, kredit) menggunakan logika yang SAMA dengan create.

export interface JournalPreviewLine {
  account_code: string;
  account_name: string;
  debit: string;   // "0" jika sisi kredit
  credit: string;  // "0" jika sisi debit
}

export interface CostPreviewResponse {
  lines: JournalPreviewLine[];
}

// ── Transaksi Pengeluaran (W-10) ─────────────────────────────────────────────
// Satu pintu masuk untuk pengeluaran kantor maupun pengeluaran proyek. Bentuk
// di bawah mengikuti backend apa adanya — frontend tidak menyimpulkan aturan
// akuntansi apa pun sendiri.

export type ExpenseScope = "operasional" | "proyek";
export type ExpenseStatus = "draft" | "posted" | "reversed";

// ExpenseType: master jenis pengeluaran (tenant-scoped). `expense_account_code`
// menentukan akun beban yang terdebit.
export interface ExpenseType {
  id: number;
  code: string;
  name: string;
  expense_account_code: string;
  is_active: boolean;
  // allows_project_tag: INV-EXP-2 — datang dari backend sebagai DATA. Jenis yang
  // akunnya termasuk taksonomi biaya proyek tidak boleh di-tag proyek, karena
  // tag itu akan terbaca sebagai realisasi RAB per kategori.
  allows_project_tag: boolean;
}

export interface ExpenseListItem {
  id: number;
  date: string;
  amount: string;
  vendor: string;
  description: string;

  scope: ExpenseScope;
  expense_type_id?: number;
  expense_type_name?: string;
  category: string;
  cost_tier: string;

  project_id?: number;
  project_name?: string;
  unit_id?: number;
  unit_code?: string;
  budget_item_id?: number;
  // BD-2 di layar: tag proyek saja BUKAN realisasi RAB.
  is_rab_realization: boolean;

  payment_method: string;
  bank_account_code?: string;
  bank_account_name?: string;

  journal_entry_id: number;
  document_number?: string;
  status: ExpenseStatus;
}

// ── Aset Tetap ────────────────────────────────────────────────────────────────
// Perolehan lewat toggle "Jenis Pembelian" di form Pengeluaran (satu pintu
// masuk yang sama, bukan endpoint terpisah) — lihat ExpenseBody.purchase_type.

export type FixedAssetDepreciationMethod = "straight_line";
export type FixedAssetStatus = "active" | "disposed";

export interface FixedAssetCategory {
  id: number;
  code: string;
  name: string;
  asset_account_code: string;
  accumulated_depreciation_account_code: string;
  depreciation_expense_account_code: string;
  default_useful_life_months: number;
  is_active: boolean;
}

// accumulated_depreciation & book_value SENGAJA opsional: hanya hadir di
// respons Register (list/get), diturunkan backend saat baca — tidak pernah
// dikirim balik saat perolehan (create).
export interface FixedAsset {
  id: number;
  category_id: number;
  project_id?: number;
  asset_code: string;
  asset_name: string;
  acquisition_date: string;
  acquisition_cost: string;
  residual_value: string;
  useful_life_months: number;
  depreciation_method: FixedAssetDepreciationMethod;
  depreciation_start_date: string;
  status: FixedAssetStatus;
  vendor?: string;
  description?: string;
  acquisition_journal_id: number;
  accumulated_depreciation?: string;
  book_value?: string;
}

export interface FixedAssetAcquisitionPreview {
  debit_account_code: string;
  credit_account_code: string;
  amount: string;
  depreciable_amount: string;
  monthly_depreciation_indicative: string;
  depreciation_schedule: string[];
}

export interface DepreciationRunLine {
  asset_id: number;
  amount?: string;
  reason?: string;
}

export interface DepreciationRunResult {
  posted: DepreciationRunLine[] | null;
  skipped: DepreciationRunLine[] | null;
}

// ── Penjualan ─────────────────────────────────────────────────────────────────

export type SalePaymentType  = "kpr" | "tunai";
export type ScheduleType     = "dp" | "installment" | "final";
export type ScheduleStatus   = "scheduled" | "received" | "overdue";

export interface SaleRecord {
  id: number;
  unit_id: number;
  project_id: number;
  phase_id?: number;
  sale_price: string;
  is_vat: boolean;
  vat_rate: string;
  total_advance_at_bast: string;
  hpp_land: string;
  hpp_hard: string;
  hpp_soft: string;
  hpp_financing: string;
  // S9/R-9: agregat derived dari backend — FE tidak menghitung ulang.
  vat_amount: string;
  hpp_total: string;
  // Temuan #5: basis penurunan HPP saat diakui — "actual" | "budgeted" | "finalized" | "none".
  hpp_method: string;
  buyer_ref: string;
  // Temuan #7: tanggal Akad — saat pendapatan+HPP diakui (dulu bast_date).
  recognition_date: string;
  // Temuan #7: tanggal serah terima fisik (opsional, tanpa jurnal).
  handed_over_at?: string;
  revenue_journal_id: number;
  cogs_journal_id?: number;
  created_at: string;
  updated_at: string;
}

export interface SaleContract {
  id: number;
  unit_id: number;
  buyer_name: string;
  buyer_id: string;
  payment_type: SalePaymentType;
  bank_kpr?: string;
  loan_amount?: string;
  contract_date: string;
  dpp_amount: string;
  is_pkp: boolean;
  vat_rate_snapshot: string;
  gross_amount: string;
  total_price: string;
  customer_id?: number;
  sales_person_id?: number;
  /** P1 — Admin Marketing menangani admin/dokumen/KPR; berbeda orang dari Sales (komisi selalu ke sales_person_id). */
  admin_marketing_person_id?: number;
  payment_scheme_id?: number;
  financing_source_id?: number;
  scheme_state?: string; // signed|dp_paid|submitted_to_bank|bank_approved|bank_rejected|akad|disbursed|handed_over|…
  created_at: string;
  updated_at: string;
  /** Produk Tambahan: Kelebihan Tanah — hadir hanya bila kontrak ini menyertakannya. */
  land_stock_id?: number;
  land_reservation_id?: number;
  land_quantity_m2?: string;
  land_unit_price_snapshot?: string;
}

// ── Billing: Invoice ──────────────────────────────────────────────────────────

export type InvoiceType   = "DP" | "TERMIN" | "PELUNASAN" | "KEKURANGAN";
export type InvoiceStatus = "issued" | "paid" | "overdue" | "cancelled";

export interface Invoice {
  id: number;
  sale_contract_id: number;
  schedule_id?: number;
  invoice_number: string;
  invoice_type: InvoiceType;
  issue_date: string;
  due_date: string;
  amount: string;
  status: InvoiceStatus;
  notes?: string;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface InvoiceSummary extends Invoice {
  buyer_name: string;
  unit_code: string;
}

/** W-13: label bisnis terstruktur "Jenis Penerimaan" — lihat backend internal/sale/model.go. */
export type TerminKind = "dp" | "installment" | "final_payment" | "other" | "bank_disbursement";

export interface TerminPayment {
  id: number;
  unit_id: number;
  project_id: number;
  phase_id?: number;
  amount: string;
  bank_account_code: string;
  date: string;
  description: string;
  journal_entry_id: number;
  /** "2-2000" (Uang Muka, sebelum BAST) atau "1-2000" (Piutang, setelah BAST). */
  credit_account_code?: string;
  /** booking_fee | realization | kpr_disbursement | collection | … — SoT tipe kwitansi. */
  payment_source?: string;
  financing_source_id?: number;
  /** W-13: Jenis Penerimaan terstruktur — selalu terisi (default "other" di DB). */
  kind?: TerminKind;
  installment_no?: number;
  created_at: string;
  updated_at: string;
}

export interface PaymentSchedule {
  id: number;
  sale_contract_id: number;
  unit_id: number;
  installment_number: number;
  due_date: string;
  amount: string;
  paid_amount: string; // DECIMAL(20,4) dari backend; sisa = amount − paid_amount (eksak, bukan float)
  type: ScheduleType;
  status: ScheduleStatus;
  termin_payment_id?: number;
  received_at?: string;
  created_at: string;
  updated_at: string;
}

export interface BuyerBalance {
  remaining_balance: string;
}

// ── Alokasi Biaya ─────────────────────────────────────────────────────────────

export type AllocationBasis = "saleable_area" | "sales_value";

export interface AllocationConfig {
  id: number;
  project_id: number;
  basis: AllocationBasis;
  created_at: string;
  updated_at: string;
}

export interface AllocationExecution {
  id: number;
  project_id: number;
  basis: AllocationBasis;
  executed_by: number;
  user_email: string;
  total_cost: string;
  executed_at: string;
  created_at: string;
  updated_at: string;
}

export interface BreakdownResponse {
  land: string;
  hard: string;
  soft: string;
  financing: string;
  total: string;
}

export interface AllocationResult {
  unit_id: number;
  direct: BreakdownResponse;
  allocated: BreakdownResponse;
  total: BreakdownResponse;
  // S9/R-9: porsi unit thd grand total (%) dari backend.
  share_pct: string;
}

// S9/R-9: roll-up alokasi dihitung backend — FE display saja.
export interface AllocationTotals {
  direct: string;
  allocated: string;
  total: string;
}

export interface AllocationComputeResponse {
  data: AllocationResult[];
  totals: AllocationTotals;
}

// ── Reports ───────────────────────────────────────────────────────────────────

export interface PipelineUnitStat {
  count: number;
  total_list_price: string;
  total_advance?: string;
  total_contract_value?: string;
}

export interface PipelineReport {
  available: PipelineUnitStat;
  reserved: PipelineUnitStat;
  sold: PipelineUnitStat;
  total_units: number;
  projected_revenue: string;
}

export interface NeracaLine {
  code: string;
  name: string;
  amount: string;
}

export interface NeracaReport {
  as_of: string;
  aset: NeracaLine[];
  kewajiban: NeracaLine[];
  ekuitas: NeracaLine[];
  total_aset: string;
  total_kewajiban: string;
  total_ekuitas: string;
  /** total_ekuitas + laba berjalan — angka "Total Ekuitas" yang benar untuk display */
  total_ekuitas_efektif?: string;
  laba_rugi_tahun_berjalan: string;
  total_kewajiban_ekuitas: string;
  is_balanced: boolean;
}

export interface PLLine {
  code: string;
  name: string;
  amount: string;
}

// PLReport — struktur Laba Rugi 8-bagian (item B, 2026-08-27):
// Pendapatan (-) HPP = Laba Kotor; (-) Beban Operasional = Laba Operasional;
// + Pendapatan Luar Usaha (-) Beban Luar Usaha = Laba Bersih Sebelum Pajak;
// (-) Beban Pajak = Laba Bersih Setelah Pajak (laba_rugi_bersih).
export interface PLReport {
  as_of: string;
  project_id?: number;

  pendapatan: PLLine[];
  total_pendapatan: string;

  hpp: PLLine[];
  total_hpp: string;

  laba_kotor: string;

  beban_operasional: PLLine[];
  total_beban_operasional: string;

  laba_operasional: string;

  pendapatan_luar_usaha: PLLine[];
  total_pendapatan_luar_usaha: string;

  beban_luar_usaha: PLLine[];
  total_beban_luar_usaha: string;

  laba_bersih_sebelum_pajak: string;

  beban_pajak: PLLine[];
  total_beban_pajak: string;

  laba_rugi_bersih: string;
}

export interface TaxLiabilityRow {
  id: number;
  unit_id?: number;
  rate_code: string;
  transfer_value: string;
  rate: string;
  tax_amount: string;
  status: "outstanding" | "paid";
  accrual_date: string;
}

export interface TaxLiabilityReport {
  period_from: string;
  period_to: string;
  items: TaxLiabilityRow[];
  total_obligation: string;
  total_paid: string;
  total_outstanding: string;
}

// ── Piutang Customer (AR Aging) ───────────────────────────────────────────────

export type ARAgingBucket = "current" | "1_30" | "31_60" | "61_90" | "90_plus";

// Status pembayaran dihitung di backend (bukan hanya frontend).
export type PaymentStatus = "scheduled" | "due_today" | "overdue" | "paid";

// W-4: sumber eksposur piutang. "house" = cicilan harga rumah,
// "realization" = tagihan biaya realisasi (notaris/PDAM/listrik/BPHTB).
// Keduanya ditagih ke orang yang sama, jadi tampil di satu daftar.
// W-7: `legacy` adalah sumber KETIGA pada mesin piutang yang sama, bukan
// laporan tersendiri. Baris legacy tidak punya contract_id/unit_id — proyeknya
// memang tidak ada di sistem — sehingga layar harus membaca `ref_id`.
export type ReceivableSource = "house" | "realization" | "legacy";

export interface ARAgingRow {
  source: ReceivableSource;
  ref_id?: number;        // payment_schedules.id atau charge_items.id
  contract_id: number;
  unit_id: number;
  buyer_name: string;
  unit_code: string;
  label?: string;         // keterangan tagihan: "Termin 3", "Biaya Realisasi / PDAM"
  invoice_number: string; // "—" bila belum ditagih
  buyer_phone?: string;   // PS-4: aksi reminder WA
  buyer_email?: string;
  due_date: string;       // YYYY-MM-DD
  amount: string;
  paid: string;
  outstanding: string;
  days_overdue: number;
  bucket: ARAgingBucket;
  status: PaymentStatus;  // scheduled | due_today | overdue
  due_this_week: boolean; // jatuh tempo dalam 7 hari ke depan (termasuk hari ini)
}

export interface ARAgingBucketSummary {
  count: number;
  total: string;
}

// Subtotal per sumber, dihitung server (W-4). Frontend TIDAK menjumlahkan
// baris sendiri — dua layar yang menjumlahkan sendiri cepat atau lambat berbeda.
export interface ARAgingSourceSummary {
  source: ReceivableSource;
  count: number;
  outstanding: string;
  overdue: string;
}

export interface ARAgingReport {
  as_of: string;
  rows: ARAgingRow[];
  total_piutang: string;
  current_due: string;
  overdue: string;
  due_this_week: string;
  collection_rate: string; // persen, mis. "73.50"
  // PS-4 — Collection Command Center
  due_today_count: number;
  due_today_amount: string;
  expected_30: string; // prediksi kas masuk 30 hari (kumulatif)
  expected_60: string;
  expected_90: string;
  total_scheduled: string;
  total_collected: string;
  by_source?: ARAgingSourceSummary[]; // kosong bila tidak ada tunggakan sama sekali
  buckets: {
    current: ARAgingBucketSummary;
    b1_30: ARAgingBucketSummary;
    b31_60: ARAgingBucketSummary;
    b61_90: ARAgingBucketSummary;
    b90_plus: ARAgingBucketSummary;
  };
}

// ── Customer Statement (rekening koran buyer) ─────────────────────────────────

export interface StatementScheduleLine {
  schedule_id: number;
  installment_number: number;
  type: ScheduleType;
  due_date: string;
  amount: string;
  // T-5: paid/outstanding memakai definisi yang PERSIS sama dengan AR Aging,
  // sehingga pembeli tidak pernah melihat dua angka tunggakan berbeda.
  paid: string;
  outstanding: string;
  status: PaymentStatus; // paid | overdue | due_today | scheduled (dihitung backend)
  received_at?: string;
  termin_payment_id?: number; // untuk cetak kwitansi
  overdue: boolean;
  days_overdue: number;
}

// ── Collection Transaction Flow ───────────────────────────────────────────────

export interface CollectionPreviewLine {
  account_code: string;
  account_name: string;
  debit: string;
  credit: string;
}

export interface PreviewAllocationLine {
  schedule_id: number;
  label: string;       // "Uang Muka (DP)", "Termin 2", ...
  due_date: string;
  amount: string;
  fully_paid: boolean; // true=Lunas, false=Sebagian
}

export interface CollectionPreview {
  contract_id: number;
  unit_id: number;
  is_bast: boolean;
  outstanding: string;
  amount: string;
  valid: boolean;
  reason?: string;
  lines: CollectionPreviewLine[];   // baris jurnal
  applied: PreviewAllocationLine[]; // dialokasikan ke tagihan
  // S8: buyer_credit = PROYEKSI saldo kredit kanonik setelah pembayaran ini;
  // overpayment_unapplied = sisa TRANSAKSI INI yang tak teralokasi.
  buyer_credit: string;
  overpayment_unapplied: string;
  // true bila overpayment_unapplied akan tercatat sbg saldo kredit buyer
  // (dana bebas); false bila sudah mengurangi piutang/pembiayaan riil
  // pasca-BAST (bukan kelebihan bayar meski tak cocok jadwal).
  unapplied_is_credit: boolean;
}

export interface AppliedSchedule {
  schedule_id: number;
  applied: string;
  paid_total: string;
  fully_paid: boolean;
}

export interface CollectionPaymentResult {
  termin_id: number;
  unit_id: number;
  credit_account: string;
  remaining_balance: string;
  applied_schedules: AppliedSchedule[];
  // S8: buyer_credit = saldo kredit KANONIK terakumulasi pasca commit;
  // overpayment_unapplied = sisa transaksi ini yang tak teralokasi.
  buyer_credit: string;
  overpayment_unapplied: string;
  already_existed: boolean;
  receipt_number?: string;
  receipt_id?: number;
}

// ── Receipt (Kwitansi) ────────────────────────────────────────────────────────

export interface Receipt {
  id: number;
  termin_payment_id: number;
  unit_id: number;
  sale_contract_id?: number;
  receipt_number: string;
  amount: string;
  bank_account_code: string;
  received_at: string;
  notes?: string;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface CustomerStatement {
  contract_id: number;
  unit_id: number;
  buyer_name: string;
  buyer_id: string;
  payment_type: SalePaymentType;
  contract_date: string;
  as_of: string;
  contract_value: string;
  dpp_amount: string;
  is_pkp: boolean;
  total_scheduled: string;
  total_paid: string;
  remaining_balance: string;
  total_overdue: string;
  overdue_count: number;
  schedules: StatementScheduleLine[];
  // ── R2 Statement 360 (additive) ──
  bank_kpr?: string;
  loan_amount?: string;
  scheme_state?: string;
  summary?: {
    contract_id: number;
    unit_id: number;
    unit_price: string;
    price_is_snapshot: boolean;
    discount: string;
    dpp: string;
    net_contract: string;
    total_paid: string;
    outstanding: string;
  };
  payments?: StatementPaymentLine[];
  timeline?: StatementTimelineEvent[];
  exposure?: StatementExposure;
}

// W-4 — SATU angka "customer ini masih ditagih berapa": harga rumah dan biaya
// realisasi sudah dijumlahkan DI SERVER, memakai mesin yang sama dengan AR Aging.
export interface StatementExposure {
  house_outstanding: string;
  house_overdue: string;
  realization_outstanding: string;
  realization_overdue: string;
  total_outstanding: string;
  total_overdue: string;
  // false = sumber tagihan realisasi tidak terbaca. WAJIB dibedakan dari nol —
  // menampilkan Rp0 untuk data yang tidak diketahui adalah berbohong.
  realization_available: boolean;
}

// R2 — satu penerimaan dalam perjalanan customer (per sumber).
export interface StatementPaymentLine {
  termin_id: number;
  date: string;
  amount: string;
  source: string; // booking_fee | unit_termin | collection | schedule_received | kpr_disbursement
  bank_account_code?: string;
  reference?: string;
  financing_source_id?: number;
  // R4: false = pembayaran DI LUAR harga unit (booking fee kebijakan baru) —
  // ditampilkan terpisah, TIDAK masuk "Total Diterima" harga.
  counts_toward_price: boolean;
}

// R2 — event lifecycle kontrak (audit trail milestone).
export interface StatementTimelineEvent {
  id: number;
  sale_contract_id: number;
  event: string;
  from_state: string;
  to_state: string;
  event_date: string;
  financing_source_id?: number;
  journal_entry_id?: number;
  notes?: string;
  created_at: string;
}

// FE-2 · P4 — satu baris sub-ledger alokasi (termin → cicilan / saldo kredit).
export interface AllocationView {
  id: number;
  termin_payment_id: number;
  payment_schedule_id?: number | null;
  allocation_type: "schedule" | "buyer_credit" | "direct";
  amount: string;
  label: string;
  installment_number?: number | null;
  termin_date: string;
  receipt_number?: string;
}

export interface DashboardData {
  neraca: NeracaReport | null;
  pl: PLReport | null;
  pipeline: PipelineReport | null;
  tax: TaxLiabilityReport | null;
}

// ── Arus Kas (Cash Flow) ──────────────────────────────────────────────────────

export interface ArusKasLine {
  description: string;
  amount: string; // positif = kas masuk; negatif = kas keluar
}

export interface ArusKasSection {
  lines: ArusKasLine[];
  net: string;
}

export interface ArusKasReport {
  period_from: string;
  period_to: string;
  operasi: ArusKasSection;
  investasi: ArusKasSection;
  pendanaan: ArusKasSection;
  net_change: string;
}

// ── Neraca Saldo (Trial Balance) & Buku Besar (General Ledger) ───────────────

export interface TrialBalanceRow {
  account_id: number;
  account_code: string;
  account_name: string;
  account_type: "asset" | "liability" | "equity" | "revenue" | "expense";
  total_debit: string;
  total_credit: string;
  balance: string;
}

export interface TrialBalance {
  as_of: string;
  rows: TrialBalanceRow[];
  total_debit: string;
  total_credit: string;
}

export interface LedgerEntry {
  entry_id: number;
  date: string;
  reference: string;
  description: string;
  debit: string;
  credit: string;
  balance: string;
}

// ── Pajak: unit BAST tanpa akrual PPh Final ───────────────────────────────────

export interface BASTWithoutPPhItem {
  unit_id: number;
}

// ── Pajak: Obligation, Payment, Reports ───────────────────────────────────────

export interface TaxObligation {
  id: number;
  unit_id?: number;
  project_id?: number;
  rate_code: string;
  transfer_value: string;
  rate: string;        // decimal string e.g. "0.025000"
  tax_amount: string;
  status: "outstanding" | "paid";
  accrual_date: string;
  journal_entry_id: number;
  created_at: string;
  updated_at: string;
}

export interface TaxPayment {
  id: number;
  obligation_id: number;
  bank_account_code: string;
  amount: string;
  payment_date: string;
  journal_entry_id: number;
  created_at: string;
  updated_at: string;
}

export interface TaxReportItem {
  obligation_id: number;
  unit_id?: number;
  transfer_value: string;
  rate: string;
  tax_amount: string;
  status: "outstanding" | "paid";
  accrual_date: string;
}

export interface TaxReport {
  period_from: string;
  period_to: string;
  items: TaxReportItem[];
  total_obligation: string;
  total_paid: string;
  total_outstanding: string;
}

export interface VATReport {
  period_from: string;
  period_to: string;
  ppn_keluaran: string;
  ppn_masukan: string;
  ppn_terutang: string;
}

export interface CombinedTaxReport {
  period_from: string;
  period_to: string;
  pph_final: TaxReport | null;
  ppn: VATReport | null;
}

// ── Hutang Usaha / Accounts Payable (W-11) ───────────────────────────────────
// Bentuk di bawah mengikuti backend `internal/ap` apa adanya. Semua nominal
// adalah STRING dari API — frontend tidak pernah menghitung ulang angka
// akuntansi (tidak ada aritmetika floating-point di sini).

// Vendor: master minimal (identitas + status PKP). Bukan modul procurement.
export interface Vendor {
  id: number;
  name: string;
  npwp?: string;
  is_pkp: boolean;
  address?: string;
  phone?: string;
  email?: string;
  bank_name?: string;
  bank_account?: string;
  is_active: boolean;
  note?: string;
  created_at: string;
  updated_at: string;
}

// Daur hidup PENGAKUAN tagihan — bukan status pembayaran.
//   draft    : belum ada apa pun di buku (tidak ada hutang, tidak ada realisasi RAB)
//   posted   : kewajiban lahir
//   reversed : dibalik lewat jurnal pembalik (append-only, baris tidak dihapus)
export type APInvoiceStatus = "draft" | "posted" | "reversed";

export interface APInvoice {
  id: number;
  vendor_id: number;
  project_id?: number;
  invoice_number: string;
  invoice_date: string;
  due_date: string;

  dpp_amount: string;
  ppn_amount: string;
  retention_amount: string;
  advance_applied: string;
  payable_amount: string;

  faktur_pajak_number?: string;
  retention_due_date?: string;

  status: APInvoiceStatus;
  journal_entry_id: number;
  reversal_journal_entry_id?: number;

  description: string;
  created_by: number;
  posted_at?: string;
  created_at: string;
  updated_at: string;
}

// Status PEMBAYARAN sebuah tagihan — turunan, bukan kolom.
//
// Backend menurunkannya dari sub-ledger alokasi setiap kali tagihan dibaca
// (`internal/ap/query.go:derivePaymentStatus`). Tidak ada kolom `payment_status`
// di database dan tidak boleh ada perhitungan tandingan di layar: dua definisi
// "lunas" adalah cara tercepat melahirkan dua angka yang tidak sepakat.
//
// `unrecognized` bukan "belum dibayar" — tagihan draft kewajibannya memang belum
// lahir di buku, jadi ia tidak berada di antrean bayar sama sekali.
export type APPaymentStatus =
  | "unrecognized"
  | "unpaid"
  | "partial"
  | "paid"
  | "reversed";

// InvoiceView = tagihan + nama vendor + angka turunan + baris biayanya.
//
// `paid_amount`, `outstanding`, dan `payment_status` seluruhnya DIHITUNG BACKEND
// dari alokasi pembayaran. Layar menampilkannya apa adanya — tidak menjumlahkan
// ulang, tidak mengurangkan sendiri, tidak menebak saat data belum termuat.
export interface APInvoiceView extends APInvoice {
  vendor_name: string;
  vendor_is_pkp: boolean;
  paid_amount: string;
  outstanding: string;
  payment_status: APPaymentStatus;
  lines?: APInvoiceLine[];
}

// Baris biaya milik satu tagihan vendor.
//
// Ini baris `cost_entries` yang sama dengan jalur biaya kas/bank — AP tidak
// punya tabel biaya sendiri (INV-AP: satu tempat untuk realisasi biaya). Tipe
// ini dipisah dari `CostEntry` di atas karena `CostEntry` menggambarkan jalur
// lama yang lebih sempit: `category`-nya hanya kategori kapitalisasi dan ia
// belum mengenal `cost_tier`. Tagihan overhead membawa kategori beban
// (marketing/other) dan tanpa proyek, sehingga tidak muat di bentuk itu.
// Menyamakan keduanya berarti melebarkan tipe yang sudah dipakai layar lain.
export interface APInvoiceLine {
  id: number;
  project_id?: number;   // kosong untuk tagihan overhead tanpa proyek
  unit_id?: number;      // hanya terisi untuk cost_tier "direct"
  phase_id?: number;
  category: string;      // land|hard|soft|financing|marketing|other
  cost_tier: string;     // direct|shared|overhead
  amount: string;        // DPP baris ini — rupiah, string
  date: string;
  vendor: string;
  description: string;
  journal_entry_id: number;
  budget_item_id?: number;
  vendor_id?: number;
  ap_invoice_id?: number;
}

// Baris jurnal pratinjau — PERSIS jurnal yang akan terbit, dihitung backend.
export interface APPreviewLine {
  account_code: string;
  account_name: string;
  debit: string;
  credit: string;
  project_id?: number;
  unit_id?: number;
  description: string;
}

export interface APPreviewResult {
  dpp_amount: string;
  ppn_amount: string;
  retention_amount: string;
  advance_applied: string;
  payable_amount: string;
  lines: APPreviewLine[];
}

// ── Pembayaran vendor (W-11 Tahap 4/5) ──────────────────────────────────────
//
// Satu pembayaran = satu pengeluaran kas = satu BKK bernomor (INV-DOC-1).
// Sisa tagihan TIDAK disimpan di mana pun; ia lahir dari penjumlahan alokasi.
// Karena itu setiap angka di bawah datang dari server, termasuk `outstanding_
// before`/`outstanding_after` di pratinjau: layar tidak boleh menghitung selisih
// sendiri walau kelihatannya sepele.

// Jenis pembayaran. Backend mengenal tiga, tetapi pada tahap ini HANYA
// `invoice` yang diterima — `advance` dan `retention` ditolak dengan pesan
// tersendiri. Tipe ini menyebut ketiganya supaya respons lama tetap terbaca,
// bukan supaya layar menawarkannya.
export type APPaymentKind = "invoice" | "advance" | "retention";

// Satu alokasi: uang sebesar `amount` mendarat di tagihan `invoice_id`.
// `outstanding_before`/`after` dibawa supaya operator bisa memeriksa
// aritmetikanya sendiri tanpa membuka tagihannya satu per satu.
export interface APAllocationView {
  invoice_id: number;
  invoice_number: string;
  due_date: string;
  outstanding_before: string;
  amount: string;
  outstanding_after: string;
}

export interface APPayment {
  id: number;
  vendor_id: number;
  payment_kind: APPaymentKind;
  payment_date: string;
  amount: string;
  cash_account_code: string;
  journal_entry_id: number;
  document_number: string;
  reversed_at?: string;
  description: string;
  created_by: number;
  created_at: string;
  updated_at: string;
}

// PaymentView = pembayaran + nama vendor + (pada detail) alokasinya.
//
// `is_reversed` datang dari server, bukan diturunkan layar dari `reversed_at`:
// keadaan "sudah dibalik" yang menentukan boleh-tidaknya tombol Balikkan muncul
// harus punya satu sumber saja.
export interface APPaymentView extends APPayment {
  vendor_name: string;
  is_reversed: boolean;
  allocations?: APAllocationView[];
}

// Pratinjau: pembayaran yang AKAN terjadi, apa adanya — termasuk baris jurnal
// yang akan terbit. Dihasilkan jalur yang sama dengan pencatatan, lalu di-
// rollback; jadi ia bukan tiruan yang "seharusnya" setara.
export interface APPaymentPreview {
  vendor_id: number;
  vendor_name: string;
  payment_kind: APPaymentKind;
  payment_date: string;
  amount: string;
  cash_account_code: string;
  cash_account_name: string;
  allocations: APAllocationView[];
  lines: APPreviewLine[];
  // Σ sisa SELURUH tagihan vendor yang bisa dibayar — bukan hanya yang
  // teralokasi. Angka ini yang menjawab "kenapa cuma segini yang terpakai".
  total_outstanding: string;
}

export interface APPaymentResult {
  payment: APPayment;
  allocations: APAllocationView[];
  journal_entry_id: number;
  document_number: string;
  // Jawaban yang berasal dari kunci idempotensi, bukan dari pembayaran yang baru
  // saja terjadi. Layar memperlakukannya sebagai SUKSES — klik dobel bukan error.
  replayed: boolean;
}

// Satu baris riwayat pembayaran sebuah tagihan.
//
// Baris yang sudah dibalik tetap dikirim (`reversed_at` terisi) dan tetap
// ditampilkan: "tidak pernah dibayar" dan "pernah dibayar lalu dibatalkan"
// adalah dua keadaan yang sangat berbeda saat ada pertanyaan.
export interface APInvoicePaymentRow {
  allocation_id: number;
  payment_id?: number;
  payment_date?: string;
  document_number?: string;
  cash_account_code?: string;
  allocation_type: string;
  amount: string;
  reversed_at?: string;
}
