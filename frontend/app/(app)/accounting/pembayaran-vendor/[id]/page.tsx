import Link from "next/link";
import { notFound } from "next/navigation";

import { getTokenAndRole } from "@/lib/auth";
import { fetchAPPayment, fetchVendor } from "@/lib/api/ap";
import { ApiError } from "@/lib/api/client";
import { errText } from "@/components/ap/apRules";
import { APPaymentDetail } from "@/components/ap/APPaymentDetail";
import { ErrorState } from "@/components/ui/ErrorState";
import type { APPaymentView, Vendor } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Detail Pembayaran Vendor — NATA ALAM RAYA" };

export default async function PembayaranDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const paymentId = Number(id);
  if (!Number.isInteger(paymentId) || paymentId <= 0) notFound();

  const { token, canWrite } = await getTokenAndRole();

  let payment: APPaymentView;
  try {
    payment = await fetchAPPayment(token, paymentId);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    return (
      <div className="mx-auto w-full max-w-5xl space-y-6">
        <ErrorState title="Pembayaran gagal dimuat" description={errText(e)} />
      </div>
    );
  }

  const vendor = await fetchVendor(token, payment.vendor_id).catch(
    () => undefined as Vendor | undefined,
  );

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <Link
          href="/accounting/pembayaran-vendor"
          className="text-xs text-text-secondary hover:text-accent"
        >
          ← Pembayaran Vendor
        </Link>
        <h1 className="display-lg mt-1 text-2xl text-text-primary">
          Detail Pembayaran Vendor
        </h1>
      </div>

      <APPaymentDetail
        token={token}
        payment={payment}
        vendor={vendor}
        canWrite={canWrite}
        today={todayLocalStr()}
      />
    </div>
  );
}
