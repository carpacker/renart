import { WorkspaceConfigResponse } from "@/lib/types";

export type OnboardingConnectionValues = Record<string, string | number | boolean>;

export type OnboardingConnectionDraft = {
  environmentName: string;
  type: string;
  name: string;
  values: OnboardingConnectionValues;
};

export type OnboardingConnectionFormProps = {
  defaultName: string;
  defaultValues: OnboardingConnectionValues;
  busy: boolean;
  environmentName: string;
  initialValues?: OnboardingConnectionValues;
  onSubmit: (values: OnboardingConnectionValues) => Promise<void>;
};

export type OnboardingComponentProps = {
  workspaceConfig: WorkspaceConfigResponse;
  onCreateConnection: (input: {
    environment_name: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onUpdateConnection: (input: {
    environment_name: string;
    current_name?: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onReloadConfig: () => Promise<void> | void;
  onReloadWorkspace?: () => Promise<void> | void;
  onSkip: () => void;
  onComplete: () => void;
};
