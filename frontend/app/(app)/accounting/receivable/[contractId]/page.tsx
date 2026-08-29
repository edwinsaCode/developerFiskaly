import Link from "next/link";
import { getTokenAndRole } from "@/lib/auth";
import { fetchCustomerStatement, fetchContractAllocations } from "@/lib/api/sale";
import { listInvoices } from "@/lib/api/billing";
import { CustomerStatementView } from "@/components/accounting/CustomerStatementView";
import { RealizationStatementSection } from "@/components/accounting/RealizationStatementSection";
import { ErrorState } from "@/components/ui/ErrorState";
import type { CustomerStatement, AllocationView, Invoice } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Rekening Customer — NATA ALAM RAYA" };

const today = () => todayLocalStr();

interface PageProps {
  params: Promise<{ contractId: string }>;
  searchParams: Promise<Record<string, string | undefined>>;
}

export default async function StatementPage({ params, searchParams }: PageProps) {
  const { contractId } = await params;
  const sp = await searchParams;
  const asOf = sp.as_of ?? today();
  const id = Number(contractId);

  const { token } = await getTokenAndRole();

  let statement: CustomerStatement | null = null;
  let allocations: AllocationView[] = [];
  let invoices: Invoice[] = [];
  let notFound = false;
  try {
    statement = await fetchCustomerStatement(token, id, asOf);
    // Breakdown alokasi + invoice (best-effort): kegagalan tidak memblok statement.
    [allocations, invoices] = await Promise.all([
      fetchContractAllocations(token, id).catch(() => []),
      listInvoices(token, id).catch(() => []),
    ]);
  } catch (err) {
    // 404 → kontrak tidak ada; selain itu → error umum.
    notFound = err instanceof Error && /404|tidak ditemukan/i.test(err.message);
    statement = null;
  }

  return (
    <div className="space-y-6">
      <div>
        <Link href="/accounting/receivable" className="text-sm text-accent hover:underline">
          ← Kembali ke Piutang Customer
        </Link>
      </div>

      {statement === null ? (
        <ErrorState
          title={notFound ? "Kontrak tidak ditemukan" : "Gagal memuat rekening customer"}
          description={
            notFound
              ? "Kontrak penjualan ini tidak ada atau bukan milik tenant Anda."
              : "Data rekening tidak dapat diambil dari server. Periksa koneksi lalu coba lagi."
          }
          showRetry={!notFound}
        />
      ) : (
        <>
          <CustomerStatementView statement={statement} asOf={asOf} token={token} allocations={allocations} invoices={invoices} />
          {/* Billing Batch 2: seksi Biaya Realisasi — billing terpisah, satu statement. */}
          <RealizationStatementSection token={token} contractId={id} unitId={statement.unit_id} />
        </>
      )}
    </div>
  );
}
