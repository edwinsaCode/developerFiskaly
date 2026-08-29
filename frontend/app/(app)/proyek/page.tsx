import { cookies } from "next/headers";
import { fetchProjects } from "@/lib/api/projects";
import { fetchUnitsByProject } from "@/lib/api/projects";
import { Project } from "@/lib/types/api";
import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { NewProjectButton } from "@/components/proyek/NewProjectButton";
import { ProjectList, type ProjectWithUnits } from "@/components/proyek/ProjectList";

const COOKIE_NAME = "esa_session";

async function getToken(): Promise<string> {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

export default async function ProyekListPage() {
  const token = await getToken();

  let projects: Project[] = [];
  let fetchError = false;

  try {
    projects = await fetchProjects(token);
  } catch {
    fetchError = true;
  }

  // Fetch units per proyek secara paralel
  const projectsWithUnits: ProjectWithUnits[] = projects.length > 0
    ? await Promise.all(
        projects.map(async (project): Promise<ProjectWithUnits> => {
          try {
            const units = await fetchUnitsByProject(token, project.id);
            return { project, units };
          } catch {
            return { project, units: null };
          }
        })
      )
    : [];

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="display-lg text-2xl text-text-primary">Daftar Proyek</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            {projects.length} proyek terdaftar
          </p>
        </div>
        <NewProjectButton token={token} />
      </div>

      {fetchError && (
        <Card className="mb-4">
          <ErrorState
            title="Gagal Memuat Proyek"
            description="Tidak dapat mengambil daftar proyek dari server. Periksa koneksi dan coba lagi."
          />
        </Card>
      )}

      {!fetchError && projects.length === 0 && (
        <Card>
          <EmptyState
            icon={
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z" />
              </svg>
            }
            title="Belum ada proyek"
            description="Mulai dengan membuat proyek pertama Anda. Setelah itu Anda bisa menambahkan unit, menyusun RAB, dan mencatat biaya di dalamnya."
            action={<NewProjectButton token={token} />}
          />
        </Card>
      )}

      {!fetchError && projects.length > 0 && (
        <ProjectList rows={projectsWithUnits} />
      )}
    </div>
  );
}
