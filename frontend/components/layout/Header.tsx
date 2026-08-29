"use client";

import { useEffect, useState } from "react";
import { useRouter, usePathname } from "next/navigation";
import { logoutViaProxy } from "@/lib/api/auth";
import { ROLE_LABELS } from "@/lib/roles";
import { User } from "@/lib/types/api";
import { Sidebar } from "./Sidebar";

interface HeaderProps {
  user?: User;
  periodLabel?: string;
}

export function Header({ user, periodLabel }: HeaderProps) {
  const router = useRouter();
  const pathname = usePathname();
  // R7/U7 — drawer navigasi mobile (sidebar disembunyikan <md).
  const [navOpen, setNavOpen] = useState(false);
  useEffect(() => { setNavOpen(false); }, [pathname]); // tutup saat pindah halaman

  async function handleLogout() {
    await logoutViaProxy();
    router.push("/login");
    router.refresh();
  }

  return (
    // h-16 = tinggi blok brand di Sidebar → garis bawah header dan garis bawah
    // logo sejajar sempurna. Sticky agar identitas periode/akun tetap terlihat
    // saat tabel panjang di-scroll. Gutter mengikuti kanvas konten (AppShell).
    <header className="h-16 shrink-0 sticky top-0 z-header flex items-center justify-between gap-3
      px-4 sm:px-6 lg:px-8 border-b border-border bg-surface/95 backdrop-blur">
      <div className="flex items-center gap-3 min-w-0">
        {/* Hamburger — hanya mobile */}
        <button
          onClick={() => setNavOpen(true)}
          className="md:hidden -ml-2 p-2 text-text-secondary hover:text-accent"
          aria-label="Buka menu navigasi"
        >
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor"
            strokeWidth="2" strokeLinecap="round"><path d="M4 6h16M4 12h16M4 18h16" /></svg>
        </button>
        {/* Period indicator */}
        <div className="min-w-0 truncate text-sm text-text-secondary">
          {periodLabel ?? "—"}
        </div>
      </div>

      {/* Drawer mobile */}
      {navOpen && (
        <div className="fixed inset-0 z-overlay md:hidden" onClick={() => setNavOpen(false)}>
          <div className="absolute inset-0 bg-text-primary/30 backdrop-blur-sm" />
          <div
            className="absolute inset-y-0 left-0 shadow-2xl border-r border-border"
            onClick={(e) => e.stopPropagation()}
          >
            <Sidebar drawer />
          </div>
        </div>
      )}

      {/* Right cluster */}
      <div className="flex items-center gap-3 sm:gap-4 shrink-0">
        {/* Identitas disembunyikan di layar sempit — sudah ada di kartu footer
            sidebar; di header ia hanya mendorong tombol keluar.
            Nama di atas, email + role di bawah: pertanyaan "saya login sebagai
            siapa, dengan hak apa" harus terjawab tanpa membuka menu apa pun. */}
        {user && (
          <div className="hidden sm:block max-w-[260px] text-right leading-tight">
            <span className="block truncate text-sm font-medium text-text-primary">
              {user.display_name || user.email}
            </span>
            <span className="block truncate text-[11px] text-text-tertiary">
              {user.name ? `${user.email} · ` : ""}{ROLE_LABELS[user.role] ?? user.role}
            </span>
          </div>
        )}
        <button
          onClick={handleLogout}
          className="text-xs text-text-secondary hover:text-danger transition-colors font-medium"
        >
          Keluar
        </button>
      </div>
    </header>
  );
}
