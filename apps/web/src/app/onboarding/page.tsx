"use client";

import { AppShell } from "@/components/app-shell";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { CreateHouseholdForm } from "@/features/households/create-household-form";

export default function OnboardingPage() {
  return (
    <AppShell>
      <Card>
        <CardHeader>
          <CardTitle>Crie sua família</CardTitle>
          <CardDescription>
            A família reúne contas, cartões e lançamentos de todos os membros.
            Você será o responsável e poderá convidar outras pessoas.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <CreateHouseholdForm />
        </CardContent>
      </Card>
    </AppShell>
  );
}
