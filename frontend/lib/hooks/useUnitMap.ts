"use client";

import { useEffect, useState } from "react";
import { fetchProjects, fetchUnitsByProject } from "@/lib/api/projects";

export interface UnitRef {
  code: string;
  projectName: string;
  projectId: number;
  status: string;
}

/**
 * useUnitMap — peta unit_id → {code, projectName} untuk tabel lintas-fitur
 * (booking/pembatalan/komisi) yang hanya membawa unit_id dari API.
 * Dimuat sekali per mount. (Backlog PS-1: endpoint list ber-denormalisasi.)
 */
export function useUnitMap(token: string) {
  const [map, setMap] = useState<Record<number, UnitRef>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const projects = await fetchProjects(token);
        const entries: Record<number, UnitRef> = {};
        await Promise.all(
          projects.map(async (p) => {
            const units = await fetchUnitsByProject(token, p.id).catch(() => []);
            for (const u of units) {
              entries[u.id] = {
                code: u.code,
                projectName: p.name,
                projectId: p.id,
                status: u.status,
              };
            }
          }),
        );
        if (alive) setMap(entries);
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [token]);

  return { unitMap: map, unitMapLoading: loading };
}
