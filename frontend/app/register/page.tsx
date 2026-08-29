"use client";

import { useState, FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { registerViaProxy } from "@/lib/api/auth";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";
import { BRAND_PREFIX, BRAND_ACCENT } from "@/lib/brand";

export default function RegisterPage() {
  const router = useRouter();
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const tenantName = fd.get("tenant_name") as string;
    const email = fd.get("email") as string;
    const password = fd.get("password") as string;

    setLoading(true);
    try {
      await registerViaProxy(tenantName, email, password);
      toast("Registrasi berhasil — selamat datang!", "success");
      router.push("/dashboard");
    } catch (err) {
      toast(err instanceof Error ? err.message : "Registrasi gagal", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg px-4">
      <div className="w-full max-w-sm">
        {/* Brand */}
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-extrabold tracking-tight text-text-primary">
            {BRAND_PREFIX} <span className="text-accent">{BRAND_ACCENT}</span>
          </h1>
          <p className="mt-1 text-sm text-text-secondary">
            Daftarkan perusahaan Anda
          </p>
        </div>

        <form
          onSubmit={handleSubmit}
          className="bg-surface border border-border rounded-xl shadow-sm p-7 space-y-5"
        >
          <Input
            name="tenant_name"
            type="text"
            label="Nama Perusahaan"
            placeholder="PT Properti Sejahtera"
            required
            autoComplete="organization"
          />
          <Input
            name="email"
            type="email"
            label="Email"
            placeholder="nama@perusahaan.com"
            required
            autoComplete="email"
          />
          <Input
            name="password"
            type="password"
            label="Kata sandi"
            placeholder="••••••••"
            required
            minLength={8}
            autoComplete="new-password"
          />
          <Button type="submit" className="w-full" loading={loading}>
            Daftar
          </Button>

          <p className="text-center text-sm text-text-secondary">
            Sudah punya akun?{" "}
            <Link href="/login" className="text-accent hover:underline font-medium">
              Masuk
            </Link>
          </p>
        </form>
      </div>
    </div>
  );
}
