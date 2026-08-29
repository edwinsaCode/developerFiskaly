import { cookies } from "next/headers";
import { CommissionBoard } from "@/components/commission/CommissionBoard";

const COOKIE_NAME = "esa_session";

export default async function KomisiPage() {
  const store = await cookies();
  const token = store.get(COOKIE_NAME)?.value ?? "";

  return (
    <div>
      <CommissionBoard token={token} />
    </div>
  );
}
