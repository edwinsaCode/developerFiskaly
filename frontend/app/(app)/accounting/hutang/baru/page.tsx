import Link from "next/link";

import { getTokenAndRole } from "@/lib/auth";
import { fetchVendors } from "@/lib/api/ap";
import { fetchProjects } from "@/lib/api/projects";
import { APInvoiceForm } from "@/components/ap/APInvoiceForm";
import { Card } from "@/components/ui/Card";
import type { Project, Vendor } from "@/lib/types/api";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Catat Tagihan Vendor — NATA ALAM RAYA" };

// Tanggal diambil dari server, bukan dari jam browser: tanggal tagihan menentukan
// periode akuntansi, dan jam perangkat operator bukan sumber yang bisa dipercaya.
function todayISO(): string {
  return todayLocalStr();
}

export default async function TagihanBaruPage() {
  const { token, canWrite } = await getTokenAndRole();

  const [vendors, projects] = await Promise.all([
    fetchVendors(token, { active: true }).catch(() => [] as Vendor[]),
    fetchProjects(token).catch(() => [] as Project[]),
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
        <h1 className="display-lg mt-1 text-2xl text-text-primary">Catat Tagihan Vendor</h1>
        <p className="mt-1 max-w-prose text-sm text-text-secondary">
          Kewajiban kepada vendor lahir saat pekerjaannya diakui — bukan saat uangnya
          dibayar. Tinjau jurnalnya lebih dulu, lalu simpan sebagai draft atau langsung
          posting.
        </p>
      </div>

      {!canWrite ? (
        <Card>
          <p className="text-sm text-text-secondary">
            Peran Anda hanya bisa melihat. Pencatatan tagihan vendor memerlukan peran
            Pemilik atau Accounting.
          </p>
        </Card>
      ) : vendors.length === 0 ? (
        <Card className="border-warning/30 bg-warning-bg/50">
          <p className="text-sm font-medium text-warning">Belum ada vendor aktif</p>
          <p className="mt-1 max-w-prose text-xs text-text-secondary">
            Setiap tagihan menunjuk satu vendor. Tambahkan vendor lebih dulu di{" "}
            <Link href="/accounting/vendor" className="text-accent hover:underline">
              Master Vendor
            </Link>
            .
          </p>
        </Card>
      ) : (
        <APInvoiceForm vendors={vendors} projects={projects} today={todayISO()} />
      )}
    </div>
  );
}
