import { cookies } from "next/headers";
import { JournalCreateForm } from "@/components/accounting/JournalCreateForm";

export const metadata = { title: "Buat Jurnal — NATA ALAM RAYA" };

export default async function JurnalCreatePage() {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value ?? "";
  return (
    <div className="space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Buat Jurnal Manual</h1>
        <p className="text-sm text-text-secondary mt-1">
          Entri jurnal berpasangan — debit harus sama dengan kredit
        </p>
      </div>
      <JournalCreateForm token={token} />
    </div>
  );
}
