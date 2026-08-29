"use server";

// Server action pembayaran hutang vendor (W-11 Tahap 5).
//
// Semua panggilan tulis lewat sini supaya token sesi tidak pernah menyeberang ke
// bundel browser. Yang tidak boleh terjadi di file ini: menghitung sisa tagihan,
// menyusun jurnal, atau menebak status. Ia hanya memanggil dan menerjemahkan
// error backend menjadi bentuk yang bisa dibawa server action.

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";

import {
  createAPPayment,
  fetchAPInvoicePayments,
  fetchAPPayment,
  previewAPPayment,
  reverseAPPayment,
  type APPaymentBody,
} from "@/lib/api/ap";
import { errStatus, errText } from "@/components/ap/apRules";
import { overpaymentFrom, type PayResult } from "@/components/ap/paymentRules";
import type {
  APInvoicePaymentRow,
  APPaymentPreview,
  APPaymentResult,
  APPaymentView,
} from "@/lib/types/api";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

// fail menerjemahkan apa pun yang dilempar klien API menjadi PayResult.
//
// Payload kelebihan bayar diangkat ke field tersendiri: pesan backend untuk
// kasus ini berbunyi "overpayment" pada field `error`, sementara kalimat yang
// berguna bagi operator ada di `message` bersama ketiga angkanya.
function fail<T>(e: unknown): PayResult<T> {
  const payload =
    e && typeof e === "object" ? (e as { payload?: unknown }).payload : undefined;
  const over = overpaymentFrom(payload);
  return {
    ok: false,
    error: over ? over.message : errText(e),
    status: errStatus(e),
    ...(over ? { overpayment: over } : {}),
  };
}

// previewPaymentAction: dry-run. Backend menjalankan SELURUH validasi,
// penguncian, alokasi, dan komposisi jurnal yang dijalankan pencatatan, lalu
// me-rollback transaksinya. Tidak ada satu baris pun yang tertulis.
export async function previewPaymentAction(
  body: APPaymentBody,
): Promise<PayResult<APPaymentPreview>> {
  try {
    return { ok: true, data: await previewAPPayment(await token(), body) };
  } catch (e) {
    return fail(e);
  }
}

// recordPaymentAction mencatat pembayaran DAN menerbitkan BKK dalam satu
// transaksi. `idempotencyKey` datang dari form dan tidak berubah antar percobaan
// — itulah yang membuat klik dobel tidak melahirkan dua pengeluaran kas.
export async function recordPaymentAction(
  body: APPaymentBody,
  idempotencyKey: string,
): Promise<PayResult<APPaymentResult>> {
  try {
    const res = await createAPPayment(await token(), body, idempotencyKey);
    revalidatePath("/accounting/pembayaran-vendor");
    revalidatePath("/accounting/hutang");
    for (const a of res.allocations) {
      revalidatePath(`/accounting/hutang/${a.invoice_id}`);
    }
    return { ok: true, data: res };
  } catch (e) {
    return fail(e);
  }
}

// reversePaymentAction membalik lewat jurnal pembalik. Pembayaran aslinya tidak
// dihapus dan tidak diedit — yang berubah hanya penandanya.
export async function reversePaymentAction(
  id: number,
  reverseDate?: string,
): Promise<PayResult<APPaymentView>> {
  try {
    const pay = await reverseAPPayment(await token(), id, reverseDate);
    revalidatePath("/accounting/pembayaran-vendor");
    revalidatePath(`/accounting/pembayaran-vendor/${id}`);
    revalidatePath("/accounting/hutang");
    return { ok: true, data: pay };
  } catch (e) {
    return fail(e);
  }
}

export async function fetchInvoicePaymentsAction(
  invoiceID: number,
): Promise<PayResult<APInvoicePaymentRow[]>> {
  try {
    return { ok: true, data: await fetchAPInvoicePayments(await token(), invoiceID) };
  } catch (e) {
    return fail(e);
  }
}

export async function fetchPaymentAction(id: number): Promise<PayResult<APPaymentView>> {
  try {
    return { ok: true, data: await fetchAPPayment(await token(), id) };
  } catch (e) {
    return fail(e);
  }
}
