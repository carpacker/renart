import type * as MonacoNS from "monaco-editor";

export type MinimalTextEdit = {
  start: number;
  end: number;
  text: string;
};

/**
 * Reduce a full incoming snapshot to the one contiguous edit that changes the
 * current model. Monaco can then transform selections and markers through the
 * edit, which is the same boundary a future collaborative transport can use.
 */
export function computeMinimalTextEdit(current: string, next: string): MinimalTextEdit | null {
  if (current === next) {
    return null;
  }

  let start = 0;
  const sharedLimit = Math.min(current.length, next.length);
  while (start < sharedLimit && current[start] === next[start]) {
    start += 1;
  }

  let currentEnd = current.length;
  let nextEnd = next.length;
  while (currentEnd > start && nextEnd > start && current[currentEnd - 1] === next[nextEnd - 1]) {
    currentEnd -= 1;
    nextEnd -= 1;
  }

  return { start, end: currentEnd, text: next.slice(start, nextEnd) };
}

export function transformOffsetThroughEdit(offset: number, edit: MinimalTextEdit) {
  if (offset <= edit.start) {
    return offset;
  }
  if (offset >= edit.end) {
    return offset + edit.text.length - (edit.end - edit.start);
  }
  return edit.start + edit.text.length;
}

export function applyExternalModelValue(
  editor: MonacoNS.editor.IStandaloneCodeEditor,
  monaco: typeof MonacoNS,
  nextValue: string,
) {
  const model = editor.getModel();
  if (!model) {
    return false;
  }
  const edit = computeMinimalTextEdit(model.getValue(), nextValue);
  if (!edit) {
    return false;
  }

  const start = model.getPositionAt(edit.start);
  const end = model.getPositionAt(edit.end);
  const transformedSelections = (editor.getSelections() ?? []).map((selection) => {
    const anchorOffset = model.getOffsetAt({
      lineNumber: selection.selectionStartLineNumber,
      column: selection.selectionStartColumn,
    });
    const activeOffset = model.getOffsetAt({
      lineNumber: selection.positionLineNumber,
      column: selection.positionColumn,
    });
    const anchor = model.getPositionAt(transformOffsetThroughEdit(anchorOffset, edit));
    const active = model.getPositionAt(transformOffsetThroughEdit(activeOffset, edit));
    return new monaco.Selection(anchor.lineNumber, anchor.column, active.lineNumber, active.column);
  });

  return editor.executeEdits(
    "renart.external-model-update",
    [
      {
        range: new monaco.Range(start.lineNumber, start.column, end.lineNumber, end.column),
        text: edit.text,
        forceMoveMarkers: true,
      },
    ],
    transformedSelections,
  );
}
