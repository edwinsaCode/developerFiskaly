import Link from "next/link";

import { getTokenAndRole } from "@/lib/auth";
import { fetchAPInvoices, fetchVendors } from "@/lib/api/ap";
import { eligibleInvoicesOf } from "@/components/ap/paymentRules";
import { APPaymentForm } from "@/components/ap/APPaymentForm";
import { Card } from "@/components/ui/Card";
import type { APInvoiceView, Vendor } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Bayar Tagihan Vendor — NATA ALAM RAYA" };

// Tanggal dari server, bukan dari jam browser: tanggal bayar menentukan periode
// akuntansi dan nomor BKK, dan jam perangkat operator bukan sumber yang bisa
// dipercaya untuk keduanya.
function todayISO(): string {
  return todayLocalStr();
}

export default async function PembayaranBaruPage({
  searchParams,
}: {
  searchParams: Promise<{ vendor?: string; invoice?: string }>;
}) {
  const { vendor: vendorParam, invoice: invoiceParam } = await searchParams;
  const { token, canWrite } = await getTokenAndRole();

  const [vendors, invoices] = await Promise.all([
    fetchVendors(token, { active: true }).catch(() => [] as Vendor[]),
    fetchAPInvoices(token).catch(() => [] as APInvoiceView[]),
  ]);

  const antrean = eligibleInvoicesOf(invoices, "");

  // Prefill dari tombol "Bayar" di detail tagihan. Vendornya diambil dari
  // tagihannya sendiri, bukan dari query — supaya tautan yang dikutip separuh
  // tidak bisa memasangkan tagihan dengan vendor yang salah.
  const prefillInvoice = invoiceParam
    ? antrean.find((inv) => String(inv.id) === invoiceParam)
    : undefined;
  const initialVendorId = prefillInvoice
    ? String(prefillInvoice.vendor_id)
    : (vendorParam ?? "");

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <Link
          href="/accounting/pembayaran-vendor"
          className="text-xs text-text-secondary hover:text-accent"
        >
          ← Pembayaran Vendor
        </Link>
        <h1 className="display-lg mt-1 text-2xl text-text-primary">Bayar Tagihan Vendor</h1>
        <p className="mt-1 max-w-prose text-sm text-text-secondary">
          Uang keluar dari kas/bank dan hutang usaha berkurang. Tinjau alokasi serta
          jurnalnya lebih dulu — tidak ada yang tersimpan sebelum Anda mengonfirmasi.
        </p>
      </div>

      {!canWrite ? (
        <Card>
          <p className="text-sm text-text-secondary">
            Peran Anda hanya bisa melihat. Pembayaran hutang vendor mengeluarkan uang
            dari kas perusahaan dan memerlukan peran Pemilik atau Accounting.
          </p>
        </Card>
      ) : antrean.length === 0 ? (
        <Card className="border-warning/30 bg-warning-bg/50">
          <p className="text-sm font-medium text-warning">
            Tidak ada tagihan yang bisa dibayar
          </p>
          <p className="mt-1 max-w-prose text-xs text-text-secondary">
            Pembayaran hanya bisa diarahkan ke tagihan yang sudah diposting dan masih
            bersisa — kewajiban yang belum diakui tidak punya saldo untuk dilunasi.
            Catat atau posting tagihannya lebih dulu di{" "}
            <Link href="/accounting/hutang" className="text-accent hover:underline">
              Hutang Usaha
            </Link>
            .
          </p>
        </Card>
      ) : (
        <APPaymentForm
          token={token}
          canWrite={canWrite}
          vendors={vendors}
          invoices={invoices}
          today={todayISO()}
          initialVendorId={initialVendorId}
          initialInvoiceId={prefillInvoice ? String(prefillInvoice.id) : ""}
        />
      )}
    </div>
  );
}
