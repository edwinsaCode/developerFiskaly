// CSV export proxy — membaca httpOnly cookie dan meneruskan ke Go backend dengan ?format=csv.
// Client tidak pernah menyentuh token; browser hanya membuka URL ini.

import { cookies } from "next/headers";
import { NextRequest, NextResponse } from "next/server";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const REPORT_PATHS: Record<string, (params: URLSearchParams) => string> = {
  "balance-sheet":    (p) => `/api/v1/reports/balance-sheet?format=csv&${p}`,
  "income-statement": (p) => `/api/v1/reports/income-statement?format=csv&${p}`,
  "project-pl":       (p) => {
    const rest = new URLSearchParams(p);
    rest.delete("project_id");
    rest.delete("report");
    return `/api/v1/reports/project-pl/${p.get("project_id") ?? "0"}?format=csv&${rest}`;
  },
  "cash-flow":        (p) => `/api/v1/reports/cash-flow?format=csv&${p}`,
  "sales-pipeline":   ()  => `/api/v1/reports/sales-pipeline?format=csv`,
  "tax-liability":    (p) => `/api/v1/reports/tax-liability?format=csv&${p}`,
  "ar-aging":         (p) => `/api/v1/reports/ar-aging?format=csv&${p}`,
  "trial-balance":    (p) => `/api/v1/reports/trial-balance?format=csv&${p}`,
  "general-ledger":   (p) => `/api/v1/reports/trial-balance/${p.get("account_id") ?? "0"}?format=csv&${p}`,
};

export async function GET(req: NextRequest): Promise<NextResponse> {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value;
  if (!token) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }

  const searchParams = req.nextUrl.searchParams;
  const report = searchParams.get("report") ?? "";
  const pathBuilder = REPORT_PATHS[report];
  if (!pathBuilder) {
    return NextResponse.json({ error: "laporan tidak dikenal" }, { status: 400 });
  }

  const backendPath = pathBuilder(searchParams);
  const backendRes = await fetch(`${API_URL}${backendPath}`, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "text/csv",
    },
  });

  if (!backendRes.ok) {
    return NextResponse.json({ error: "gagal mengambil laporan" }, { status: backendRes.status });
  }

  const csvText = await backendRes.text();
  // BOM UTF-8 agar Excel membaca encoding benar
  const bom = "﻿";
  const reportName = report.replace(/-/g, "_");
  const filename = `${reportName}_${new Date().toISOString().slice(0, 10)}.csv`;

  return new NextResponse(bom + csvText, {
    status: 200,
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": `attachment; filename="${filename}"`,
    },
  });
}
