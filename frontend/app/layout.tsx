import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { ToastProvider } from "@/components/ui/Toast";
import { BRAND_NAME } from "@/lib/brand";

// Inter — grotesque netral, x-height tinggi; match referensi klien (kesan
// "mahal/modern" tanpa karakter geometris menonjol). Variable --font-inter.
const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});

export const metadata: Metadata = {
  title: BRAND_NAME,
  description: "Platform akuntansi developer properti Indonesia",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="id" className={inter.variable}>
      <body className="font-sans bg-bg text-text-primary antialiased">
        <ToastProvider>
          {children}
        </ToastProvider>
      </body>
    </html>
  );
}
