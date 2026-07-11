import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PluginsView } from "./PluginsView";
import type { Plugin } from "../api/types";

function makeClient(plugins: Plugin[]) {
  return {
    listPlugins: vi.fn().mockResolvedValue(plugins),
    createPlugin: vi.fn().mockResolvedValue({}),
    updatePlugin: vi.fn().mockResolvedValue({}),
    deletePlugin: vi.fn().mockResolvedValue(undefined),
    checkPluginHealth: vi.fn().mockResolvedValue({ healthy: true }),
  };
}

const whisper: Plugin = {
  id: "p1",
  kind: "transcriber",
  name: "Whisper",
  endpoint_url: "http://whisper",
  enabled: true,
  is_default: true,
};

const whisperWithSchema: Plugin = {
  ...whisper,
  config: { model: "small", compute_type: "int8" },
  config_schema: {
    type: "object",
    properties: {
      model: {
        type: "string",
        title: "Model",
        enum: ["tiny", "base", "small", "medium", "large-v3"],
        default: "base",
      },
      compute_type: {
        type: "string",
        title: "Compute type",
        enum: ["default", "int8", "float16", "float32"],
        default: "default",
      },
      beam_size: { type: "integer", title: "Beam size", default: 5 },
    },
  },
};

const ollamaAgentWithSchema: Plugin = {
  id: "p4",
  kind: "agent",
  name: "Ollama",
  endpoint_url: "http://ollama-agent",
  enabled: true,
  is_default: true,
  config: { ollama_url: "http://localhost:11434", model: "mistral:latest", temperature: 0.2 },
  config_schema: {
    type: "object",
    properties: {
      ollama_url: {
        type: "string",
        title: "Ollama URL",
        default: "http://localhost:11434",
      },
      model: {
        type: "string",
        title: "Model",
        enum: ["llama3.2:latest", "mistral:latest"],
        default: "llama3.2",
      },
      base_url: {
        type: "string",
        title: "OpenAI-compatible base URL (opt-in egress)",
      },
      api_key: {
        type: "string",
        title: "API key (for base_url)",
        writeOnly: true,
        format: "password",
      },
      temperature: {
        type: "number",
        title: "Temperature",
        default: 0.2,
      },
    },
  },
};

const noEnumSchemaPlugin: Plugin = {
  ...whisper,
  id: "p3",
  name: "NoEnum",
  config: { endpoint_note: "hello" },
  config_schema: {
    type: "object",
    properties: {
      endpoint_note: { type: "string", title: "Endpoint note" },
    },
  },
};

