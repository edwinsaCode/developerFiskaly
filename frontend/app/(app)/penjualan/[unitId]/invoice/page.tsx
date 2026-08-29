import { cookies } from "next/headers";
import { notFound } from "next/navigation";
import { fetchUnit } from "@/lib/api/projects";
import { fetchContractByUnit, fetchSchedulesByContract } from "@/lib/api/sale";
import { listInvoices } from "@/lib/api/billing";
import { ApiError } from "@/lib/api/client";
import { InvoiceList } from "@/components/billing/InvoiceList";
import { EmptyState } from "@/components/ui/EmptyState";

const COOKIE_NAME = "esa_session";

async function getToken() {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

interface PageProps {
  params: Promise<{ unitId: string }>;
}

export default async function InvoicePage({ params }: PageProps) {
  const { unitId: unitIdStr } = await params;
  const unitId = parseInt(unitIdStr, 10);
  if (isNaN(unitId)) notFound();

  const token = await getToken();

  const unit = await fetchUnit(token, unitId).catch((err) => {
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  });

  const contract = await fetchContractByUnit(token, unitId).catch((err) => {
    if (err instanceof ApiError && err.status === 404) return null;
    return null;
  });

  const [schedules, invoices] = contract
    ? await Promise.all([
        fetchSchedulesByContract(token, contract.id),
        listInvoices(token, contract.id),
      ])
    : [[], []];

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <a
          href={`/penjualan/${unitId}`}
          className="text-sm text-text-secondary hover:underline"
        >
          ← Kembali
        </a>
        <h1 className="display-lg text-2xl text-text-primary">
          Invoice — Unit {unit.code}
        </h1>
      </div>

      {!contract ? (
        <EmptyState
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
              strokeLinecap="round" strokeLinejoin="round">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
              <path d="M14 2v6h6M9 13h6M9 17h4" />
            </svg>
          }
          title="Unit ini belum memiliki kontrak penjualan"
          description="Invoice hanya bisa dibuat setelah unit terjual dan kontrak penjualan beserta jadwal pembayaran (DP, termin, pelunasan) dibuat. Mulai dari panel penjualan unit."
          action={
            <a
              href={`/penjualan/${unitId}`}
              className="inline-flex items-center gap-2 px-4 py-2 rounded-md bg-accent text-white text-sm font-medium hover:bg-accent/90 transition-colors"
            >
              Buat Kontrak Penjualan →
            </a>
          }
        />
      ) : (
        <InvoiceList
          token={token}
          contractId={contract.id}
          unitId={unitId}
          buyerName={contract.buyer_name}
          schedules={schedules}
          initialInvoices={invoices}
        />
      )}
    </div>
  );
}
