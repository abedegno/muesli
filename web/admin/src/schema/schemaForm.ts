import type { JsonSchema, JsonSchemaProperty } from "../api/types";

export type FieldControl = "text" | "number" | "checkbox" | "select" | "secret";

export interface FormField {
  name: string;
  label: string;
  control: FieldControl;
  required: boolean;
  description?: string;
  options?: string[];
  default?: unknown;
}

/**
 * schemaToFields turns a (subset of) JSON Schema into an ordered list of form
 * fields the SchemaForm component can render. Unknown/unsupported types fall
 * back to a text control. Secret fields (writeOnly or format "password") render
 * as write-only inputs and are never pre-populated from stored values.
 */
export function schemaToFields(schema: JsonSchema | undefined): FormField[] {
  if (!schema || !schema.properties) return [];
  const required = new Set(schema.required ?? []);
  return Object.entries(schema.properties).map(([name, prop]) => ({
    name,
    label: prop.title ?? name,
    control: controlFor(prop),
    required: required.has(name),
    description: prop.description,
    options: prop.enum,
    default: prop.default,
  }));
}

function controlFor(prop: JsonSchemaProperty): FieldControl {
  if (prop.writeOnly || prop.format === "password") return "secret";
  if (prop.enum && prop.enum.length > 0) return "select";
  switch (prop.type) {
    case "boolean":
      return "checkbox";
    case "number":
    case "integer":
      return "number";
    default:
      return "text";
  }
}

/**
 * initialValues seeds a config object from schema defaults. Secret fields are
 * intentionally omitted so an empty secret input means "leave unchanged".
 */
export function initialValues(schema: JsonSchema | undefined): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const field of schemaToFields(schema)) {
    if (field.control === "secret") continue;
    if (field.default !== undefined) out[field.name] = field.default;
  }
  return out;
}
