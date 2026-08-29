import { getTokenAndRole } from "@/lib/auth";
import { fetchBillingPlan, type BillingPlan } from "@/lib/api/sale";
import { BillingPlanView } from "@/components/accounting/BillingPlanView";
import { ErrorState } from "@/components/ui/ErrorState";
import { todayLocalStr } from "@/lib/date";

export const metadata = { title: "Jadwal Penagihan — NATA ALAM RAYA" };

const today = () => todayLocalStr();

interface PageProps {
  searchParams: Promise<Record<string, string | undefined>>;
}

// W-8 · BD-1 — kewajiban terjadwal SEBELUM serah terima.
//
// Piutang harga rumah lahir saat BAST, sehingga cicilan pra-BAST keluar dari
// laporan piutang. Cicilannya tetap wajib ditagih; halaman ini yang memegangnya
// supaya tim penagihan tidak kehilangan daftar kerjanya.
export default async function BillingSchedulePage({ searchParams }: PageProps) {
  const sp = await searchParams;
  const asOf = sp.as_of ?? today();

  const { token } = await getTokenAndRole();

  let plan: BillingPlan | null = null;
  try {
    plan = await fetchBillingPlan(token, asOf);
  } catch {
    plan = null;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Jadwal Penagihan</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          Cicilan yang harus ditagih pada kontrak yang unitnya belum diserahterimakan (BAST).
          Rencana penagihan — belum menjadi piutang.
        </p>
      </div>

      {plan === null ? (
        <ErrorState
          title="Gagal memuat Jadwal Penagihan"
          description="Data jadwal tidak dapat diambil dari server. Periksa koneksi lalu coba lagi."
        />
      ) : (
        <BillingPlanView plan={plan} asOf={asOf} />
      )}
    </div>
  );
}
