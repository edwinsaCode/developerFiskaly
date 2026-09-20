"use client";

import { Button } from "@/components/ui/Button";

interface Props {
  report: string;
  params?: Record<string, string>;
  label?: string;
}

// Excel (.xlsx) sungguhan — dibangkitkan backend (excelize) dari data yang sama
// dengan laporan/PDF, lewat proxy /api/reports/csv?format=xlsx (cookie → Bearer).
export function ExportExcelButton({ report, params = {}, label = "Export Excel" }: Props) {
  function handleClick() {
    const qs = new URLSearchParams({ report, ...params, format: "xlsx" });
    window.location.href = `/api/reports/csv?${qs.toString()}`;
  }

  return (
    <Button variant="secondary" size="sm" onClick={handleClick}>
      ↓ {label}
    </Button>
  );
}
