import { cookies } from "next/headers";
import { fetchProjects } from "@/lib/api/projects";
import { ProjectPickerList } from "@/components/proyek/ProjectPickerList";
import type { Project } from "@/lib/types/api";

const COOKIE_NAME = "esa_session";

async function getToken(): Promise<string> {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

// RAB per-proyek: tampilkan pemilih proyek (bukan redirect ke /proyek).
export default async function RABIndexPage() {
  const token = await getToken();

  let projects: Project[] = [];
  let fetchError = false;
  try {
    projects = await fetchProjects(token);
  } catch {
    fetchError = true;
  }

  return (
    <ProjectPickerList
      title="Rencana Anggaran Biaya (RAB)"
      description="Pilih proyek untuk menyusun & meninjau RAB-nya."
      feature="rab"
      cta="Buka RAB"
      emptyDescription="RAB dikelola per proyek. Tambahkan proyek terlebih dahulu untuk mulai menyusun anggaran biaya."
      projects={projects}
      fetchError={fetchError}
    />
  );
}
