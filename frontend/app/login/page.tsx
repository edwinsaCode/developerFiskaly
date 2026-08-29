import { LoginForm } from "@/components/auth/LoginForm";
import { BRAND_NAME, BRAND_PREFIX, BRAND_ACCENT } from "@/lib/brand";

// W-12 §5 — layar masuk dua kolom.
//
// Server component: `?expired=1` dibaca di sini dan diturunkan sebagai prop,
// sehingga form tidak perlu useSearchParams (yang di Next 15 menyeret seluruh
// halaman ke dalam Suspense). Logika autentikasi tidak berubah sama sekali —
// halaman ini hanya berganti tata letak.

interface PageProps {
  searchParams: Promise<{ expired?: string }>;
}

export default async function LoginPage({ searchParams }: PageProps) {
  const { expired } = await searchParams;

  return (
    <div className="min-h-screen bg-bg lg:grid lg:grid-cols-[1.1fr_1fr]">
      {/* ── Kiri: panel properti. Disembunyikan di bawah lg — di layar sempit
             ia hanya mendorong form ke bawah lipatan. ─────────────────────── */}
      <aside className="relative hidden overflow-hidden bg-accent lg:flex lg:flex-col lg:justify-end">
        {/* Ilustrasi atap perumahan. Sengaja gambar vektor, bukan foto stok:
            tidak ada foto proyek yang benar-benar milik tenant ini, dan
            memasang foto orang lain sebagai "proyek kami" adalah klaim palsu.
            Ganti blok ini bila pemilik menyediakan fotonya sendiri. */}
        <svg
          className="absolute inset-0 h-full w-full"
          viewBox="0 0 800 1000"
          preserveAspectRatio="xMidYMax slice"
          fill="none"
          aria-hidden="true"
        >
          {/* langit bergradasi hangat */}
          <defs>
            <linearGradient id="sky" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="rgb(var(--color-accent))" />
              <stop offset="100%" stopColor="rgb(var(--color-accent-hover))" />
            </linearGradient>
          </defs>
          <rect width="800" height="1000" fill="url(#sky)" />
          <circle cx="620" cy="200" r="90" fill="rgb(var(--color-surface))" opacity="0.10" />

          {/* deret rumah — atap pelana, dinding, jendela */}
          <g fill="rgb(var(--color-surface))" opacity="0.14">
            <path d="M40 760 L170 660 L300 760 Z" />
            <rect x="70" y="760" width="200" height="180" />
            <path d="M280 800 L400 710 L520 800 Z" />
            <rect x="310" y="800" width="180" height="140" />
            <path d="M500 740 L630 640 L760 740 Z" />
            <rect x="530" y="740" width="200" height="200" />
          </g>
          <g fill="rgb(var(--color-text-primary))" opacity="0.16">
            <rect x="105" y="800" width="45" height="55" />
            <rect x="190" y="800" width="45" height="55" />
            <rect x="345" y="835" width="40" height="50" />
            <rect x="415" y="835" width="40" height="50" />
            <rect x="565" y="790" width="45" height="55" />
            <rect x="650" y="790" width="45" height="55" />
          </g>
          {/* garis tanah */}
          <rect y="940" width="800" height="60" fill="rgb(var(--color-text-primary))" opacity="0.18" />
        </svg>

        {/* Overlay tipis supaya teks tetap terbaca di atas ilustrasi apa pun. */}
        <div className="absolute inset-0 bg-text-primary/25" />

        <div className="relative p-12">
          <p className="text-2xl font-semibold leading-snug text-surface">
            Setiap unit, setiap rupiah,<br />tercatat pada tempatnya.
          </p>
          <p className="mt-3 max-w-sm text-sm leading-relaxed text-surface/80">
            Pembukuan developer properti — dari RAB dan biaya proyek sampai
            penjualan unit, penerimaan, dan laporan keuangan.
          </p>
        </div>
      </aside>


      {/* ── Kanan: form ──────────────────────────────────────────────────── */}
      <main className="flex min-h-screen items-center justify-center px-4 py-12 sm:px-8">
        <div className="w-full max-w-sm">
          <div className="mb-8">
            <div className="flex items-center gap-3">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src="/logo.png" alt={BRAND_NAME} className="h-11 w-11 shrink-0 object-contain" />
              <h1 className="text-xl font-extrabold tracking-tight text-text-primary">
                {BRAND_PREFIX} <span className="text-accent">{BRAND_ACCENT}</span>
              </h1>
            </div>
            <h2 className="display-lg mt-6 text-2xl text-text-primary">Masuk ke akun Anda</h2>
            <p className="mt-1 text-sm text-text-secondary">
              Gunakan email dan kata sandi yang diberikan admin perusahaan Anda.
            </p>
          </div>

          <LoginForm expired={expired === "1"} />
        </div>
      </main>
    </div>
  );
}