describe("PluginsView", () => {
  it("lists plugins fetched from the client", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    expect(await screen.findByText("Whisper")).toBeInTheDocument();
    expect(client.listPlugins).toHaveBeenCalled();
  });

  it("shows the plugin kind as a badge", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    // The kind badge should contain the kind text
    const badge = screen.getByText("transcriber");
    expect(badge).toBeInTheDocument();
    expect(badge.className).toMatch(/badge/);
  });

  it("shows the plugin endpoint_url", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    expect(screen.getByText("http://whisper")).toBeInTheDocument();
  });

  it("shows a styled enabled indicator (pill) for an enabled plugin", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    // Enabled pill should be present and have the yes class
    const pill = screen.getByText(/✓ enabled/i);
    expect(pill).toBeInTheDocument();
    expect(pill.className).toMatch(/pill-yes/);
  });

  it("shows a styled disabled indicator (pill) for a disabled plugin", async () => {
    const disabled: Plugin = { ...whisper, id: "p2", name: "Disabled", enabled: false, is_default: false };
    const client = makeClient([disabled]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Disabled");
    const pill = screen.getByText(/✗ disabled/i);
    expect(pill).toBeInTheDocument();
    expect(pill.className).toMatch(/pill-no/);
  });

  it("shows a pill-yes styled Default indicator when is_default is true", async () => {
    const client = makeClient([whisper]); // whisper has is_default: true
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    const defaultPill = screen.getByText(/✓ default/i);
    expect(defaultPill).toBeInTheDocument();
    expect(defaultPill.className).toMatch(/pill-yes/);
  });

  it("shows a pill-no styled Default indicator when is_default is false", async () => {
    const nonDefault: Plugin = { ...whisper, id: "p2", name: "NonDefault", is_default: false };
    const client = makeClient([nonDefault]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("NonDefault");
    const defaultPill = screen.getByText(/✗ no/i);
    expect(defaultPill).toBeInTheDocument();
    expect(defaultPill.className).toMatch(/pill-no/);
  });

  it("toggles enabled via updatePlugin", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /disable/i }));
    await waitFor(() => expect(client.updatePlugin).toHaveBeenCalledWith("p1", { enabled: false }));
  });

  it("sets default via updatePlugin", async () => {
    const other: Plugin = { ...whisper, id: "p2", name: "Whisper2", is_default: false };
    const client = makeClient([whisper, other]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper2");
    const rows = screen.getAllByRole("row");
    const targetRow = rows.find((r) => r.textContent?.includes("Whisper2"))!;
    await userEvent.click(within(targetRow).getByRole("button", { name: /set default/i }));
    await waitFor(() => expect(client.updatePlugin).toHaveBeenCalledWith("p2", { is_default: true }));
  });

  it("deletes via deletePlugin", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /delete/i }));
    await waitFor(() => expect(client.deletePlugin).toHaveBeenCalledWith("p1"));
  });

  it("includes the write-only auth token in the create payload", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /add plugin/i }));

    await userEvent.type(screen.getByLabelText(/^name$/i), "Ollama");
    await userEvent.type(screen.getByLabelText(/endpoint url/i), "http://o");
    await userEvent.type(screen.getByLabelText(/plugin auth token/i), "shared-secret");
    await userEvent.click(screen.getByRole("button", { name: /save plugin/i }));

    await waitFor(() =>
      expect(client.createPlugin).toHaveBeenCalledWith(
        expect.objectContaining({ name: "Ollama", token: "shared-secret" }),
      ),
    );
  });

  it("does not render a config section for a plugin with no config_schema", async () => {
    const client = makeClient([whisper]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    expect(screen.queryByRole("button", { name: /configure/i })).not.toBeInTheDocument();
  });

  it("does not render the enum picker block for a plugin with no enum fields", async () => {
    const client = makeClient([noEnumSchemaPlugin]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("NoEnum");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));
    // The generic (non-enum) field is still editable.
    expect(await screen.findByLabelText(/endpoint note/i)).toBeInTheDocument();
    // But no <select> for a config field should be present (only the picker
    // renders selects, and there are no enum fields on this schema).
    expect(document.querySelector("select")).toBeNull();
  });

  it("renders enum config fields as selects pre-set to the plugin's current config values", async () => {
    const client = makeClient([whisperWithSchema]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));

    const modelSelect = (await screen.findByLabelText(/^model$/i)) as HTMLSelectElement;
    expect(modelSelect.value).toBe("small");
    const computeSelect = screen.getByLabelText(/compute type/i) as HTMLSelectElement;
    expect(computeSelect.value).toBe("int8");
  });

  it("Apply saves config then checks health and shows the healthy state", async () => {
    const client = makeClient([whisperWithSchema]);
    client.checkPluginHealth.mockResolvedValue({ healthy: true });
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));

    const modelSelect = await screen.findByLabelText(/^model$/i);
    await userEvent.selectOptions(modelSelect, "medium");
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));

    await waitFor(() =>
      expect(client.updatePlugin).toHaveBeenCalledWith(
        "p1",
        expect.objectContaining({ config: expect.objectContaining({ model: "medium" }) }),
      ),
    );
    await waitFor(() => expect(client.checkPluginHealth).toHaveBeenCalledWith("p1"));
    expect(await screen.findByText(/healthy/i)).toBeInTheDocument();
  });

  it("Apply shows the inline unhealthy state with the error message", async () => {
    const client = makeClient([whisperWithSchema]);
    client.checkPluginHealth.mockResolvedValue({ healthy: false, error: "model not found" });
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));

    await waitFor(() => expect(client.checkPluginHealth).toHaveBeenCalledWith("p1"));
    expect(await screen.findByText(/unhealthy/i)).toBeInTheDocument();
    expect(screen.getByText(/model not found/i)).toBeInTheDocument();
  });

  it("surfaces an updatePlugin save error inline and does not call checkPluginHealth", async () => {
    const client = makeClient([whisperWithSchema]);
    client.updatePlugin.mockRejectedValue(new Error("invalid model"));
    render(<PluginsView client={client as never} />);
    await screen.findByText("Whisper");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));

    expect(await screen.findByText(/invalid model/i)).toBeInTheDocument();
    expect(client.checkPluginHealth).not.toHaveBeenCalled();
  });

  it("renders the ollama-agent's single enum field (Model) pre-set to the plugin's current config value", async () => {
    const client = makeClient([ollamaAgentWithSchema]);
    render(<PluginsView client={client as never} />);
    await screen.findByText("Ollama");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));

    // config.model is "mistral:latest" — the SECOND enum option, not the
    // first ("llama3.2:latest") — proving this tracks the current stored
    // config rather than defaulting to options[0].
    const modelSelect = (await screen.findByLabelText(/^model$/i)) as HTMLSelectElement;
    expect(modelSelect.value).toBe("mistral:latest");
    // Only one enum field on this schema (model) — ollama_url/base_url/
    // api_key/temperature have no enum, so only one <select> renders.
    expect(document.querySelectorAll("select")).toHaveLength(1);
  });

  it("Apply on the ollama-agent picker saves the newly selected model then checks health", async () => {
    const client = makeClient([ollamaAgentWithSchema]);
    client.checkPluginHealth.mockResolvedValue({ healthy: true });
    render(<PluginsView client={client as never} />);
    await screen.findByText("Ollama");
    await userEvent.click(screen.getByRole("button", { name: /configure/i }));

    const modelSelect = await screen.findByLabelText(/^model$/i);
    await userEvent.selectOptions(modelSelect, "llama3.2:latest");
    await userEvent.click(screen.getByRole("button", { name: /apply/i }));

    await waitFor(() =>
      expect(client.updatePlugin).toHaveBeenCalledWith(
        "p4",
        expect.objectContaining({
          config: expect.objectContaining({ model: "llama3.2:latest" }),
        }),
      ),
    );
    await waitFor(() => expect(client.checkPluginHealth).toHaveBeenCalledWith("p4"));
    expect(await screen.findByText(/healthy/i)).toBeInTheDocument();
  });
});
