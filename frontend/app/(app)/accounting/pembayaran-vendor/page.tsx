import Link from "next/link";

import { getTokenAndRole } from "@/lib/auth";
import { fetchAPInvoices, fetchAPPayments, fetchVendors } from "@/lib/api/ap";
import { errText } from "@/components/ap/apRules";
import { eligibleInvoicesOf } from "@/components/ap/paymentRules";
import { APPaymentList } from "@/components/ap/APPaymentList";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import type { APInvoiceView, APPaymentView, Vendor } from "@/lib/types/api";

export const metadata = { title: "Pembayaran Vendor — NATA ALAM RAYA" };

// Pembayaran vendor: uang keluar untuk melunasi kewajiban yang SUDAH diakui.
// Halaman ini daftar pengeluarannya — pengakuan hutangnya ada di Hutang Usaha.
export default async function PembayaranVendorPage() {
  const { token, canWrite } = await getTokenAndRole();

  let payments: APPaymentView[] = [];
  let loadError: string | undefined;
  try {
    payments = await fetchAPPayments(token);
  } catch (e) {
    loadError = errText(e);
  }

  const [vendors, invoices] = await Promise.all([
    fetchVendors(token).catch(() => [] as Vendor[]),
    fetchAPInvoices(token).catch(() => [] as APInvoiceView[]),
  ]);

  // Antrean bayar dibaca dari status yang dihitung backend, bukan dari
  // pengurangan di layar. Angkanya hanya dipakai untuk mengarahkan perhatian.
  const antrean = eligibleInvoicesOf(invoices, "");

  return (
    <div className="mx-auto w-full max-w-7xl space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Pembayaran Vendor</h1>
          <p className="mt-1 max-w-prose text-sm text-text-secondary">
            Uang keluar untuk melunasi tagihan vendor yang kewajibannya sudah diakui.
            Setiap pembayaran menerbitkan satu bukti kas keluar (BKK) bernomor, dan
            sisa tagihan berkurang karena alokasinya.
          </p>
        </div>
        {canWrite && antrean.length > 0 && (
          <Link href="/accounting/pembayaran-vendor/baru">
            <Button>Bayar Tagihan Vendor</Button>
          </Link>
        )}
      </div>

      <AccountingNav />

      {antrean.length === 0 ? (
        <Card>
          <p className="text-sm font-medium text-text-primary">
            Tidak ada tagihan yang menunggu dibayar
          </p>
          <p className="mt-1 max-w-prose text-xs text-text-secondary">
            Yang bisa dibayar hanya tagihan yang sudah <strong>diposting</strong> dan
            masih bersisa. Tagihan draft belum menjadi kewajiban siapa pun. Buka{" "}
            <Link href="/accounting/hutang" className="text-accent hover:underline">
              Hutang Usaha
            </Link>{" "}
            untuk mencatat atau memposting tagihan lebih dulu.
          </p>
        </Card>
      ) : (
        <Card className="border-accent/30 bg-accent-light/40">
          <p className="text-sm font-medium text-text-primary">
            {antrean.length} tagihan menunggu dibayar
          </p>
          <p className="mt-1 max-w-prose text-xs text-text-secondary">
            Jatuh tempo paling dekat:{" "}
            <strong>
              {antrean[0].invoice_number} — {antrean[0].vendor_name || `#${antrean[0].vendor_id}`}
            </strong>
            . Sisa tiap tagihan terbaca di{" "}
            <Link href="/accounting/hutang" className="text-accent hover:underline">
              Hutang Usaha
            </Link>
            .
          </p>
        </Card>
      )}

      <APPaymentList
        payments={payments}
        vendors={vendors}
        canWrite={canWrite}
        loadError={loadError}
      />
    </div>
  );
}
