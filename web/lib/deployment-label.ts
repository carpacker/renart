export function deploymentLabel(
  ordinal: number | undefined,
  versionId: string | undefined,
  noun = "Deployment",
) {
  const normalizedOrdinal = Number.isInteger(ordinal) && Number(ordinal) > 0 ? Number(ordinal) : 0;
  const shortVersion = versionId?.trim().slice(0, 8);
  const identity = normalizedOrdinal > 0 ? `${noun} #${normalizedOrdinal}` : noun;
  return shortVersion ? `${identity} · ${shortVersion}` : identity;
}
