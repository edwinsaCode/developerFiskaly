import { ReactNode } from "react";
import { Sidebar } from "./Sidebar";
import { Header } from "./Header";
import { CommandPalette } from "./CommandPalette";
import { UserProvider } from "@/lib/context/UserContext";
import { User } from "@/lib/types/api";

interface AppShellProps {
  children: ReactNode;
  user: User;
  periodLabel?: string;
  /** R7 — token untuk command palette (jump ke unit/customer). */
  token?: string;
}

export function AppShell({ children, user, periodLabel, token }: AppShellProps) {
  return (
    <UserProvider user={user}>
      <div className="flex min-h-screen bg-bg">
        <Sidebar />
        <div className="flex flex-col flex-1 min-w-0">
          <Header user={user} periodLabel={periodLabel} />
          {/* Kanvas halaman — SATU-SATUNYA tempat gutter & lebar maksimum konten
              didefinisikan. Halaman TIDAK boleh menambah padding sendiri (dulu
              banyak page memakai p-6 di atas p-6 ini → padding ganda & tidak
              konsisten antar halaman). max-w menjaga baris data tetap terbaca
              di layar ultrawide, mx-auto menjaga konten terpusat — bukan
              menempel kiri dengan ruang kosong menganga di kanan. */}
          <main className="flex-1 overflow-auto">
            <div className="mx-auto w-full max-w-[1560px] px-4 py-5 sm:px-6 sm:py-6 lg:px-8">
              {children}
            </div>
          </main>
        </div>
      </div>
      {token && <CommandPalette token={token} />}
    </UserProvider>
  );
}
