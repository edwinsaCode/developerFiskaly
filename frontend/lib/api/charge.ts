import { apiFetch } from "./client";

// ── Billing Batch 2 — Charge Group (Titipan Realisasi & Addon) ───────────────
//
// Seluruh angka (billed/paid/outstanding/residual) DIHITUNG BACKEND dari
// formula kanonik charge.Service.GroupSummary — frontend hanya menampilkan.

export type ChargeKind = "realization" | "addon";
export type ChargeGroupStatus = "open" | "settled" | "cancelled";
export type ChargeItemStatus = "open" | "cancelled";

// W-5 / R-5: piutang lahir saat INVOICE terbit, bukan saat tagihan dibuat.
//   unbilled   — belum di-invoice; masih kewajiban titipan, bukan piutang
//   receivable — sudah diakui, masih ada sisa
//   paid       — sudah diakui dan lunas
export type ChargeRecognitionStatus = "unbilled" | "receivable" | "paid";

export interface ChargeItemSummary {
  item_id: number;
  label: string;
  // W-1: kosong = baris historis (dibuat sebelum master jenis biaya ada).
  charge_type_code?: string;
  // W-13: produk katalog untuk item addon. Kosong = item realisasi, atau baris
  // addon historis yang dibuat saat produk tambahan masih teks bebas.
  product_code?: string;
  // Akun kewajiban yang DIBEKUKAN saat item dibuat (snapshot, seperti label):
  // jurnal pembalik selalu mengenai akun yang benar-benar dikredit dulu.
  deposit_account_code: string;
  status: ChargeItemStatus;
  amount: string;
  original_amount: string;
  paid: string;
  outstanding: string;
  payout: string;
  due_date?: string;
  // ── W-5 ──
  // recognized_amount: nominal yang benar-benar di-invoice & diposting ke 1-2000.
  // Bisa lebih kecil dari amount bila tagihan dinaikkan (K-5) tanpa invoice baru;
  // selisih itulah unbilled.
  recognized_amount: string;
  recognized_at?: string;
  receivable: string;
  unbilled: string;
  recognition_status: ChargeRecognitionStatus;
}

export interface ChargeGroupSummary {
  group_id: number;
  sale_contract_id: number;
  unit_id: number;
  kind: ChargeKind;
  label: string;
  status: ChargeGroupStatus;
  billed: string;
  paid: string;
  outstanding: string;
  payout: string;
  returned: string;
  residual: string;
  // Rincian sisa titipan per akun kewajiban. Σ deposit_residuals == residual,
  // selalu — satu grup boleh memakai lebih dari satu akun.
  deposit_residuals: { account_code: string; amount: string }[];
  // ── W-5 ── recognized: grup sudah pernah di-invoice.
  // receivable = Σ piutang item (kontribusi ke 1-2000);
  // unbilled   = Σ yang belum ditagihkan (belum menjadi piutang).
  recognized: boolean;
  receivable: string;
  unbilled: string;
  // Nomor invoice terakhir yang mengakui piutang grup — dokumen asal angka
  // receivable. Kosong bila belum pernah ditagihkan.
  invoice_number?: string;
  items: ChargeItemSummary[];
}

export interface ChargePaymentRow {
  termin_id: number;
  amount: string;
  date: string;
  bank_account_code: string;
  payment_source: string;
  description: string;
  receipt_number?: string;
  receipt_id?: number;
  voided: boolean;
}

export interface ChargePayout {
  id: number;
  charge_group_id: number;
  charge_item_id: number;
  amount: string;
  date: string;
  bank_account_code: string;
  vendor: string;
  notes?: string;
}

export interface ChargeSettlement {
  id: number;
  charge_group_id: number;
  action: "refund" | "transfer_house" | "transfer_group" | "void_payment";
  amount: string;
  date: string;
  bank_account_code?: string;
  target_group_id?: number;
  voided_termin_id?: number;
  notes?: string;
  // T-1: nomor Memo Transfer Internal (MTI/{tahun}/{6 digit}). Hanya terisi
  // untuk aksi transfer — transfer BUKAN kas masuk baru sehingga tidak pernah
  // menerbitkan kwitansi; memo inilah dokumen audit trail-nya.
  memo_number?: string;
}

