"use client";

import { ReactNode } from "react";

// Tata letak form di dalam dialog.
//
// Melebarkan modal saja tidak memperbaiki apa pun: satu kolom yang direntangkan
// justru membuat jarak label→isian makin jauh dan mata harus melompat. Lebar
// baru dipakai dengan menaruh field berpasangan, dan field yang panjang
// (alamat, catatan, pilihan akun) mengambil dua kolom lewat <FormFull>.

export function FormGrid({
  children,
  columns = 2,
  className = "",
}: {
  children: ReactNode;
  columns?: 1 | 2 | 3;
  className?: string;
}) {
  const cols =
    columns === 1
      ? ""
      : columns === 3
        ? "sm:grid-cols-2 lg:grid-cols-3"
        : "sm:grid-cols-2";
  return <div className={`grid grid-cols-1 ${cols} gap-x-5 gap-y-4 ${className}`}>{children}</div>;
}

/** Field yang melebar penuh di dalam FormGrid. */
export function FormFull({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return <div className={`sm:col-span-2 lg:col-span-3 ${className}`}>{children}</div>;
}

/**
 * Pengelompokan bagian form. Dipakai saat satu dialog memuat lebih dari satu
 * urusan (mis. identitas + termin pembayaran) — pemisah yang jelas lebih
 * menolong daripada satu tumpukan field panjang.
 */
export function FormSection({
  title,
  description,
  children,
  className = "",
}: {
  title?: string;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={className}>
      {(title || description) && (
        <div className="mb-3">
          {title && (
            <h3 className="text-xs font-semibold uppercase tracking-wide text-text-secondary">
              {title}
            </h3>
          )}
          {description && <p className="text-xs text-text-tertiary mt-0.5">{description}</p>}
        </div>
      )}
      {children}
    </section>
  );
}

/** Garis pemisah antar-FormSection. */
export function FormDivider({ className = "" }: { className?: string }) {
  return <hr className={`border-0 border-t border-border-subtle my-6 ${className}`} />;
}
