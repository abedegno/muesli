import { describe, expect, it } from "vitest";
import { schemaToFields, initialValues } from "./schemaForm";
import type { JsonSchema } from "../api/types";

const schema: JsonSchema = {
  type: "object",
  required: ["model"],
  properties: {
    model: { type: "string", title: "Model", default: "llama3" },
    temperature: { type: "number", title: "Temperature", default: 0.2 },
    stream: { type: "boolean", title: "Stream" },
    api_key: { type: "string", title: "API Key", writeOnly: true },
    password: { type: "string", title: "Password", format: "password" },
    provider: { type: "string", title: "Provider", enum: ["ollama", "openai"] },
  },
};

describe("schemaToFields", () => {
  it("maps each property to a field with the right control kind", () => {
    const fields = schemaToFields(schema);
    const byName = Object.fromEntries(fields.map((f) => [f.name, f]));
    expect(byName.model.control).toBe("text");
    expect(byName.temperature.control).toBe("number");
    expect(byName.stream.control).toBe("checkbox");
    expect(byName.api_key.control).toBe("secret");
    expect(byName.password.control).toBe("secret");
    expect(byName.provider.control).toBe("select");
    expect(byName.provider.options).toEqual(["ollama", "openai"]);
  });

  it("marks required fields", () => {
    const fields = schemaToFields(schema);
    const model = fields.find((f) => f.name === "model")!;
    const temp = fields.find((f) => f.name === "temperature")!;
    expect(model.required).toBe(true);
    expect(temp.required).toBe(false);
  });

  it("uses title as label, falling back to the property name", () => {
    const fields = schemaToFields({ type: "object", properties: { raw_key: { type: "string" } } });
    expect(fields[0].label).toBe("raw_key");
  });

  it("returns an empty list when there are no properties", () => {
    expect(schemaToFields({ type: "object" })).toEqual([]);
    expect(schemaToFields(undefined)).toEqual([]);
  });
});

describe("initialValues", () => {
  it("seeds defaults, omits secret fields, and leaves others undefined", () => {
    const values = initialValues(schema);
    expect(values.model).toBe("llama3");
    expect(values.temperature).toBe(0.2);
    expect(values.stream).toBeUndefined();
    expect("api_key" in values).toBe(false); // secrets are write-only, never pre-filled
    expect("password" in values).toBe(false); // format=password is also write-only
  });
});
