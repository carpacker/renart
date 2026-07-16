"use client";

import { useEffect, useRef } from "react";

import {
  registerWorkspaceSaveParticipant,
  retireWorkspaceSaveParticipant,
  type WorkspaceSaveParticipant,
} from "@/lib/workspace-save-barrier";

/** Register a mounted editor with the process-wide workspace save barrier. */
export function useWorkspaceSaveParticipant(participant: WorkspaceSaveParticipant) {
  const participantRef = useRef(participant);
  participantRef.current = participant;

  useEffect(() => {
    const registeredParticipant = () => participantRef.current();
    const unregister = registerWorkspaceSaveParticipant(registeredParticipant);

    return () => {
      unregister();
      retireWorkspaceSaveParticipant(registeredParticipant);
    };
  }, []);
}
