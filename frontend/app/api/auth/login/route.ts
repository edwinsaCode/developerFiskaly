import { NextRequest, NextResponse } from "next/server";
import {
  SESSION_COOKIE,
  sessionCookieOptions,
  backendErrorMessage,
} from "@/lib/session-cookie";

const BACKEND = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export async function POST(req: NextRequest) {
  const body = await req.json();

  const backendRes = await fetch(`${BACKEND}/api/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  if (!backendRes.ok) {
    const text = await backendRes.text();
    return NextResponse.json(
      { error: backendErrorMessage(text, "Autentikasi gagal") },
      { status: backendRes.status }
    );
  }

  const data = await backendRes.json();
  const token: string = data.token;

  const res = NextResponse.json({ user: data.user });
  res.cookies.set(SESSION_COOKIE, token, sessionCookieOptions(token));
  return res;
}
