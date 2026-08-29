import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { ApiError } from "@/lib/api/client";
import { fetchKPRPipeline, type KPRPipelineReport } from "@/lib/api/reports";
import { KPRPipelineBoard } from "@/components/kpr/KPRPipelineBoard";
import { Card } from "@/components/ui/Card";
import { ErrorState } from "@/components/ui/ErrorState";

const COOKIE_NAME = "esa_session";

// R1 — Pipeline KPR: pengajuan → SP3K → akad → dana cair → lunas.
export default async function KPRPipelinePage() {
  const store = await cookies();
  const token = store.get(COOKIE_NAME)?.value ?? "";

  let report: KPRPipelineReport | null = null;
  try {
    report = await fetchKPRPipeline(token);
  } catch (err) {
    // Sesi kadaluarsa → langsung ke login (jangan error misterius).
    if (err instanceof ApiError && err.status === 401) redirect("/login");
    console.error("[kpr-pipeline] fetch gagal:", err);
    report = null;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Pipeline KPR</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          Posisi semua kontrak KPR: pengajuan → SP3K → akad → pencairan → lunas.
          Kartu menua diberi peringatan — kejar yang kuning dulu.
        </p>
      </div>

      {report ? (
        <KPRPipelineBoard report={report} />
      ) : (
        <Card>
          <ErrorState
            title="Gagal memuat pipeline KPR"
            description="Muat ulang halaman, atau periksa koneksi backend."
          />
        </Card>
      )}
    </div>
  );
}
