import type { Metadata } from "next";
import { Suspense } from "react";

import { PublicFilePage } from "@/features/share/components/public-file-page";

export const metadata: Metadata = {
  referrer: "no-referrer",
};

export default function SharedFilePage() {
  return (
    <Suspense fallback={null}>
      <PublicFilePage />
    </Suspense>
  );
}
