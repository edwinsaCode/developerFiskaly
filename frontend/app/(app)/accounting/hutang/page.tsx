import Link from "next/link";

import { getTokenAndRole } from "@/lib/auth";
import { fetchAPInvoices, fetchVendors } from "@/lib/api/ap";
import { fetchProjects } from "@/lib/api/projects";
import { errText } from "@/components/ap/apRules";
import { APInvoiceList } from "@/components/ap/APInvoiceList";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { ExportPdfButton } from "@/components/laporan/ExportPdfButton";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import type { APInvoiceView, Project, Vendor } from "@/lib/types/api";

export const metadata = { title: "Hutang Usaha — NATA ALAM RAYA" };

// Hutang usaha: kewajiban kepada vendor yang lahir saat pekerjaannya diakui,
// bukan saat uangnya dibayar. Halaman ini adalah daftar pengakuannya.
export default async function HutangPage() {
  const { token, canWrite } = await getTokenAndRole();

  let invoices: APInvoiceView[] = [];
  let loadError: string | undefined;
  try {
    invoices = await fetchAPInvoices(token);
  } catch (e) {
    loadError = errText(e);
  }

  const [vendors, projects] = await Promise.all([
    fetchVendors(token).catch(() => [] as Vendor[]),
    fetchProjects(token).catch(() => [] as Project[]),
  ]);

  const noVendor = vendors.filter((v) => v.is_active).length === 0;

  return (
    <div className="mx-auto w-full max-w-7xl space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Hutang Usaha</h1>
          <p className="mt-1 max-w-prose text-sm text-text-secondary">
            Tagihan vendor yang sudah diterima perusahaan. Kewajibannya diakui saat
            tagihan diposting — biayanya masuk realisasi RAB pada saat yang sama,
            tanpa menunggu uangnya keluar.
          </p>
        </div>
        <div className="flex items-center gap-2">
          {invoices.length > 0 && (
            <ExportPdfButton token={token} report="ap-invoices" label="Export PDF" />
          )}
          {canWrite && (
            <Link href="/accounting/hutang/baru">
              <Button>Catat Tagihan Vendor</Button>
            </Link>
          )}
        </div>
      </div>

      <AccountingNav />

      {noVendor && (
        <Card className="border-warning/30 bg-warning-bg/50">
          <p className="text-sm font-medium text-warning">Belum ada vendor aktif</p>
          <p className="mt-1 max-w-prose text-xs text-text-secondary">
            Setiap tagihan menunjuk satu vendor, dan status PKP vendor itu yang
            menentukan boleh-tidaknya tagihan membawa PPN Masukan. Tambahkan vendor
            lebih dulu di{" "}
            <Link href="/accounting/vendor" className="text-accent hover:underline">
              Master Vendor
            </Link>
            .
          </p>
        </Card>
      )}

      <APInvoiceList
        invoices={invoices}
        vendors={vendors}
        projects={projects}
        canWrite={canWrite}
        loadError={loadError}
      />
    </div>
  );
}
