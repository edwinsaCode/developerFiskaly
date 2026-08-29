"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import type { Vendor } from "@/lib/types/api";
import { createVendor, fetchVendors, updateVendor, type VendorBody } from "@/lib/api/ap";
import { errStatus, errText, type ApResult } from "@/components/ap/apRules";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

// Setiap aksi mengembalikan ApResult, bukan melempar: pesan penolakan backend
// ("nama vendor wajib diisi", dst.) adalah satu-satunya petunjuk yang berguna
// bagi operator, dan exception dari server action kehilangan pesannya di
// build produksi.

export async function createVendorAction(body: VendorBody): Promise<ApResult<Vendor>> {
  try {
    const v = await createVendor(await token(), body);
    revalidatePath("/accounting/vendor");
    return { ok: true, data: v };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

export async function updateVendorAction(
  id: number,
  body: VendorBody,
): Promise<ApResult<Vendor>> {
  try {
    const v = await updateVendor(await token(), id, body);
    revalidatePath("/accounting/vendor");
    return { ok: true, data: v };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}

// Dipakai layar tagihan: daftar vendor aktif untuk pemilihan, tanpa memuat
// ulang seluruh halaman.
export async function fetchVendorsAction(): Promise<ApResult<Vendor[]>> {
  try {
    return { ok: true, data: await fetchVendors(await token(), { active: true }) };
  } catch (e) {
    return { ok: false, error: errText(e), status: errStatus(e) };
  }
}
