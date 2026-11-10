import { Suspense } from "react";

import { AcceptInvite } from "@/features/households/accept-invite";

export default function InvitePage() {
  return (
    <Suspense>
      <AcceptInvite />
    </Suspense>
  );
}
