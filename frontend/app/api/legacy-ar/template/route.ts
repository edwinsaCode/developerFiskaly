// Template Piutang Proyek Lama proxy — membaca httpOnly cookie dan meneruskan
// ke Go backend dengan Authorization: Bearer. Diperlukan karena berkas ini
// diunduh lewat navigasi <a href download>, yang TIDAK bisa menyertakan header
// custom — hanya cookie ikut otomatis. Tanpa proxy ini, backend menolak
// (401 JSON tanpa Authorization header) dan browser menyimpan JSON itu sebagai
// "template.xlsx". Sama pola dengan app/api/reports/csv/route.ts, tapi biner
// (arrayBuffer), bukan teks — xlsx yang di-.text()-kan akan rusak encodingnya.

import { cookies } from "next/headers";
import { NextResponse } from "next/server";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export async function GET(): Promise<NextResponse> {
  const cookieStore = await cookies();
  const token = cookieStore.get("esa_session")?.value;
  if (!token) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }

  const backendRes = await fetch(`${API_URL}/api/v1/legacy-ar/template`, {
    headers: { Authorization: `Bearer ${token}` },
  });

  if (!backendRes.ok) {
    return NextResponse.json({ error: "gagal mengambil template" }, { status: backendRes.status });
  }

  const bytes = await backendRes.arrayBuffer();

  return new NextResponse(bytes, {
    status: 200,
    headers: {
      "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      "Content-Disposition": `attachment; filename="template-piutang-proyek-lama.xlsx"`,
    },
  });
}
