import { apiFetch, ApiError } from "./client";
import type { AllocationComputeResponse, AllocationConfig, AllocationBasis, AllocationExecution } from "@/lib/types/api";

export async function fetchAllocationConfig(
  token: string,
  projectId: number,
): Promise<AllocationConfig | null> {
  try {
    return await apiFetch<AllocationConfig>(
      `/projects/${projectId}/allocation/config`,
      { token },
    );
  } catch {
    return null;
  }
}

export async function setAllocationConfig(
  token: string,
  projectId: number,
  basis: AllocationBasis,
): Promise<AllocationConfig> {
  return apiFetch<AllocationConfig>(
    `/projects/${projectId}/allocation/config`,
    { token, method: "PUT", body: { basis } },
  );
}

const emptyAllocation: AllocationComputeResponse = {
  data: [],
  totals: { direct: "0", allocated: "0", total: "0" },
};

export async function fetchAllocationCompute(
  token: string,
  projectId: number,
): Promise<AllocationComputeResponse> {
  try {
    const res = await apiFetch<AllocationComputeResponse>(
      `/projects/${projectId}/allocation/compute`,
      { token },
    );
    return { data: res.data ?? [], totals: res.totals ?? emptyAllocation.totals };
  } catch (err) {
    // 404 = belum ada konfigurasi alokasi (proyek baru / tanpa biaya). Ini state
    // normal, bukan error → kembalikan kosong agar UI tampilkan empty state.
    if (err instanceof ApiError && err.status === 404) return emptyAllocation;
    throw err;
  }
}

export async function executeAllocation(
  token: string,
  projectId: number,
): Promise<AllocationExecution> {
  return apiFetch<AllocationExecution>(
    `/projects/${projectId}/allocation/execute`,
    { token, method: "POST" },
  );
}

export async function fetchAllocationHistory(
  token: string,
  projectId: number,
): Promise<AllocationExecution[]> {
  const res = await apiFetch<{ data: AllocationExecution[] }>(
    `/projects/${projectId}/allocation/history`,
    { token },
  );
  return res.data ?? [];
}
