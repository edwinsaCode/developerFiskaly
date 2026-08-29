import { getTokenAndRole } from "@/lib/auth";
import { fetchVendors } from "@/lib/api/ap";
import { errText } from "@/components/ap/apRules";
import { VendorView } from "@/components/ap/VendorView";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import type { Vendor } from "@/lib/types/api";

export const metadata = { title: "Master Vendor — NATA ALAM RAYA" };

// Master vendor adalah pintu masuk hutang usaha: setiap tagihan menunjuk satu
// vendor, dan status PKP vendor itu yang memutuskan boleh-tidaknya tagihan
// membawa PPN Masukan.
export default async function VendorPage() {
  const { token, canWrite } = await getTokenAndRole();

  let vendors: Vendor[] = [];
  let loadError: string | undefined;
  try {
    // Tanpa saringan `active`: layar menampilkan vendor nonaktif juga bila
    // diminta, dan penyaringannya dilakukan di klien tanpa perjalanan ulang.
    vendors = await fetchVendors(token);
  } catch (e) {
    loadError = errText(e);
  }

  return (
    <div className="mx-auto w-full max-w-6xl space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Master Vendor</h1>
        <p className="mt-1 max-w-prose text-sm text-text-secondary">
          Daftar pihak yang menagih perusahaan. Vendor tidak pernah dihapus —
          yang sudah tidak dipakai cukup dinonaktifkan, sehingga tagihan lama
          tetap menunjuk data yang sama.
        </p>
      </div>

      <AccountingNav />

      <VendorView vendors={vendors} canWrite={canWrite} loadError={loadError} />
    </div>
  );
}
