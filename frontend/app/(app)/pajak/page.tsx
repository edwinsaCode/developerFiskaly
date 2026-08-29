import { cookies } from "next/headers";
import { fetchProjects, fetchUnitsByProject } from "@/lib/api/projects";
import { TaxDashboard } from "@/components/pajak/TaxDashboard";
import { Card } from "@/components/ui/Card";
import { ErrorState } from "@/components/ui/ErrorState";
import type { Unit } from "@/lib/types/api";

const COOKIE_NAME = "esa_session";

async function getToken() {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

export default async function PajakPage() {
  const token = await getToken();

  let soldUnits: Unit[] = [];
  let fetchError = false;

  try {
    const projects = await fetchProjects(token);
    const unitResults = await Promise.allSettled(
      projects.map((p) => fetchUnitsByProject(token, p.id)),
    );
    const allUnits = unitResults
      .filter((r): r is PromiseFulfilledResult<Unit[]> => r.status === "fulfilled")
      .flatMap((r) => r.value);
    soldUnits = allUnits.filter((u) => u.status === "sold");
  } catch {
    fetchError = true;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="display-lg text-2xl text-text-primary">Pajak</h1>
        <p className="text-sm text-text-secondary mt-0.5">
          PPh Final Pengalihan (Event 5a/5b) dan PPN Keluaran
        </p>
      </div>

      {fetchError ? (
        <Card>
          <ErrorState
            title="Gagal Memuat Data Pajak"
            description="Data unit tidak dapat dimuat. Pastikan backend berjalan dan coba lagi."
          />
        </Card>
      ) : (
        <TaxDashboard soldUnits={soldUnits} token={token} />
      )}
    </div>
  );
}
