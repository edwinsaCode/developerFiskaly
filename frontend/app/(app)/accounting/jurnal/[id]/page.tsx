import { cookies } from "next/headers";
import { JournalDetail } from "@/components/accounting/JournalDetail";

export const metadata = { title: "Detail Jurnal — NATA ALAM RAYA" };

interface Props {
  params: Promise<{ id: string }>;
}

export default async function JurnalDetailPage({ params }: Props) {
  const { id } = await params;
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value ?? "";
  return (
    <div className="space-y-6">
      <JournalDetail token={token} id={Number(id)} />
    </div>
  );
}
