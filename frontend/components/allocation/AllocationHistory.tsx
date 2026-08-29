"use client";

import { useState, useEffect } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { fetchAllocationHistory } from "@/lib/api/allocation";
import type { AllocationExecution, AllocationBasis } from "@/lib/types/api";

function formatDateTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "—";
  return new Intl.DateTimeFormat("id-ID", {
    day: "numeric", month: "short", year: "numeric",
    hour: "2-digit", minute: "2-digit",
  }).format(d);
}

const BASIS_LABEL: Record<AllocationBasis, string> = {
  saleable_area: "Luas Area",
  sales_value:   "Nilai Jual",
};

interface Props {
  token: string;
  projectId: number;
  refreshKey: number;
}

export function AllocationHistory({ token, projectId, refreshKey }: Props) {
  const [history, setHistory]   = useState<AllocationExecution[]>([]);
  const [loading, setLoading]   = useState(true);
  const [error, setError]       = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchAllocationHistory(token, projectId)
      .then((data) => { if (!cancelled) setHistory(data); })
      .catch((e) => { if (!cancelled) setError(e?.message ?? "Gagal memuat riwayat"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [token, projectId, refreshKey]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Riwayat Eksekusi Alokasi</CardTitle>
        <p className="text-xs text-text-secondary mt-0.5">
          Audit log setiap kali alokasi HPP dijalankan untuk proyek ini.
        </p>
      </CardHeader>

      {loading ? (
        <p className="text-sm text-text-secondary animate-pulse">Memuat riwayat…</p>
      ) : error ? (
        <div className="space-y-3">
          <p className="text-sm text-danger">{error}</p>
          <Button variant="secondary" size="sm" onClick={() => setLoading(true)}>
            Coba Lagi
          </Button>
        </div>
      ) : history.length === 0 ? (
        <EmptyState
          title="Belum ada riwayat"
          description="Jalankan alokasi pertama kali untuk mencatat histori eksekusi."
        />
      ) : (
        <div className="-mx-4 -mb-4 overflow-x-auto">
          <Table>
            <TableHead>
              <TableRow>
                <Th right>#</Th>
                <Th>Basis</Th>
                <Th right>Total HPP Dialokasikan</Th>
                <Th>Dijalankan Oleh</Th>
                <Th>Waktu Eksekusi</Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {history.map((exec, idx) => (
                <TableRow key={exec.id}>
                  <Td right>
                    <span className="text-text-tertiary font-mono text-xs">
                      {history.length - idx}
                    </span>
                  </Td>
                  <Td>
                    <span className="text-sm font-medium">
                      {BASIS_LABEL[exec.basis] ?? exec.basis}
                    </span>
                  </Td>
                  <Td right>
                    <Rupiah value={exec.total_cost} />
                  </Td>
                  <Td>
                    <span className="text-sm text-text-secondary">{exec.user_email}</span>
                  </Td>
                  <Td>
                    <span className="text-sm whitespace-nowrap">
                      {formatDateTime(exec.executed_at)}
                    </span>
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Card>
  );
}
