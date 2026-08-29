import Link from "next/link";
import { notFound } from "next/navigation";
import { getTokenAndRole } from "@/lib/auth";
import { AccountingNav } from "@/components/accounting/AccountingNav";
import { LegacyARDetail } from "@/components/accounting/LegacyARDetail";
import { fetchLegacyReceivable } from "@/lib/api/legacyar";
import type { LegacyDetail } from "@/lib/api/legacyar";

export const metadata = { title: "Rincian Piutang Proyek Lama — NATA ALAM RAYA" };

interface PageProps {
  params: Promise<{ id: string }>;
}

export default async function LegacyARDetailPage({ params }: PageProps) {
  const { id } = await params;
  const numericID = Number(id);
  if (!Number.isFinite(numericID) || numericID <= 0) notFound();

  const { token } = await getTokenAndRole();

  let detail: LegacyDetail;
  try {
    detail = await fetchLegacyReceivable(token, numericID);
  } catch {
    notFound();
  }

  return (
    <div className="space-y-6">
      <AccountingNav />
      <div>
        <Link href="/accounting/legacy-ar" className="text-sm text-accent hover:underline">
          ← Piutang Proyek Lama
        </Link>
        <h1 className="display-lg mt-1 text-2xl text-text-primary">Rincian Piutang</h1>
      </div>

      <LegacyARDetail token={token} initial={detail} />
    </div>
  );
}
