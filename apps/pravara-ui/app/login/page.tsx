"use client";

import { pravaraSignIn } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/50">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">PravaraMES</CardTitle>
          <CardDescription>
            Sign in to access your manufacturing dashboard
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            className="w-full"
            onClick={() => pravaraSignIn("/dashboard")}
          >
            Sign in with Janua SSO
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
