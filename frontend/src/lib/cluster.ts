/** The button text for a node action. */
export function actionLabel(action: string): string {
  switch (action) {
    case "stop":
      return "Stop";
    case "start":
      return "Start";
    case "remove":
      return "Remove";
    case "revive":
      return "Revive";
    default:
      return action.charAt(0).toUpperCase() + action.slice(1);
  }
}

/** The Carbon tag colour for a node's state. */
export function nodeTagType(state: string): "green" | "gray" | "red" {
  if (state === "running") return "green";
  if (state === "stopped") return "gray";
  return "red";
}
