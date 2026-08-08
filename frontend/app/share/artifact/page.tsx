import { Suspense } from "react";

import { PublicArtifactPage } from "@/features/share/components/public-artifact-page";

export default function ArtifactSharePage() {
  return (
    <Suspense fallback={<div className="flex min-h-screen items-center justify-center text-xs text-muted-foreground">Loading…</div>}>
      <PublicArtifactPage />
    </Suspense>
  );
}
