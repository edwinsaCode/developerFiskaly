import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // ── Design tokens — satu-satunya tempat warna didefinisikan ─────────
        bg: "rgb(var(--color-bg) / <alpha-value>)",
        surface: "rgb(var(--color-surface) / <alpha-value>)",
        border: "rgb(var(--color-border) / <alpha-value>)",
        "border-subtle": "rgb(var(--color-border-subtle) / <alpha-value>)",
        // Teks
        "text-primary": "rgb(var(--color-text-primary) / <alpha-value>)",
        "text-secondary": "rgb(var(--color-text-secondary) / <alpha-value>)",
        "text-tertiary": "rgb(var(--color-text-tertiary) / <alpha-value>)",
        // Aksen institusional (slate biru gelap)
        accent: {
          DEFAULT: "rgb(var(--color-accent) / <alpha-value>)",
          light: "rgb(var(--color-accent-light) / <alpha-value>)",
          hover: "rgb(var(--color-accent-hover) / <alpha-value>)",
        },
        // Semantik (hemat — hanya status)
        success: {
          DEFAULT: "rgb(var(--color-success) / <alpha-value>)",
          bg: "rgb(var(--color-success-bg) / <alpha-value>)",
        },
        danger: {
          DEFAULT: "rgb(var(--color-danger) / <alpha-value>)",
          bg: "rgb(var(--color-danger-bg) / <alpha-value>)",
        },
        warning: {
          DEFAULT: "rgb(var(--color-warning) / <alpha-value>)",
          bg: "rgb(var(--color-warning-bg) / <alpha-value>)",
        },
        // Palet data kategorikal "Warm Ledger" (divalidasi dataviz skill).
        // Urutan tetap; dipakai untuk seri chart, BUKAN dekorasi UI.
        chart: {
          1: "rgb(var(--chart-1) / <alpha-value>)",
          2: "rgb(var(--chart-2) / <alpha-value>)",
          3: "rgb(var(--chart-3) / <alpha-value>)",
          4: "rgb(var(--chart-4) / <alpha-value>)",
          5: "rgb(var(--chart-5) / <alpha-value>)",
          6: "rgb(var(--chart-6) / <alpha-value>)",
        },
      },
      fontFamily: {
        sans: ["var(--font-inter)", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "monospace"],
      },
      fontSize: {
        "2xs": ["11px", "16px"],
        xs: ["12px", "16px"],
        sm: ["13px", "20px"],
        base: ["14px", "20px"],
        md: ["15px", "22px"],
        lg: ["16px", "24px"],
        xl: ["18px", "28px"],
        "2xl": ["22px", "30px"],
        "3xl": ["27px", "34px"],
        "4xl": ["33px", "40px"],
      },
      spacing: {
        // 4px base grid
        0.5: "2px",
        1: "4px",
        1.5: "6px",
        2: "8px",
        2.5: "10px",
        3: "12px",
        3.5: "14px",
        4: "16px",
        5: "20px",
        6: "24px",
        7: "28px",
        8: "32px",
        10: "40px",
        12: "48px",
        16: "64px",
      },
      borderRadius: {
        sm: "4px",
        DEFAULT: "6px",
        md: "6px",
        lg: "8px",
        xl: "12px",
      },
      // ── Skala lapisan (W-12) — SATU-SATUNYA sumber urutan tumpukan ─────────
      //
      // Sebelumnya setiap overlay memakai z-50 apa adanya: modal, drawer,
      // command palette, DAN toast semuanya di angka yang sama. Pada nilai
      // z-index yang sama, urutan DOM yang menentukan — dan modal mem-portal
      // dirinya ke akhir <body>, jadi modal selalu menang. Akibatnya pesan
      // error dari dalam modal muncul di belakang modal itu sendiri: tak
      // terbaca persis ketika ia paling dibutuhkan.
      //
      // Perbaikannya bukan menaikkan angka di sana-sini, melainkan menyatakan
      // urutannya sekali di sini. Toast berada di lapisan tersendiri di atas
      // semua overlay, karena ia memang harus selalu terbaca.
      zIndex: {
        header: "30",   // header sticky, di bawah semua overlay
        overlay: "50",  // modal, drawer, command palette, konfirmasi
        toast: "100",   // notifikasi — selalu di atas overlay apa pun
      },
      boxShadow: {
        sm: "0 1px 2px 0 rgb(0 0 0 / 0.05)",
        DEFAULT: "0 1px 3px 0 rgb(0 0 0 / 0.06), 0 1px 2px -1px rgb(0 0 0 / 0.04)",
        md: "0 4px 6px -1px rgb(0 0 0 / 0.07), 0 2px 4px -2px rgb(0 0 0 / 0.04)",
        lg: "0 10px 15px -3px rgb(0 0 0 / 0.07), 0 4px 6px -4px rgb(0 0 0 / 0.04)",
        // Dialog melayang di atas seluruh halaman — bayangannya harus lebih
        // dalam dari kartu biasa agar bidangnya terbaca terangkat, bukan
        // tertempel.
        dialog: "0 24px 48px -12px rgb(0 0 0 / 0.18), 0 8px 16px -8px rgb(0 0 0 / 0.10)",
      },
      keyframes: {
        "overlay-in": {
          from: { opacity: "0" },
          to: { opacity: "1" },
        },
        "panel-in": {
          from: { opacity: "0", transform: "translateY(8px) scale(0.985)" },
          to: { opacity: "1", transform: "translateY(0) scale(1)" },
        },
      },
      animation: {
        "overlay-in": "overlay-in 140ms ease-out",
        // Kurva decelerate: cepat muncul lalu melambat — terbaca sebagai
        // panel yang "mendarat", bukan meletus.
        "panel-in": "panel-in 200ms cubic-bezier(0.16, 1, 0.3, 1)",
      },
    },
  },
  plugins: [],
};

export default config;
