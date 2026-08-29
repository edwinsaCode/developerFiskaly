"use client";

import { Button } from "@/components/ui/Button";

interface Props {
  report: string;
  params?: Record<string, string>;
  label?: string;
}

export function ExportCsvButton({ report, params = {}, label = "Export CSV" }: Props) {
  function handleClick() {
    const qs = new URLSearchParams({ report, ...params });
    window.location.href = `/api/reports/csv?${qs.toString()}`;
  }

  return (
    <Button variant="secondary" size="sm" onClick={handleClick}>
      ↓ {label}
    </Button>
  );
}
