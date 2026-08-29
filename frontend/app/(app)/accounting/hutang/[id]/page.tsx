import Link from "next/link";
import { notFound } from "next/navigation";

import { getTokenAndRole } from "@/lib/auth";
import { fetchAPInvoice, fetchAPInvoicePayments, fetchVendor } from "@/lib/api/ap";
import { fetchProject } from "@/lib/api/projects";
import { ApiError } from "@/lib/api/client";
import { errText } from "@/components/ap/apRules";
import { APInvoiceDetail } from "@/components/ap/APInvoiceDetail";
import { ErrorState } from "@/components/ui/ErrorState";
import { todayLocalStr } from "@/lib/date";
import type {
  APInvoicePaymentRow,
  APInvoiceView,
  Project,
  Vendor,
} from "@/lib/types/api";

export const metadata = { title: "Detail Tagihan Vendor — NATA ALAM RAYA" };

export default async function TagihanDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const invoiceId = Number(id);
  if (!Number.isInteger(invoiceId) || invoiceId <= 0) notFound();

  const { token, canWrite } = await getTokenAndRole();

  let invoice: APInvoiceView;
  try {
    invoice = await fetchAPInvoice(token, invoiceId);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    return (
      <div className="mx-auto w-full max-w-5xl space-y-6">
        <ErrorState
          title="Tagihan gagal dimuat"
          description={errText(e)}
        />
      </div>
    );
  }

  // Riwayat pembayaran diambil terpisah dan kegagalannya TIDAK menjatuhkan
  // halaman: tagihannya sendiri sudah membawa sisa dan status pembayaran, jadi
  // panel yang kosong lebih baik daripada detail tagihan yang tidak bisa dibuka.
  let paymentRows: APInvoicePaymentRow[] = [];
  let paymentRowsError: string | undefined;

  const [vendor, project] = await Promise.all([
    fetchVendor(token, invoice.vendor_id).catch(() => undefined as Vendor | undefined),
    invoice.project_id
      ? fetchProject(token, invoice.project_id).catch(() => undefined as Project | undefined)
      : Promise.resolve(undefined as Project | undefined),
    fetchAPInvoicePayments(token, invoiceId)
      .then((rows) => {
        paymentRows = rows;
      })
      .catch((e) => {
        paymentRowsError = errText(e);
      }),
  ]);

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <Link
          href="/accounting/hutang"
          className="text-xs text-text-secondary hover:text-accent"
        >
          ← Hutang Usaha
        </Link>
        <h1 className="display-lg mt-1 text-2xl text-text-primary">Detail Tagihan Vendor</h1>
      </div>

      <APInvoiceDetail
        invoice={invoice}
        vendor={vendor}
        project={project}
        canWrite={canWrite}
        today={todayLocalStr()}
        paymentRows={paymentRows}
        paymentRowsError={paymentRowsError}
      />
    </div>
  );
}
