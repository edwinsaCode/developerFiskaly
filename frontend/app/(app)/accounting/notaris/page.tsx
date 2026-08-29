import { getTokenAndRole } from "@/lib/auth";
import { NotaryDepositView } from "@/components/accounting/NotaryDepositView";

export const metadata = { title: "Titipan Notaris (warisan) — NATA ALAM RAYA" };

export default async function NotaryDepositPage() {
  const { token } = await getTokenAndRole();
  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Titipan Notaris (warisan)</h1>
        <p className="text-sm text-text-secondary mt-1">
          Halaman penutup untuk titipan yang dicatat sebelum jalur ini dipindahkan.
          Titipan pihak ketiga sekarang dicatat sebagai <strong>Biaya Realisasi</strong> pada
          unit, dengan jenis biaya dan akun kewajiban dari master.
        </p>
      </div>
      <NotaryDepositView token={token} />
    </div>
  );
}
