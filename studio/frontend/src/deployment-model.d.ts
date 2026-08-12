import type {DeploymentArtifact} from './types';

export function preferredDeploymentArtifact(artifacts: DeploymentArtifact[]): DeploymentArtifact | null;
export function deploymentCanStart(artifact: DeploymentArtifact | null | undefined): boolean;
export function deploymentCompatibilityLabel(value: string): string;
export function deploymentPhaseLabel(value: string): string;
