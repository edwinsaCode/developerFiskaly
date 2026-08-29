import { cookies } from "next/headers";
import { SettingsHub } from "@/components/settings/SettingsHub";

const COOKIE_NAME = "esa_session";

export default async function PengaturanPage() {
  const store = await cookies();
  const token = store.get(COOKIE_NAME)?.value ?? "";

  return (
    <div className="space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Pengaturan</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          Master data operasional: sales, skema pembayaran, bank, pelanggan, dan pengguna
        </p>
      </div>
      <SettingsHub token={token} />
    </div>
  );
}
