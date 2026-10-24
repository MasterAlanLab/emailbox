import client, { type ApiResponse } from "@/lib/client";

export interface PublicPlan {
  name: string;
  max_accounts: number;
  max_groups: number;
  daily_mail_fetch: number;
}

export interface PublicPlanPrice {
  billing_period: "monthly" | "yearly";
  currency: string;
  amount: string;
}

export interface PublicPricing {
  billing_enabled: boolean;
  free_plan: PublicPlan;
  paid_plans: (PublicPlan & { prices: PublicPlanPrice[] })[];
}

export const pricingApi = {
  get: async () => (await client.get<ApiResponse<PublicPricing>>("/api/v1/pricing")).data,
};
