import { atom } from "jotai";
import { Edge, Node } from "reactflow";

export const nodesAtom = atom<Node[]>([]);
export const edgesAtom = atom<Edge[]>([]);
