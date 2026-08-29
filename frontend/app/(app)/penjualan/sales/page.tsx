import { cookies } from "next/headers";
import { SalesWorkspace } from "@/components/sales/SalesWorkspace";

const COOKIE_NAME = "esa_session";

export default async function SalesCRMPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | undefined>>;
}) {
  const store = await cookies();
  const token = store.get(COOKIE_NAME)?.value ?? "";
  const sp = await searchParams;

  return (
    <div>
      <SalesWorkspace token={token} initialTab={sp.tab} />
    </div>
  );
}
