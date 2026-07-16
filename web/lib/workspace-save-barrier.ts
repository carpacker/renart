export type WorkspaceSaveParticipant = () => Promise<void>;

const participants = new Set<WorkspaceSaveParticipant>();
type RetiredWorkspaceSave = {
  participant: WorkspaceSaveParticipant;
  promise: Promise<void>;
  failed: boolean;
};
const retiredSaves = new Set<RetiredWorkspaceSave>();

function startRetiredWorkspaceSave(retiredSave: RetiredWorkspaceSave) {
  const promise = Promise.resolve().then(retiredSave.participant);
  retiredSave.promise = promise;
  retiredSave.failed = false;

  void promise.then(
    () => {
      if (retiredSave.promise === promise) {
        retiredSaves.delete(retiredSave);
      }
    },
    () => {
      if (retiredSave.promise === promise) {
        retiredSave.failed = true;
      }
    },
  );
}

/**
 * Keep an editor's final save visible to the next workspace operation after
 * that editor has unmounted and is no longer an active participant.
 */
export function retireWorkspaceSaveParticipant(participant: WorkspaceSaveParticipant) {
  const retiredSave: RetiredWorkspaceSave = {
    participant,
    promise: Promise.resolve(),
    failed: false,
  };
  retiredSaves.add(retiredSave);
  startRetiredWorkspaceSave(retiredSave);
}

export function registerWorkspaceSaveParticipant(participant: WorkspaceSaveParticipant) {
  participants.add(participant);
  return () => {
    participants.delete(participant);
  };
}

/**
 * Flush and await every mounted editor that can have a local workspace write
 * pending. Run/deploy actions call this before resolving their source so the
 * filesystem snapshot includes the latest editor state.
 */
export async function awaitWorkspaceSaves() {
  const failures: unknown[] = [];
  const recordFailures = (results: PromiseSettledResult<void>[]) => {
    for (const result of results) {
      if (
        result.status === "rejected" &&
        !failures.some((failure) => Object.is(failure, result.reason))
      ) {
        failures.push(result.reason);
      }
    }
  };

  recordFailures(
    await Promise.allSettled(
      Array.from(participants, (participant) => Promise.resolve().then(participant)),
    ),
  );

  // A participant can unmount while the mounted saves above are in flight.
  // Drain retired saves until the set is stable so navigation cannot let a
  // run/deploy action overtake the editor's final write.
  const observedRetiredSaves = new Set<RetiredWorkspaceSave>();
  while (true) {
    const retired = Array.from(retiredSaves).filter(
      (retiredSave) => !observedRetiredSaves.has(retiredSave),
    );
    if (retired.length === 0) {
      break;
    }
    for (const retiredSave of retired) {
      observedRetiredSaves.add(retiredSave);
      // Keep a failed, unmounted editor registered so the next operation gets
      // one opportunity to retry its pending value instead of silently moving
      // on with the stale file.
      if (retiredSave.failed) {
        startRetiredWorkspaceSave(retiredSave);
      }
    }
    const results = await Promise.allSettled(retired.map(({ promise }) => promise));
    recordFailures(results);
  }

  if (failures.length === 1) {
    throw failures[0];
  }
  if (failures.length > 1) {
    throw new AggregateError(failures, "Could not save all workspace changes");
  }
}
