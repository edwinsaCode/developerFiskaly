import { notFound } from "next/navigation";
import { getTokenAndRole } from "@/lib/auth";
import { fetchUnit } from "@/lib/api/projects";
import { fetchContractByUnit } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import { ChargeGroupsPanel } from "@/components/penjualan/ChargeGroupsPanel";

// Billing Batch 2 — Tagihan Terpisah per unit:
// Biaya Realisasi (Titipan 2-2400) & Produk Tambahan, dengan outstanding,
// invoice (REALISASI), dan kwitansi (KWR) yang TERPISAH dari harga rumah.

interface PageProps {
  params: Promise<{ unitId: string }>;
}

export default async function UnitChargesPage({ params }: PageProps) {
  const { unitId: unitIdStr } = await params;
  const unitId = parseInt(unitIdStr, 10);
  if (isNaN(unitId)) notFound();

  const { token, canWrite } = await getTokenAndRole();

  const unit = await fetchUnit(token, unitId).catch((err) => {
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  });
  const contract = await fetchContractByUnit(token, unitId).catch(() => null);

  return (
    <div className="space-y-4">
      <div>
        <p className="text-xs text-text-tertiary">
          <a href="/penjualan" className="hover:underline">Penjualan</a>
          {" / "}
          <a href={`/penjualan/${unitId}`} className="hover:underline">Unit {unit.code}</a>
          {" / "}
          <span className="text-text-primary">Tagihan Terpisah</span>
        </p>
        <h1 className="display-lg text-2xl text-text-primary mt-1">
          Biaya Realisasi &amp; Tagihan Lain — Unit {unit.code}
        </h1>
        {!canWrite && (
          <p className="text-xs text-warning mt-1">
            Mode hanya-baca. Hubungi owner atau akuntan untuk mencatat tagihan/pembayaran.
          </p>
        )}
      </div>

      <ChargeGroupsPanel
        token={token}
        unitId={unitId}
        contractId={contract?.id}
        canWrite={canWrite}
      />
    </div>
  );
}
