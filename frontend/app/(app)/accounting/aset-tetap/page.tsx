import { getTokenAndRole } from "@/lib/auth";
import { fetchFixedAssets, fetchFixedAssetCategories } from "@/lib/api/fixedAsset";
import { FixedAssetList } from "@/components/fixedasset/FixedAssetList";
import { DepreciationRunPanel } from "@/components/fixedasset/DepreciationRunPanel";
import type { FixedAsset, FixedAssetCategory } from "@/lib/types/api";

export const metadata = { title: "Aset Tetap — NATA ALAM RAYA" };

// Register Aset Tetap: perolehan tercatat lewat Transaksi Pengeluaran (Jenis
// Pembelian "Aset Tetap") — halaman ini hanya membaca hasilnya dan memicu
// penyusutan bulanan. Tidak ada jalur perolehan kedua di sini (§ satu pintu).
export default async function FixedAssetPage() {
  const { token, canWrite } = await getTokenAndRole();

  const [assets, categories] = await Promise.all([
    fetchFixedAssets(token).catch(() => [] as FixedAsset[]),
    fetchFixedAssetCategories(token).catch(() => [] as FixedAssetCategory[]),
  ]);

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Aset Tetap</h1>
        <p className="text-sm text-text-secondary mt-1 max-w-prose">
          Kendaraan, peralatan kantor, dan aset berumur panjang lainnya — biaya
          perolehan, akumulasi penyusutan, dan nilai buku dalam satu register.
          Perolehan baru dicatat lewat halaman Transaksi Pengeluaran.
        </p>
      </div>

      <DepreciationRunPanel assets={assets} canWrite={canWrite} />

      <FixedAssetList assets={assets} categories={categories} />
    </div>
  );
}
