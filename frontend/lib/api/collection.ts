import { apiFetch } from "./client";
import type { CollectionPreview, CollectionPaymentResult, TerminKind } from "@/lib/types/api";

// previewCollectionPayment menampilkan pratinjau jurnal + validasi sebelum submit.
export async function previewCollectionPayment(
  token: string,
  contractId: number,
  amount: string,
  bankAccountCode: string,
): Promise<CollectionPreview> {
  const qs = new URLSearchParams({
    contract_id: String(contractId),
    amount: amount || "0",
    bank_account_code: bankAccountCode,
  });
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
