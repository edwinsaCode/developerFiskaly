import { apiFetch } from "./client";
import type { CollectionPreview, CollectionPaymentResult, TerminKind } from "@/lib/types/api";

// previewCollectionPayment menampilkan pratinjau jurnal + validasi sebelum submit.
export async function previewCollectionPayment(
  token: string,
  contractId: number,
  amount: string,
  bankAccountCode: string,
  /** UAT 2026-09-03 Rule #5: provisi/administrasi bank dipotong dari pencairan. */
  bankFee?: string,
  /** P1 Kelebihan Tanah: bila diisi, pembayaran ditarget ke SATU schedule (mis.
   *  cicilan Kelebihan Tanah) alih-alih waterfall seluruh kontrak. */
  scheduleId?: number,
  /** R1/7C (UAT 2026-09-07, Gap 2): isi untuk PENCAIRAN BANK KPR — harus sejajar
   *  dengan recordCollectionPayment, supaya backend meresolusi source=kpr_disbursement
   *  dan pratinjau menampilkan Dana Jaminan Bank (bukan Piutang Usaha generik). */
  financingSourceId?: number,
): Promise<CollectionPreview> {
  const qs = new URLSearchParams({
    contract_id: String(contractId),
    amount: amount || "0",
    bank_account_code: bankAccountCode,
  });
  if (bankFee) qs.set("bank_fee", bankFee);
  if (scheduleId) qs.set("schedule_id", String(scheduleId));
  if (financingSourceId) qs.set("financing_source_id", String(financingSourceId));
  return apiFetch<CollectionPreview>(`/collections/payment/preview?${qs}`, { token });
}

export interface CollectionPaymentInput {
  contract_id: number;
  amount: string; // integer rupiah string
  date: string; // RFC3339
  bank_account_code: string;
  reference?: string;
  notes?: string;
  idempotency_key: string;
  /** R1: isi untuk PENCAIRAN BANK KPR — backend otomatis source kpr_disbursement + validasi akad. */
  financing_source_id?: number;
  /** W-13: Jenis Penerimaan terstruktur — diabaikan/diturunkan otomatis bila financing_source_id diisi. */
  kind?: TerminKind;
  installment_no?: number;
  /** UAT 2026-09-03 Rule #5: provisi/administrasi bank yang dipotong saat pencairan (mis. KPR) —
   *  DITANGGUNG DEVELOPER, bukan titipan customer. Kosong/"0" = tanpa perubahan perilaku. */
  bank_fee?: string;
  /** P1 Kelebihan Tanah: schedule yang ditarget (mis. cicilan Kelebihan Tanah);
   *  kosong = waterfall seluruh kontrak seperti sebelumnya. */
  schedule_id?: number;
}

export async function recordCollectionPayment(
  token: string,
  data: CollectionPaymentInput,
): Promise<CollectionPaymentResult> {
  return apiFetch<CollectionPaymentResult>("/collections/payment", {
    token,
    method: "POST",
    body: data,
  });
}
