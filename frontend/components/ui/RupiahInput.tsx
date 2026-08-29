"use client";

import { useState, useEffect } from "react";
import { shouldShowRupiahError, validateRupiah } from "./rupiah";

// Diekspor ulang supaya seluruh pemanggil lama tidak perlu diubah.
export { validateRupiah };

interface RupiahInputProps {
  label?: string;
  value: string;            // raw integer string, misal "500000000"
  onChange: (val: string) => void;
  error?: string;
  hint?: string;
  required?: boolean;
  /**
   * Tampilkan `error` walau kotaknya masih kosong. Dinyalakan oleh form yang
   * mengisi `error` hanya saat submit — tanpa ini pesannya tertelan dan tombol
   * simpan terasa mati. Bawaannya `false`: form yang menghitung error secara
   * eager tetap tenang sebelum disentuh. Lihat `shouldShowRupiahError`.
   */
  showErrorWhenEmpty?: boolean;
  disabled?: boolean;
  placeholder?: string;
  /** Utilitas untuk pembungkus field (mis. "sm:col-span-2" di dalam FormGrid). */
  className?: string;
  wrapperClassName?: string;
}

function formatThousands(raw: string): string {
  if (!raw) return "";
  const n = parseInt(raw, 10);
  if (isNaN(n)) return raw;
  return n.toLocaleString("id-ID");
}

// Disamakan dengan Input/Select (rounded-lg, py-2.5, ring-2): field uang paling
// sering berdiri bersebelahan dengan field biasa, dan beda tinggi 4px antar
// kotak langsung terbaca sebagai form yang tidak disusun.
const baseInput =
  "w-full rounded-lg border border-border bg-surface text-sm text-text-primary" +
  " placeholder:text-text-tertiary outline-none transition-shadow transition-colors pl-10 pr-3.5 py-2.5" +
  " num-right tabular" +
  " focus:border-accent focus:ring-2 focus:ring-accent/25" +
  " disabled:bg-border-subtle disabled:text-text-secondary disabled:cursor-not-allowed";

export function RupiahInput({
  label,
  value,
  onChange,
  error,
  hint,
  required,
  showErrorWhenEmpty,
  disabled,
  placeholder = "0",
  className = "",
  wrapperClassName = "",
}: RupiahInputProps) {
  const [display, setDisplay] = useState(formatThousands(value));

  // Sinkronisasi dari luar (reset form, dll)
  useEffect(() => {
    setDisplay(formatThousands(value));
  }, [value]);

  // Aturannya pindah ke `shouldShowRupiahError` (lihat alasannya di sana):
  // error pada kotak kosong ditelan secara bawaan, kecuali pemanggil menyatakan
  // errornya lahir dari submit lewat `showErrorWhenEmpty`.
  const showError = shouldShowRupiahError(error, value, showErrorWhenEmpty);

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    // Tolak semua karakter non-digit (termasuk . , desimal)
    const raw = e.target.value.replace(/\D/g, "");
    setDisplay(formatThousands(raw));
    onChange(raw);
  }

  function handleBlur() {
    setDisplay(formatThousands(value));
  }

  return (
    <div className={`flex flex-col gap-1.5 ${className} ${wrapperClassName}`}>
      {label && (
        <label className="text-sm font-medium text-text-primary">
          {label}
          {required && <span className="text-danger ml-0.5">*</span>}
        </label>
      )}
      <div className="relative">
        <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-sm text-text-tertiary pointer-events-none select-none">
          Rp
        </span>
        <input
          type="text"
          inputMode="numeric"
          value={display}
          onChange={handleChange}
          onBlur={handleBlur}
          disabled={disabled}
          placeholder={placeholder}
          aria-invalid={showError || undefined}
          className={`${baseInput} ${showError ? "border-danger focus:border-danger focus:ring-danger/25" : ""}`}
        />
      </div>
      {hint && !showError && <p className="text-xs text-text-tertiary">{hint}</p>}
      {showError && <p className="text-xs text-danger">{error}</p>}
    </div>
  );
}
