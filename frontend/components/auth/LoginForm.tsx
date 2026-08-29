"use client";

import { useState, FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { loginViaProxy } from "@/lib/api/auth";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";

// Kelas kotak isian disalin dari components/ui/Input agar field kata sandi
// dengan tombol lihat/sembunyi tetap terlihat identik dengan field lain.
// (Input bersama sengaja tidak diubah hanya demi satu layar.)
const FIELD =
  "w-full rounded-lg border border-border bg-surface px-3.5 py-2.5 pr-12 text-sm text-text-primary" +
  " placeholder:text-text-tertiary outline-none transition-shadow transition-colors" +
  " focus:border-accent focus:ring-2 focus:ring-accent/25";

export function LoginForm({ expired }: { expired: boolean }) {
  const router = useRouter();
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const email = fd.get("email") as string;
    const password = fd.get("password") as string;

    setLoading(true);
    try {
      const res = await loginViaProxy(email, password);
      // Marketing tidak punya Dashboard (KPI keuangan). Mengantarnya ke sana
      // hanya untuk dilempar balik oleh middleware adalah kedipan yang tak perlu.
      router.push(res.user?.role === "marketing" ? "/penjualan" : "/dashboard");
    } catch (err) {
      toast(err instanceof Error ? err.message : "Login gagal", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-5">
      {/* Sesi berakhir: alasan kembali ke halaman ini harus terbaca di halaman
          itu sendiri — toast sudah lama hilang saat halaman selesai dimuat. */}
      {expired && (
        <div
          role="status"
          className="rounded-lg border border-warning/30 bg-warning-bg px-3.5 py-3 text-sm text-warning"
        >
          Sesi Anda telah berakhir. Silakan login kembali.
        </div>
      )}

      <Input
        name="email"
        type="email"
        label="Email"
        placeholder="nama@perusahaan.com"
        required
        autoComplete="email"
        autoFocus
      />

      <div className="flex flex-col gap-1.5">
        <label htmlFor="login-password" className="text-sm font-medium text-text-primary">
          Kata sandi<span className="text-danger ml-0.5">*</span>
        </label>
        <div className="relative">
          <input
            id="login-password"
            name="password"
            // Hanya `type` yang berubah saat ditampilkan — nilai isian tidak
            // pernah disentuh, sehingga apa yang diketik tetap apa yang dikirim.
            type={showPassword ? "text" : "password"}
            placeholder="••••••••"
            required
            autoComplete="current-password"
            className={FIELD}
          />
          <button
            type="button"
            onClick={() => setShowPassword((v) => !v)}
            aria-pressed={showPassword}
            aria-controls="login-password"
            aria-label={showPassword ? "Sembunyikan kata sandi" : "Tampilkan kata sandi"}
            title={showPassword ? "Sembunyikan kata sandi" : "Tampilkan kata sandi"}
            className="absolute inset-y-0 right-0 flex items-center px-3 text-text-tertiary
              hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/40
              rounded-r-lg transition-colors"
          >
            {showPassword ? (
              // mata tercoret = sedang terlihat, klik untuk menyembunyikan
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
                <path d="M3 3l18 18" />
                <path d="M10.6 10.6a2 2 0 002.8 2.8" />
                <path d="M9.4 5.2A9.6 9.6 0 0112 5c5 0 9 4.5 9 7 0 .9-.6 2.1-1.6 3.3M6.2 6.7C3.9 8.2 3 10.2 3 12c0 2.5 4 7 9 7 1.4 0 2.7-.3 3.8-.9" />
              </svg>
            ) : (
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
                <path d="M3 12s3.5-7 9-7 9 7 9 7-3.5 7-9 7-9-7-9-7z" />
                <circle cx="12" cy="12" r="2.6" />
              </svg>
            )}
          </button>
        </div>
      </div>

      <Button type="submit" className="w-full" loading={loading}>
        Masuk
      </Button>

      <p className="text-center text-sm text-text-secondary">
        Belum punya akun?{" "}
        <Link href="/register" className="text-accent hover:underline font-medium">
          Daftar
        </Link>
      </p>
    </form>
  );
}
