"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import { apiFetch } from "@/lib/api/client";
import type { CostEntry, CostPreviewResponse } from "@/lib/types/api";
import type { CreateCostEntryBody } from "@/lib/api/cost";

const COOKIE = "esa_session";

async function token(): Promise<string> {
  return (await cookies()).get(COOKIE)?.value ?? "";
}

// previewCostEntryAction adalah dry-run: mengembalikan baris jurnal yang AKAN terbentuk
// tanpa menyimpan apa pun. Dipanggil sebelum user mengkonfirmasi submit.
// Backend menggunakan jalur resolusi akun yang SAMA dengan create — preview dijamin akurat.
export async function previewCostEntryAction(
  projectId: number,
  body: CreateCostEntryBody,
): Promise<CostPreviewResponse> {
  const t = await token();
  return apiFetch<CostPreviewResponse>(`/projects/${projectId}/cost-entries/preview`, {
    method: "POST",
    token: t,
    body,
  });
}

// Membuat cost entry DRAFT lalu langsung post (finalisasi jurnal).
// Dua langkah ini diperlakukan sebagai satu operasi atomik dari sisi UI.
export async function createAndPostCostEntryAction(
  projectId: number,
  body: CreateCostEntryBody,
): Promise<CostEntry> {
  const t = await token();

  const entry = await apiFetch<CostEntry>(`/projects/${projectId}/cost-entries`, {
    method: "POST",
    token: t,
    body,
  });

  // Post langsung — jurnal menjadi immutable setelah langkah ini
  await apiFetch<void>(`/cost-entries/${entry.id}/post`, {
    method: "POST",
    token: t,
  });

  revalidatePath(`/proyek/${projectId}/biaya`);
  return entry;
}