// TransferMemo — dokumen transfer internal (T-1). Bukan bukti terima uang:
// uangnya sudah diterima & ber-kwitansi KWR sebelumnya.
export interface TransferMemo {
  settlement_id: number;
  memo_number: string;
  action: "transfer_house" | "transfer_group";
  date: string;
  amount: string;
  source_group_id: number;
  source_group_label: string;
  source_unit_code: string;
  sale_contract_id: number;
  buyer_name: string;
  target_group_id?: number;
  target_group_label?: string;
  target_unit_code?: string;
  target_termin_id?: number;
  target_label: string;
  journal_entry_id?: number;
  notes?: string;
  created_by?: number;
  company_name?: string;
}

// R-A: jejak audit perubahan kebijakan finansial tenant. Kebijakannya sendiri
// bisa saja sudah dicabut (mis. gate BAST realisasi di W-5) — riwayatnya tetap.
export interface PolicyChange {
  id: number;
  policy_key: string;
  old_value: string;
  new_value: string;
  changed_by?: number;
  notes?: string;
  created_at: string;
}

export interface ChargeGroupDetail {
  summary: ChargeGroupSummary;
  payments: ChargePaymentRow[];
  payouts: ChargePayout[];
  settlements: ChargeSettlement[];
}

export interface ChargePortfolio {
  open_groups: number;
  billed: string;
  paid: string;
  outstanding: string;
  residual: string;
  // W-5: belahan sisa terutang — yang sudah menjadi piutang (berdokumen invoice)
  // dan yang belum ditagihkan sama sekali.
  receivable: string;
  unbilled: string;
}

export interface ChargeAllocationInput {
  charge_item_id: number;
  amount: string;
}

export interface ChargeItemInput {
  label: string;
  amount: string;
  due_date?: string;
  // W-1: wajib untuk grup realization — menunjuk master jenis biaya realisasi.
  // Backend fail-closed: item realization tanpa kode ini ditolak, tidak ada
  // fallback diam-diam ke akun titipan default.
  charge_type_code?: string;
  // W-13: wajib untuk grup addon — menunjuk master Katalog Produk. Produk yang
  // menentukan akun pendapatannya, bukan yang mengetik. Sama fail-closed-nya:
  // item addon tanpa produk ditolak, dan produk berkategori properti ditolak
  // pula (rumah dijual sebagai unit, bukan sebagai baris tagihan).
  product_code?: string;
}

// ── W-1: master jenis biaya realisasi ────────────────────────────────────────
//
// Satu jenis biaya = satu akun titipan (kewajiban). Treatment SELALU
// deposit_liability — keputusan bisnis klien yang dikunci, bukan setelan yang
// bisa diubah admin, jadi tidak ada kontrol UI untuknya.

export interface RealizationChargeType {
  id: number;
  code: string;
  name: string;
  treatment: "deposit_liability" | "revenue";
  deposit_account_code: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

// TD-8: audit append-only perubahan master data finansial. Mengubah akun
// titipan mengubah ke mana uang customer mendarat — itu jejak yang wajib ada.
export interface MasterDataChange {
  id: number;
  entity: string;
  entity_code: string;
  field: string;
  old_value: string;
  new_value: string;
  changed_by?: number;
  created_at: string;
}

export interface ChargePaymentResult {
  termin_id: number;
  receipt_number?: string;
  receipt_id?: number;
  already_existed?: boolean;
  summary: ChargeGroupSummary;
}

export interface SettleResult {
  settled: boolean;
  summary: ChargeGroupSummary;
}

// ── Readers ──────────────────────────────────────────────────────────────────

export async function fetchChargeGroupsByUnit(token: string, unitId: number): Promise<ChargeGroupSummary[]> {
  return (await apiFetch<ChargeGroupSummary[]>(`/charges/groups?unit_id=${unitId}`, { token })) ?? [];
}

export async function fetchChargeGroupsByContract(token: string, contractId: number): Promise<ChargeGroupSummary[]> {
  return (await apiFetch<ChargeGroupSummary[]>(`/charges/groups?contract_id=${contractId}`, { token })) ?? [];
}

export async function fetchChargeGroupDetail(token: string, groupId: number): Promise<ChargeGroupDetail> {
  return apiFetch<ChargeGroupDetail>(`/charges/groups/${groupId}`, { token });
}

export async function fetchChargePortfolio(token: string): Promise<ChargePortfolio> {
  return apiFetch<ChargePortfolio>(`/charges/portfolio`, { token });
}

export async function fetchChargePolicyHistory(token: string, limit = 20): Promise<PolicyChange[]> {
  const res = await apiFetch<{ data: PolicyChange[] }>(`/charges/policy/history?limit=${limit}`, { token });
  return res?.data ?? [];
}

export async function fetchTransferMemo(token: string, settlementId: number): Promise<TransferMemo> {
  return apiFetch(`/charges/settlements/${settlementId}/memo`, { token });
}

export async function fetchChargeTypes(token: string): Promise<RealizationChargeType[]> {
  const res = await apiFetch<{ data: RealizationChargeType[] }>(`/charges/types`, { token });
  return res?.data ?? [];
}

export async function fetchChargeTypeHistory(token: string, limit = 20): Promise<MasterDataChange[]> {
  const res = await apiFetch<{ data: MasterDataChange[] }>(`/charges/types/history?limit=${limit}`, { token });
  return res?.data ?? [];
}

// ── Writers ──────────────────────────────────────────────────────────────────

export async function createChargeGroup(
  token: string,
  data: { sale_contract_id: number; kind: ChargeKind; label: string; notes?: string; items: ChargeItemInput[] },
): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/groups`, { token, method: "POST", body: data });
}

export async function addChargeItems(token: string, groupId: number, items: ChargeItemInput[]): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/groups/${groupId}/items`, { token, method: "POST", body: { items } });
}

