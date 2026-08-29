import { cookies } from "next/headers";
import { fetchProjects } from "@/lib/api/projects";
import { ProjectPickerList } from "@/components/proyek/ProjectPickerList";
import type { Project } from "@/lib/types/api";

const COOKIE_NAME = "esa_session";

async function getToken(): Promise<string> {
  const store = await cookies();
  return store.get(COOKIE_NAME)?.value ?? "";
}

// Biaya per-proyek: tampilkan pemilih proyek (bukan redirect ke /proyek).
export default async function BiayaIndexPage() {
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
      title="Biaya Proyek"
      description="Pilih proyek untuk mencatat & meninjau biaya (tanah, hard, soft, pendanaan)."
      feature="biaya"
      cta="Buka Biaya"
      emptyDescription="Biaya dicatat per proyek. Tambahkan proyek terlebih dahulu untuk mulai mencatat biaya."
      projects={projects}
      fetchError={fetchError}
    />
  );
}
