import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";

import { AppVersionGuard } from "@/features/layouts";
import { AppearancePreferencesProvider } from "@/features/settings";
import { AppI18nProvider } from "@/i18n/app-i18n-provider";
import { BrandingProvider } from "@/shared/config/branding-provider";
import { DevtoolsBrandBanner } from "@/shared/components/devtools-brand-banner";
import { ThemeProvider } from "@/shared/components/theme-provider";
import { LegacyPWAServiceWorkerMigration } from "@/shared/pwa/migrations/legacy-service-worker-migration";
import { Toaster } from "@/components/ui/sonner";

import "./globals.css";
import "katex/dist/katex.min.css";
import "streamdown/styles.css";

const geistSans = localFont({
  src: "./fonts/geist-latin.woff2",
  variable: "--font-sans",
  weight: "100 900",
});

const geistMono = localFont({
  src: "./fonts/geist-mono-latin.woff2",
  variable: "--font-mono",
  weight: "100 900",
});

const jetBrainsMono = localFont({
  src: "./fonts/geist-mono-latin.woff2",
  variable: "--font-jetbrains-mono",
  weight: "100 900",
});

export const metadata: Metadata = {
  appleWebApp: {
    capable: true,
    statusBarStyle: "default",
  },
  formatDetection: {
    telephone: false,
  },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  maximumScale: 1,
  userScalable: false,
  themeColor: "#0f172a",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} ${jetBrainsMono.variable} h-full`}
      data-branding-pending="true"
      suppressHydrationWarning
    >
      <body
        className="h-full min-h-svh overflow-hidden antialiased"
      >
        <BrandingProvider>
          <AppI18nProvider>
            <ThemeProvider>
              <AppearancePreferencesProvider>
                {children}
                <AppVersionGuard />
                <LegacyPWAServiceWorkerMigration />
                <Toaster />
                <DevtoolsBrandBanner />
              </AppearancePreferencesProvider>
            </ThemeProvider>
          </AppI18nProvider>
        </BrandingProvider>
      </body>
    </html>
  );
}
