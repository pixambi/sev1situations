import type { ParameterView } from "../../bindings/github.com/pixambi/sev1situations/internal/gui/models.js";

/** A parameter's value as the form holds it: text for a choice, a number for an int. */
export type Values = Record<string, string | number>;

/** The longest name Go accepts. */
export const maxNameLength = 100;

/** The starting value of each parameter: its default, else the first choice, else the lowest allowed number. */
export function initialValues(params: ParameterView[]): Values {
  const out: Values = {};
  for (const p of params) {
    if (p.Default !== null && p.Default !== undefined && p.Default !== "") {
      out[p.ID] = p.Default as string | number;
    } else if (p.Type === "edition") {
      out[p.ID] = p.Options?.[0] ?? "";
    } else if (p.Type === "int") {
      out[p.ID] = p.Min;
    } else {
      out[p.ID] = ""; // a version is filled in when the versions arrive
    }
  }
  return out;
}

/** The edition the versions are listed for: the edition parameter if there is one, else the scenario's first. */
export function editionFor(params: ParameterView[], values: Values, editions: string[]): string {
  const p = params.find((q) => q.Type === "edition");
  if (p) return String(values[p.ID] ?? "");
  return editions.includes("community") ? "community" : (editions[0] ?? "community");
}

/** What is wrong with each field, by field name. An empty result means the form can be sent. */
export function validate(form: { instructor: string; trainee: string }, params: ParameterView[], values: Values): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const [field, label, v] of [["instructor", "instructor's name", form.instructor], ["trainee", "trainee's name", form.trainee]] as const) {
    const name = v.trim();
    if (name === "") errors[field] = `Enter the ${label}.`;
    else if ([...name].length > maxNameLength) errors[field] = `The ${label} can be at most ${maxNameLength} characters.`;
  }
  for (const p of params) {
    const v = values[p.ID];
    if (p.Type === "int") {
      const n = typeof v === "number" ? v : Number.NaN;
      if (!Number.isInteger(n)) errors[p.ID] = `Enter a whole number for ${p.Label.toLowerCase()}.`;
      else if (p.Max !== 0 && (n < p.Min || n > p.Max)) errors[p.ID] = `${p.Label} must be from ${p.Min} to ${p.Max}.`;
    } else if (v === "" || v === undefined) {
      errors[p.ID] = `Choose ${p.Label.toLowerCase()}.`;
    }
  }
  return errors;
}

/** The parameters in the shape Go expects. */
export function paramsToSend(values: Values): Record<string, string | number> {
  return { ...values };
}