export async function payChargeGroup(
  token: string,
  groupId: number,
  data: {
    amount: string;
    date?: string;
    bank_account_code: string;
    allocations: ChargeAllocationInput[];
    reference?: string;
    notes?: string;
    idempotency_key?: string;
  },
): Promise<ChargePaymentResult> {
  return apiFetch(`/charges/groups/${groupId}/payments`, { token, method: "POST", body: data });
}

export async function recordChargePayout(
  token: string,
  itemId: number,
  data: { amount: string; date?: string; bank_account_code: string; vendor: string; notes?: string; idempotency_key?: string },
): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/items/${itemId}/payouts`, { token, method: "POST", body: data });
}

export async function refundChargeGroup(
  token: string,
  groupId: number,
  data: { amount: string; date?: string; bank_account_code: string; notes?: string; idempotency_key?: string },
): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/groups/${groupId}/refund`, { token, method: "POST", body: data });
}

export async function transferChargeToHouse(
  token: string,
  groupId: number,
  data: { amount: string; date?: string; notes?: string; idempotency_key?: string },
): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/groups/${groupId}/transfer-house`, { token, method: "POST", body: data });
}

export async function transferChargeToGroup(
  token: string,
  groupId: number,
  data: {
    target_group_id: number;
    amount: string;
    date?: string;
    allocations: ChargeAllocationInput[];
    notes?: string;
    idempotency_key?: string;
  },
): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/groups/${groupId}/transfer-group`, { token, method: "POST", body: data });
}

export async function settleChargeGroup(token: string, groupId: number): Promise<SettleResult> {
  return apiFetch(`/charges/groups/${groupId}/settle`, { token, method: "POST", body: {} });
}

export async function cancelChargeGroup(token: string, groupId: number): Promise<{ status: string }> {
  return apiFetch(`/charges/groups/${groupId}/cancel`, { token, method: "POST", body: {} });
}

export async function cancelChargeItem(token: string, itemId: number): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/items/${itemId}/cancel`, { token, method: "POST", body: {} });
}

export async function voidChargePayment(token: string, terminId: number, reason: string): Promise<ChargeGroupSummary> {
  return apiFetch(`/charges/payments/${terminId}/void`, { token, method: "POST", body: { reason } });
}

export async function issueChargeInvoice(
  token: string,
  groupId: number,
  data: { due_date?: string; notes?: string } = {},
): Promise<{ invoice_id: number; invoice_number: string; recognized_amount: string }> {
  return apiFetch(`/charges/groups/${groupId}/invoice`, { token, method: "POST", body: data });
}

export async function createChargeType(
  token: string,
  data: { code: string; name: string; deposit_account_code?: string },
): Promise<RealizationChargeType> {
  return apiFetch(`/charges/types`, { token, method: "POST", body: data });
}

export async function updateChargeType(
  token: string,
  typeId: number,
  data: { name?: string; deposit_account_code?: string; is_active?: boolean },
): Promise<RealizationChargeType> {
  return apiFetch(`/charges/types/${typeId}`, { token, method: "PATCH", body: data });
}

// Kebijakan gate BAST realisasi (GET/PUT /charges/policy) DIHAPUS di W-5:
// keputusan klien D-3 mencabutnya permanen — BAST tidak pernah lagi menuntut
// biaya realisasi lunas. Riwayatnya tetap dibaca lewat fetchChargePolicyHistory.
