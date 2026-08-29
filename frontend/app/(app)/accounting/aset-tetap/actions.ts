"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import type { DepreciationRunResult } from "@/lib/types/api";
import { runDepreciation } from "@/lib/api/fixedAsset";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

// runDepreciationAction: memposting penyusutan SEMUA aset aktif untuk satu
// periode. Idempoten di sisi backend (UNIQUE tenant+asset+periode) — memanggil
// ulang untuk periode yang sama hanya menghasilkan skip, bukan jurnal ganda.
export async function runDepreciationAction(year: number, month: number): Promise<DepreciationRunResult> {
  const result = await runDepreciation(await token(), year, month);
  revalidatePath("/accounting/aset-tetap");
  return result;
}
