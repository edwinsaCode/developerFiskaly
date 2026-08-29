import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { AllInvoiceList } from "@/components/billing/AllInvoiceList";
import { listAllInvoices, type AllInvoicesResponse } from "@/lib/api/billing";
import { ErrorState } from "@/components/ui/ErrorState";

export const metadata = { title: "Invoice Customer — NATA ALAM RAYA" };

async function getToken(): Promise<string | null> {
  const store = await cookies();
  return store.get("esa_session")?.value ?? null;
}

export default async function InvoicesPage() {
  const token = await getToken();
  if (!token) redirect("/login");

  // Distinguish "no invoices yet" from "failed to load" — an accountant must
  // never see a falsely-empty list. Let the error surface, don't swallow it.
  let invoices: AllInvoicesResponse | null = null;
  try {
    invoices = await listAllInvoices(token);
  } catch {
    invoices = null;
  }

  return (
    <div className="mx-auto w-full max-w-7xl">
      <div className="mb-6">
        <h1 className="display-lg text-2xl text-text-primary">Invoice Customer</h1>
        <p className="text-sm text-text-secondary mt-1">
          Semua invoice yang diterbitkan ke pembeli, lintas proyek dan unit.
        </p>
      </div>
      {invoices === null ? (
        <ErrorState
          title="Gagal memuat daftar invoice"
          description="Data invoice tidak dapat diambil dari server. Periksa koneksi lalu coba lagi."
        />
      ) : (
        <AllInvoiceList token={token} initialInvoices={invoices.invoices} totalUnpaid={invoices.total_unpaid} />
      )}
    </div>
  );
}
