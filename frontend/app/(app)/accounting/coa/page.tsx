import { cookies } from "next/headers";
import { COAManager } from "@/components/accounting/COAManager";
import { AccountingNav } from "@/components/accounting/AccountingNav";

export const metadata = { title: "Daftar Akun — NATA ALAM RAYA" };

export default async function COAPage() {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value ?? "";

  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Daftar Akun</h1>
        <p className="text-sm text-text-secondary mt-1">
          Chart of Accounts — kelola akun buku besar untuk tenant ini
        </p>
      </div>
      <COAManager token={token} />
    </div>
  );
}
