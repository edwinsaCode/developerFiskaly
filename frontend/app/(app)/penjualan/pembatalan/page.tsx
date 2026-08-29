import { cookies } from "next/headers";
import { CancellationCenter } from "@/components/cancellation/CancellationCenter";

const COOKIE_NAME = "esa_session";

interface PageProps {
  searchParams: Promise<{ tab?: string; open?: string }>;
}

export default async function PembatalanPage({ searchParams }: PageProps) {
  const store = await cookies();
  const token = store.get(COOKIE_NAME)?.value ?? "";
  const { tab, open } = await searchParams;
  const openId = open ? parseInt(open, 10) : undefined;

  return (
    <div>
      <CancellationCenter
        token={token}
        initialTab={tab === "refund" ? "refund" : "cancellation"}
        openId={openId && !isNaN(openId) ? openId : undefined}
      />
    </div>
  );
}
