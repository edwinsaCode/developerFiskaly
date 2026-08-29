import { cookies } from "next/headers";
import { DocumentRegisterView } from "@/components/accounting/DocumentRegisterView";
import { AccountingNav } from "@/components/accounting/AccountingNav";

export const metadata = { title: "Buku Dokumen — NATA ALAM RAYA" };

export default async function DokumenPage() {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value ?? "";
  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Buku Dokumen</h1>
        <p className="text-sm text-text-secondary mt-1">
          Setiap nomor bukti yang pernah terbit, dan jurnal yang dibuktikannya
        </p>
      </div>
      <DocumentRegisterView token={token} />
    </div>
  );
}
