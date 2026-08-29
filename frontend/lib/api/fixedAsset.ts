import { apiFetch } from "./client";
import type {
  DepreciationRunResult,
  FixedAsset,
  FixedAssetCategory,
} from "@/lib/types/api";

// Klien Fixed Asset Register. Perolehan aset TIDAK ada di sini — itu lewat
// toggle "Jenis Pembelian" di internal/cost/expense.ts (satu pintu masuk
// pengeluaran). Modul ini hanya membaca master kategori, membaca Register,
// dan memicu penyusutan bulanan.

export async function fetchFixedAssetCategories(token: string): Promise<FixedAssetCategory[]> {
  return apiFetch<FixedAssetCategory[]>("/fixed-asset-categories", { token });
}

export async function fetchFixedAssets(token: string): Promise<FixedAsset[]> {
  return apiFetch<FixedAsset[]>("/fixed-assets", { token });
}

export async function runDepreciation(
  token: string,
  year: number,
  month: number,
): Promise<DepreciationRunResult> {
  return apiFetch<DepreciationRunResult>("/fixed-assets/depreciation-runs", {
    method: "POST",
    token,
    body: { year, month },
  });
}
