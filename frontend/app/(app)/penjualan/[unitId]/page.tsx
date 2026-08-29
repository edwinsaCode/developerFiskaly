import { notFound } from "next/navigation";
import { fetchUnit } from "@/lib/api/projects";
import { fetchSaleRecord, fetchContractByUnit } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import { UnitSalePanel } from "@/components/penjualan/UnitSalePanel";
import { getTokenAndRole } from "@/lib/auth";

interface PageProps {
  params: Promise<{ unitId: string }>;
  searchParams: Promise<{ contract?: string; booking?: string }>;
}

export default async function UnitSalePage({ params, searchParams }: PageProps) {
  const { unitId: unitIdStr } = await params;
  const { contract: contractIdStr, booking: bookingIdStr } = await searchParams;

  const unitId = parseInt(unitIdStr, 10);
  if (isNaN(unitId)) notFound();

  const { token, role } = await getTokenAndRole();

  const unitRes = await fetchUnit(token, unitId).catch((err) => {
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  });

  // W-12 — sale record memuat rincian HPP; backend membalas 403 untuk marketing.
  // Status serah terima tetap terbaca dari status unit.
  const saleRecord =
    role === "marketing"
      ? null
      : await fetchSaleRecord(token, unitId).catch(() => null);

  // Kontrak aktif unit dimuat dari backend — TIDAK bergantung query param.
  // (Sebelumnya hanya dari ?contract= → user yang kembali ke halaman ini
  // mengira kontrak belum ada dan bisa membuat kontrak ganda.)
  let contractId = contractIdStr ? parseInt(contractIdStr, 10) : undefined;
  if (!contractId || isNaN(contractId)) {
    const existing = await fetchContractByUnit(token, unitId).catch(() => null);
    if (existing) contractId = existing.id;
  }
  const bookingId = bookingIdStr ? parseInt(bookingIdStr, 10) : undefined;

  return (
    <div>
      <UnitSalePanel
        unit={unitRes}
        saleRecord={saleRecord}
        contractId={contractId && !isNaN(contractId) ? contractId : undefined}
        bookingParam={bookingId && !isNaN(bookingId) ? bookingId : undefined}
        token={token}
      />
    </div>
  );
}
